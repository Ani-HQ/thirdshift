package runtime

import (
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
	"time"
)

type Manager struct {
	Root        string
	PublicKey   ed25519.PublicKey
	PlatformKey string
	HTTPClient  *http.Client
}

type InstalledRuntime struct {
	Engine         string `json:"engine"`
	Version        string `json:"version"`
	BuildID        string `json:"build_id"`
	PlatformKey    string `json:"platform_key"`
	Path           string `json:"path"`
	ExecutablePath string `json:"executable_path"`
	SHA256         string `json:"sha256"`
	InstalledAt    string `json:"installed_at"`
}

func (m Manager) Ensure(ctx context.Context, manifest ReleaseManifest) (InstalledRuntime, error) {
	if m.Root == "" {
		return InstalledRuntime{}, fmt.Errorf("runtime root is required")
	}
	if err := manifest.VerifySignature(m.PublicKey); err != nil {
		return InstalledRuntime{}, err
	}

	platformKey := m.PlatformKey
	if platformKey == "" {
		platformKey = goruntime.GOOS + "/" + goruntime.GOARCH
	}
	artifact, ok := manifest.Artifacts[platformKey]
	if !ok {
		return InstalledRuntime{}, fmt.Errorf("runtime artifact for platform %s is not in manifest", platformKey)
	}
	if artifact.ExecutablePath == "" {
		return InstalledRuntime{}, fmt.Errorf("runtime artifact for %s has no executable_path", platformKey)
	}

	targetDir := filepath.Join(m.Root, safePathPart(manifest.Engine), safePathPart(manifest.Version), safePathPart(platformKey))
	if installed, ok := readInstalled(targetDir); ok && installed.SHA256 == artifact.SHA256 {
		_ = writeJSON(filepath.Join(m.Root, "current.json"), installed)
		return installed, nil
	}

	if err := os.MkdirAll(m.Root, 0o755); err != nil {
		return InstalledRuntime{}, fmt.Errorf("create runtime root: %w", err)
	}
	tmpDir, err := os.MkdirTemp(m.Root, ".install-*")
	if err != nil {
		return InstalledRuntime{}, fmt.Errorf("create runtime temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	downloadPath := filepath.Join(tmpDir, "artifact")
	if err := downloadFile(ctx, m.client(), artifact.URL, downloadPath); err != nil {
		return InstalledRuntime{}, err
	}
	if err := verifyFile(downloadPath, artifact.SHA256, artifact.SizeBytes); err != nil {
		return InstalledRuntime{}, err
	}
	extractDir := filepath.Join(tmpDir, "extract")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return InstalledRuntime{}, fmt.Errorf("create extract dir: %w", err)
	}
	if artifact.ArchiveType == "" || artifact.ArchiveType == "binary" {
		if err := copyBinaryArtifact(downloadPath, filepath.Join(extractDir, filepath.FromSlash(artifact.ExecutablePath))); err != nil {
			return InstalledRuntime{}, err
		}
	} else if err := extractArtifact(downloadPath, artifact.ArchiveType, extractDir); err != nil {
		return InstalledRuntime{}, err
	}
	for idx, support := range artifact.SupportArtifacts {
		supportPath := filepath.Join(tmpDir, fmt.Sprintf("support-%d", idx))
		if err := downloadFile(ctx, m.client(), support.URL, supportPath); err != nil {
			return InstalledRuntime{}, err
		}
		if err := verifyFile(supportPath, support.SHA256, support.SizeBytes); err != nil {
			return InstalledRuntime{}, err
		}
		if support.ArchiveType == "" || support.ArchiveType == "binary" {
			name := support.Name
			if name == "" {
				name = filepath.Base(supportPath)
			}
			if err := copyBinaryArtifact(supportPath, filepath.Join(extractDir, filepath.Base(name))); err != nil {
				return InstalledRuntime{}, err
			}
		} else if err := extractArtifact(supportPath, support.ArchiveType, extractDir); err != nil {
			return InstalledRuntime{}, err
		}
	}
	execPath := filepath.Join(extractDir, filepath.FromSlash(artifact.ExecutablePath))
	if _, err := os.Stat(execPath); err != nil {
		return InstalledRuntime{}, fmt.Errorf("runtime executable %s not found in artifact: %w", artifact.ExecutablePath, err)
	}
	if goruntime.GOOS != "windows" {
		if err := os.Chmod(execPath, 0o755); err != nil {
			return InstalledRuntime{}, fmt.Errorf("mark runtime executable: %w", err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(targetDir), 0o755); err != nil {
		return InstalledRuntime{}, fmt.Errorf("create runtime target parent: %w", err)
	}
	if err := os.RemoveAll(targetDir); err != nil {
		return InstalledRuntime{}, fmt.Errorf("replace runtime target: %w", err)
	}
	if err := os.Rename(extractDir, targetDir); err != nil {
		return InstalledRuntime{}, fmt.Errorf("promote runtime atomically: %w", err)
	}

	installed := InstalledRuntime{
		Engine:         manifest.Engine,
		Version:        manifest.Version,
		BuildID:        manifest.BuildID,
		PlatformKey:    platformKey,
		Path:           targetDir,
		ExecutablePath: filepath.Join(targetDir, filepath.FromSlash(artifact.ExecutablePath)),
		SHA256:         artifact.SHA256,
		InstalledAt:    time.Now().UTC().Format(time.RFC3339),
	}
	if err := writeJSON(filepath.Join(targetDir, ".thirdshift-runtime.json"), installed); err != nil {
		return InstalledRuntime{}, err
	}
	if current, ok := readCurrent(m.Root); ok && current.Path != installed.Path {
		if err := writeJSON(filepath.Join(m.Root, "previous.json"), current); err != nil {
			return InstalledRuntime{}, err
		}
	}
	if err := writeJSON(filepath.Join(m.Root, "current.json"), installed); err != nil {
		return InstalledRuntime{}, err
	}
	return installed, nil
}

func (m Manager) Rollback() (InstalledRuntime, error) {
	previous, ok := readInstalledFile(filepath.Join(m.Root, "previous.json"))
	if !ok {
		return InstalledRuntime{}, fmt.Errorf("no previous runtime is available for rollback")
	}
	current, hasCurrent := readCurrent(m.Root)
	if hasCurrent {
		if err := writeJSON(filepath.Join(m.Root, "previous.json"), current); err != nil {
			return InstalledRuntime{}, err
		}
	}
	if err := writeJSON(filepath.Join(m.Root, "current.json"), previous); err != nil {
		return InstalledRuntime{}, err
	}
	return previous, nil
}

func readInstalled(dir string) (InstalledRuntime, bool) {
	return readInstalledFile(filepath.Join(dir, ".thirdshift-runtime.json"))
}

func readCurrent(root string) (InstalledRuntime, bool) {
	return readInstalledFile(filepath.Join(root, "current.json"))
}

func readInstalledFile(path string) (InstalledRuntime, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return InstalledRuntime{}, false
	}
	var installed InstalledRuntime
	if err := json.Unmarshal(data, &installed); err != nil {
		return InstalledRuntime{}, false
	}
	if installed.ExecutablePath == "" {
		return InstalledRuntime{}, false
	}
	return installed, true
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func downloadFile(ctx context.Context, client *http.Client, rawurl, dest string) error {
	parsed, err := url.Parse(rawurl)
	if err == nil && parsed.Scheme == "file" {
		return copyLocalFile(parsed.Path, dest)
	}
	if err == nil && parsed.Scheme == "" {
		return copyLocalFile(rawurl, dest)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return fmt.Errorf("create runtime download request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download runtime artifact: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download runtime artifact: HTTP %d", resp.StatusCode)
	}
	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create runtime artifact temp file: %w", err)
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("write runtime artifact temp file: %w", err)
	}
	return nil
}

func copyLocalFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open local runtime artifact: %w", err)
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create runtime artifact temp file: %w", err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy local runtime artifact: %w", err)
	}
	return nil
}

func verifyFile(path, expectedSHA256 string, expectedSize int64) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open artifact for verification: %w", err)
	}
	defer file.Close()
	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return fmt.Errorf("hash artifact: %w", err)
	}
	if expectedSize > 0 && size != expectedSize {
		return fmt.Errorf("artifact size = %d, want %d", size, expectedSize)
	}
	actual := hex.EncodeToString(hasher.Sum(nil))
	expected := strings.TrimPrefix(expectedSHA256, "sha256:")
	if actual != expected {
		return fmt.Errorf("artifact sha256 = %s, want %s", actual, expected)
	}
	return nil
}

func (m Manager) client() *http.Client {
	if m.HTTPClient != nil {
		return m.HTTPClient
	}
	return http.DefaultClient
}

func safePathPart(value string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "..", "_")
	return replacer.Replace(value)
}
