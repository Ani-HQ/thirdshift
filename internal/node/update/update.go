package update

import (
	"archive/zip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"

	noderuntime "github.com/Ani-HQ/thirdshift/internal/node/runtime"
)

const (
	DefaultReleaseManifestURL     = "https://github.com/Ani-HQ/thirdshift/releases/latest/download/release-manifest.json"
	DefaultReleasePublicKeyBase64 = noderuntime.DefaultRuntimePublicKeyBase64
	defaultDownloadedArtifactName = "thirdshift-update-artifact"
	defaultDownloadedManifestName = "thirdshift-release-manifest.json"
)

type Manager struct {
	InstallDir    string
	CurrentBinary string
	PublicKey     ed25519.PublicKey
	PlatformKey   string
	HTTPClient    *http.Client
}

type Result struct {
	Version        string `json:"version"`
	BuildID        string `json:"build_id"`
	InstalledPath  string `json:"installed_path"`
	PreviousPath   string `json:"previous_path"`
	ArtifactSHA256 string `json:"artifact_sha256"`
}

func (m Manager) Update(ctx context.Context, manifestLocation string) (Result, error) {
	if manifestLocation == "" {
		manifestLocation = DefaultReleaseManifestURL
	}
	tmpDir, err := os.MkdirTemp("", "thirdshift-update-*")
	if err != nil {
		return Result{}, fmt.Errorf("create update temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	manifestPath := filepath.Join(tmpDir, defaultDownloadedManifestName)
	if err := m.download(ctx, manifestLocation, manifestPath); err != nil {
		return Result{}, err
	}
	manifest, err := noderuntime.LoadReleaseManifest(manifestPath)
	if err != nil {
		return Result{}, err
	}
	if err := manifest.VerifySignature(m.PublicKey); err != nil {
		return Result{}, err
	}
	if manifest.Engine != "thirdshift" {
		return Result{}, fmt.Errorf("release manifest engine %q is not thirdshift", manifest.Engine)
	}
	artifact, platformKey, err := selectArtifact(manifest, m.PlatformKey)
	if err != nil {
		return Result{}, err
	}
	artifactURL := resolveAgainst(manifestLocation, artifact.URL)
	artifactPath := filepath.Join(tmpDir, defaultDownloadedArtifactName)
	if err := m.download(ctx, artifactURL, artifactPath); err != nil {
		return Result{}, err
	}
	if err := VerifyDownloadedArtifact(manifest, platformKey, artifactPath, m.PublicKey); err != nil {
		return Result{}, err
	}

	extractDir := filepath.Join(tmpDir, "extract")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create update extract dir: %w", err)
	}
	if err := extractUpdateArtifact(artifactPath, artifact.ArchiveType, artifact.ExecutablePath, extractDir); err != nil {
		return Result{}, err
	}
	newBinary := filepath.Join(extractDir, filepath.FromSlash(artifact.ExecutablePath))
	if _, err := os.Stat(newBinary); err != nil {
		return Result{}, fmt.Errorf("updated binary %s not found in artifact: %w", artifact.ExecutablePath, err)
	}
	if goruntime.GOOS != "windows" {
		if err := os.Chmod(newBinary, 0o755); err != nil {
			return Result{}, fmt.Errorf("mark updated binary executable: %w", err)
		}
	}
	currentPath, installDir, err := m.currentPath()
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create install dir: %w", err)
	}
	previousPath := previousBinaryPath(currentPath)
	if err := promoteBinary(newBinary, currentPath, previousPath); err != nil {
		return Result{}, err
	}
	return Result{
		Version:        manifest.Version,
		BuildID:        manifest.BuildID,
		InstalledPath:  currentPath,
		PreviousPath:   previousPath,
		ArtifactSHA256: artifact.SHA256,
	}, nil
}

func VerifyDownloadedArtifact(manifest noderuntime.ReleaseManifest, platformKey, artifactPath string, publicKey ed25519.PublicKey) error {
	if err := manifest.VerifySignature(publicKey); err != nil {
		return err
	}
	if manifest.Engine != "thirdshift" {
		return fmt.Errorf("release manifest engine %q is not thirdshift", manifest.Engine)
	}
	artifact, ok := manifest.Artifacts[platformKey]
	if !ok {
		return fmt.Errorf("release artifact for platform %s is not in manifest", platformKey)
	}
	if artifact.ExecutablePath == "" {
		return fmt.Errorf("release artifact for %s has no executable_path", platformKey)
	}
	return verifyFile(artifactPath, artifact.SHA256, artifact.SizeBytes)
}

func (m Manager) currentPath() (string, string, error) {
	currentPath := m.CurrentBinary
	if currentPath == "" {
		executable, err := os.Executable()
		if err != nil {
			return "", "", fmt.Errorf("locate current executable: %w", err)
		}
		currentPath = executable
	}
	installDir := m.InstallDir
	if installDir == "" {
		installDir = filepath.Dir(currentPath)
	}
	if filepath.Base(currentPath) == currentPath {
		currentPath = filepath.Join(installDir, currentPath)
	}
	return currentPath, installDir, nil
}

func (m Manager) download(ctx context.Context, rawURL, dest string) error {
	parsed, err := url.Parse(rawURL)
	if err == nil && parsed.Scheme == "file" {
		return copyFile(parsed.Path, dest, 0o600)
	}
	if err == nil && parsed.Scheme == "" {
		return copyFile(rawURL, dest, 0o600)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("create update download request: %w", err)
	}
	client := m.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download update artifact: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download update artifact: HTTP %d", resp.StatusCode)
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create update download file: %w", err)
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("write update download file: %w", err)
	}
	return nil
}

func selectArtifact(manifest noderuntime.ReleaseManifest, platformKey string) (noderuntime.RuntimeArtifact, string, error) {
	if platformKey == "" {
		platformKey = goruntime.GOOS + "/" + goruntime.GOARCH
	}
	// The app updater ships one build per platform, so it resolves the bare
	// platform key. Vendor-specific keys are a runtime-engine concern.
	artifact, ok := manifest.Artifacts[platformKey]
	if !ok {
		return noderuntime.RuntimeArtifact{}, "", fmt.Errorf("release artifact for platform %s is not in manifest", platformKey)
	}
	return artifact, platformKey, nil
}

func resolveAgainst(base, value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() {
		return value
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return value
	}
	return baseURL.ResolveReference(parsed).String()
}

func extractUpdateArtifact(path, archiveType, executablePath, dest string) error {
	switch archiveType {
	case "", "binary":
		if executablePath == "" {
			return fmt.Errorf("binary release artifact requires executable_path")
		}
		return copyFile(path, filepath.Join(dest, filepath.FromSlash(executablePath)), 0o755)
	case "zip":
		return extractZip(path, dest)
	default:
		return fmt.Errorf("unsupported release archive type %q", archiveType)
	}
}

func extractZip(path, dest string) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("open update zip: %w", err)
	}
	defer reader.Close()
	cleanDest, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	for _, file := range reader.File {
		target := filepath.Join(dest, filepath.Clean(file.Name))
		cleanTarget, err := filepath.Abs(target)
		if err != nil {
			return err
		}
		if cleanTarget != cleanDest && !strings.HasPrefix(cleanTarget, cleanDest+string(os.PathSeparator)) {
			return fmt.Errorf("release zip contains unsafe path %q", file.Name)
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create update zip dir: %w", err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create update zip parent: %w", err)
		}
		in, err := file.Open()
		if err != nil {
			return fmt.Errorf("open update zip entry: %w", err)
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, file.Mode())
		if err != nil {
			in.Close()
			return fmt.Errorf("create update zip entry: %w", err)
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		in.Close()
		if copyErr != nil {
			return fmt.Errorf("copy update zip entry: %w", copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close update zip entry: %w", closeErr)
		}
	}
	return nil
}

func promoteBinary(newBinary, currentPath, previousPath string) error {
	if _, err := os.Stat(currentPath); err == nil {
		_ = os.Remove(previousPath)
		if err := os.Rename(currentPath, previousPath); err != nil {
			return fmt.Errorf("retain previous binary: %w", err)
		}
	}
	if err := os.Rename(newBinary, currentPath); err != nil {
		if _, statErr := os.Stat(previousPath); statErr == nil {
			_ = os.Rename(previousPath, currentPath)
		}
		return fmt.Errorf("promote updated binary: %w", err)
	}
	return nil
}

func previousBinaryPath(currentPath string) string {
	ext := filepath.Ext(currentPath)
	base := strings.TrimSuffix(currentPath, ext)
	return base + ".previous" + ext
}

func verifyFile(path, expectedSHA256 string, expectedSize int64) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open release artifact for verification: %w", err)
	}
	defer file.Close()
	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return fmt.Errorf("hash release artifact: %w", err)
	}
	if expectedSize > 0 && size != expectedSize {
		return fmt.Errorf("release artifact size mismatch: got %d want %d", size, expectedSize)
	}
	expected := strings.TrimPrefix(expectedSHA256, "sha256:")
	if got := hex.EncodeToString(hasher.Sum(nil)); !strings.EqualFold(got, expected) {
		return fmt.Errorf("release artifact sha256 mismatch")
	}
	return nil
}

func copyFile(src, dest string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create destination parent: %w", err)
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("create destination file: %w", err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy file: %w", err)
	}
	return nil
}

func LoadManifestBytes(path string) (noderuntime.ReleaseManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return noderuntime.ReleaseManifest{}, fmt.Errorf("read release manifest: %w", err)
	}
	var manifest noderuntime.ReleaseManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return noderuntime.ReleaseManifest{}, fmt.Errorf("parse release manifest: %w", err)
	}
	return manifest, nil
}
