package agent

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/Ani-HQ/thirdshift/internal/node/models"
	noderuntime "github.com/Ani-HQ/thirdshift/internal/node/runtime"
)

const defaultModelQuotaBytes = int64(20 * 1024 * 1024 * 1024)

type LocalRuntimeProvider struct {
	CatalogDir       string
	DataDir          string
	RuntimeRoot      string
	ModelCacheDir    string
	ModelQuotaBytes  int64
	RuntimePublicKey ed25519.PublicKey
	RuntimeEnv       []string
	HTTPClient       *http.Client
	StartupTimeout   time.Duration
	ShutdownTimeout  time.Duration
	HealthInterval   time.Duration
	mu               sync.Mutex
	running          *noderuntime.RunningRuntime
}

func (p *LocalRuntimeProvider) Prepare(ctx context.Context, modelID string) (RuntimeStatus, error) {
	if p.CatalogDir == "" {
		p.CatalogDir = "models/catalog"
	}
	if p.DataDir == "" {
		p.DataDir = filepath.Join(".", ".thirdshift")
	}
	if p.RuntimeRoot == "" {
		p.RuntimeRoot = filepath.Join(p.DataDir, "runtimes")
	}
	if p.ModelCacheDir == "" {
		p.ModelCacheDir = filepath.Join(p.DataDir, "models")
	}
	if p.ModelQuotaBytes == 0 {
		p.ModelQuotaBytes = defaultModelQuotaBytes
	}

	manifest, _, err := models.LoadCatalogManifest(p.CatalogDir, modelID)
	if err != nil {
		return RuntimeStatus{}, err
	}
	var runtimeManifest noderuntime.ReleaseManifest
	if runtimeManifestName := manifest.Runtime.ReleaseManifest; filepath.IsAbs(runtimeManifestName) {
		runtimeManifest, err = noderuntime.LoadReleaseManifest(runtimeManifestName)
	} else {
		data, source, readErr := models.ReadCatalogFile(p.CatalogDir, runtimeManifestName)
		if readErr != nil {
			return RuntimeStatus{}, readErr
		}
		runtimeManifest, err = noderuntime.ParseReleaseManifest(data, source)
	}
	if err != nil {
		return RuntimeStatus{}, err
	}
	publicKey := p.RuntimePublicKey
	if publicKey == nil {
		publicKey, err = noderuntime.DefaultRuntimePublicKey()
		if err != nil {
			return RuntimeStatus{}, err
		}
	}
	installed, err := noderuntime.Manager{
		Root:       p.RuntimeRoot,
		PublicKey:  publicKey,
		HTTPClient: p.HTTPClient,
	}.Ensure(ctx, runtimeManifest)
	if err != nil {
		return RuntimeStatus{}, err
	}
	cache := models.Cache{Dir: p.ModelCacheDir, QuotaBytes: p.ModelQuotaBytes, HTTPClient: p.HTTPClient}
	modelPath, err := cache.Ensure(ctx, models.Artifact{
		URL:       manifest.Source.URL,
		SHA256:    manifest.Source.SHA256,
		SizeBytes: manifest.Source.SizeBytes,
	}, manifest.Source.SHA256)
	if err != nil {
		return RuntimeStatus{}, err
	}
	if manifest.License.DistributeWithModel {
		text, ok := models.LicenseTextFor(manifest.License.Identifier)
		if !ok {
			return RuntimeStatus{}, fmt.Errorf("model %s requires license distribution but no vendored text for %q", manifest.ModelID, manifest.License.Identifier)
		}
		if _, err := cache.EnsureLicense(manifest.Source.SHA256, text); err != nil {
			return RuntimeStatus{}, err
		}
	}
	port, err := parsePort(manifest.Runtime.Arguments.Port)
	if err != nil {
		return RuntimeStatus{}, err
	}
	running, err := noderuntime.StartLlamaServer(ctx, noderuntime.LaunchConfig{
		ExecutablePath:  installed.ExecutablePath,
		ModelPath:       modelPath,
		WorkDir:         filepath.Dir(installed.ExecutablePath),
		LogDir:          filepath.Join(p.DataDir, "logs"),
		Env:             p.RuntimeEnv,
		StartupTimeout:  p.StartupTimeout,
		ShutdownTimeout: p.ShutdownTimeout,
		HealthInterval:  p.HealthInterval,
		Args: noderuntime.ApprovedArgs{
			ContextSize: manifest.Runtime.Arguments.ContextSize,
			BatchSize:   manifest.Runtime.Arguments.BatchSize,
			GPULayers:   manifest.Runtime.Arguments.GPULayers,
			Device:      manifest.Runtime.Arguments.Device,
			Parallel:    manifest.Runtime.Arguments.Parallel,
			Host:        manifest.Runtime.Arguments.Host,
			Port:        port,
		},
	})
	if err != nil {
		return RuntimeStatus{}, err
	}
	p.mu.Lock()
	if p.running != nil {
		_ = p.running.Stop(context.Background(), p.ShutdownTimeout)
	}
	p.running = running
	p.mu.Unlock()
	return RuntimeStatus{
		ModelID:     manifest.ModelID,
		RuntimeHash: canonicalSHA256(installed.SHA256),
		ModelHash:   canonicalSHA256(manifest.Source.SHA256),
		BaseURL:     running.BaseURL,
	}, nil
}

func (p *LocalRuntimeProvider) Close(ctx context.Context) error {
	p.mu.Lock()
	running := p.running
	p.running = nil
	p.mu.Unlock()
	if running == nil {
		return nil
	}
	return running.Stop(ctx, p.ShutdownTimeout)
}

func parsePort(value string) (int, error) {
	if value == "" || value == "dynamic" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("runtime.arguments.port must be dynamic or an integer: %w", err)
	}
	return parsed, nil
}
