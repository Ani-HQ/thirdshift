package local

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Ani-HQ/thirdshift/internal/node/models"
	noderuntime "github.com/Ani-HQ/thirdshift/internal/node/runtime"
)

const defaultModelQuotaBytes = int64(20 * 1024 * 1024 * 1024)

type RunOptions struct {
	ModelID          string
	Prompt           string
	CatalogDir       string
	DataDir          string
	RuntimeRoot      string
	ModelCacheDir    string
	ModelQuotaBytes  int64
	RuntimePublicKey ed25519.PublicKey
	RuntimeEnv       []string
	Output           io.Writer
	StartupTimeout   time.Duration
	ShutdownTimeout  time.Duration
	HealthInterval   time.Duration
}

func Run(ctx context.Context, opts RunOptions) error {
	if opts.ModelID == "" {
		return fmt.Errorf("--model is required")
	}
	if opts.Prompt == "" {
		return fmt.Errorf("--prompt is required")
	}
	if opts.CatalogDir == "" {
		opts.CatalogDir = "models/catalog"
	}
	if opts.Output == nil {
		opts.Output = os.Stdout
	}
	dataDir, err := resolveDataDir(opts.DataDir)
	if err != nil {
		return err
	}
	if opts.RuntimeRoot == "" {
		opts.RuntimeRoot = filepath.Join(dataDir, "runtimes")
	}
	if opts.ModelCacheDir == "" {
		opts.ModelCacheDir = filepath.Join(dataDir, "models")
	}
	if opts.ModelQuotaBytes == 0 {
		opts.ModelQuotaBytes = defaultModelQuotaBytes
	}

	manifest, manifestPath, err := models.LoadCatalogManifest(opts.CatalogDir, opts.ModelID)
	if err != nil {
		return err
	}
	runtimeManifestPath := manifest.Runtime.ReleaseManifest
	if !filepath.IsAbs(runtimeManifestPath) {
		runtimeManifestPath = filepath.Join(filepath.Dir(manifestPath), runtimeManifestPath)
	}
	runtimeManifest, err := noderuntime.LoadReleaseManifest(runtimeManifestPath)
	if err != nil {
		return err
	}
	publicKey := opts.RuntimePublicKey
	if publicKey == nil {
		publicKey, err = noderuntime.DefaultRuntimePublicKey()
		if err != nil {
			return err
		}
	}
	installed, err := noderuntime.Manager{
		Root:      opts.RuntimeRoot,
		PublicKey: publicKey,
	}.Ensure(ctx, runtimeManifest)
	if err != nil {
		return err
	}

	cache := models.Cache{Dir: opts.ModelCacheDir, QuotaBytes: opts.ModelQuotaBytes}
	modelPath, err := cache.Ensure(ctx, models.Artifact{
		URL:       manifest.Source.URL,
		SHA256:    manifest.Source.SHA256,
		SizeBytes: manifest.Source.SizeBytes,
	}, manifest.Source.SHA256)
	if err != nil {
		return err
	}

	port, err := parseManifestPort(manifest.Runtime.Arguments.Port)
	if err != nil {
		return err
	}
	running, err := noderuntime.StartLlamaServer(ctx, noderuntime.LaunchConfig{
		ExecutablePath:  installed.ExecutablePath,
		ModelPath:       modelPath,
		WorkDir:         filepath.Dir(installed.ExecutablePath),
		LogDir:          filepath.Join(dataDir, "logs"),
		Redactions:      []string{opts.Prompt},
		Env:             opts.RuntimeEnv,
		StartupTimeout:  opts.StartupTimeout,
		ShutdownTimeout: opts.ShutdownTimeout,
		HealthInterval:  opts.HealthInterval,
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
		return err
	}
	defer running.Stop(context.Background(), opts.ShutdownTimeout)

	completion, err := noderuntime.ChatCompletion(ctx, running.BaseURL, noderuntime.CompletionRequest{
		Model:       manifest.ModelID,
		Messages:    []noderuntime.ChatMessage{{Role: "user", Content: opts.Prompt}},
		Temperature: 0.2,
		MaxTokens:   manifest.Limits.MaxOutputTokens,
		Stream:      false,
	})
	if err != nil {
		return err
	}
	if len(completion.Choices) == 0 {
		return fmt.Errorf("runtime returned no completion choices")
	}
	fmt.Fprintln(opts.Output, completion.Choices[0].Message.Content)
	fmt.Fprintf(opts.Output, "usage: prompt_tokens=%d completion_tokens=%d total_tokens=%d\n",
		completion.Usage.PromptTokens,
		completion.Usage.CompletionTokens,
		completion.Usage.TotalTokens,
	)
	return nil
}

func resolveDataDir(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return ".thirdshift", nil
	}
	return filepath.Join(base, "thirdshift"), nil
}

func parseManifestPort(value string) (int, error) {
	if value == "" || value == "dynamic" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("runtime.arguments.port must be dynamic or an integer: %w", err)
	}
	return parsed, nil
}
