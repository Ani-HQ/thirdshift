//go:build slice

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/anianroid/thirdshift/internal/node/local"
	noderuntime "github.com/anianroid/thirdshift/internal/node/runtime"
	"github.com/anianroid/thirdshift/internal/node/runtime/fakellama"
)

func TestMain(m *testing.M) {
	if os.Getenv("THIRDSHIFT_FAKE_LLAMA_SERVER") == "1" {
		os.Exit(fakellama.Main(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func TestRunLocalSliceWithFakeRuntime(t *testing.T) {
	modelBytes := []byte("fake gguf model")
	modelSum := sha256.Sum256(modelBytes)
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "model.gguf", time.Now(), bytes.NewReader(modelBytes))
	}))
	defer modelServer.Close()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate runtime key: %v", err)
	}

	catalogDir := t.TempDir()
	runtimeManifest := signedSliceRuntimeManifest(t, privateKey)
	runtimeManifestPath := filepath.Join(catalogDir, "fake-runtime.json")
	writeJSON(t, runtimeManifestPath, runtimeManifest)
	modelManifest := fmt.Sprintf(`schema_version: 1
model_id: thirdshift-slice-chat-v1
display_name: Thirdshift Slice Chat v1
status: alpha
source:
  provider: test
  repository: local/fake
  revision: test
  file: model.gguf
  url: %s
  sha256: %s
  size_bytes: %d
license:
  identifier: apache-2.0
  reviewed_at: 2026-07-29
  notes: test fixture
runtime:
  engine: llama.cpp
  build_id: fake-runtime
  release_manifest: fake-runtime.json
  binary_sha256: fake
  arguments:
    context_size: 128
    batch_size: 16
    gpu_layers: 0
    parallel: 1
    host: 127.0.0.1
    port: dynamic
hardware:
  min_vram_mb: 0
  min_ram_mb: 0
  min_disk_mb: 0
  eligible_gpu_classes:
    - test
limits:
  max_input_tokens: 64
  max_output_tokens: 16
  max_request_bytes: 4096
`, modelServer.URL, hex.EncodeToString(modelSum[:]), len(modelBytes))
	if err := os.WriteFile(filepath.Join(catalogDir, "thirdshift-slice-chat-v1.yaml"), []byte(modelManifest), 0o644); err != nil {
		t.Fatalf("write model manifest: %v", err)
	}

	var output bytes.Buffer
	err = local.Run(context.Background(), local.RunOptions{
		ModelID:          "thirdshift-slice-chat-v1",
		Prompt:           "hello slice",
		CatalogDir:       catalogDir,
		DataDir:          t.TempDir(),
		RuntimePublicKey: publicKey,
		RuntimeEnv:       []string{"THIRDSHIFT_FAKE_LLAMA_SERVER=1"},
		Output:           &output,
		StartupTimeout:   2 * time.Second,
		ShutdownTimeout:  2 * time.Second,
		HealthInterval:   20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("run local slice: %v", err)
	}
	got := output.String()
	if !strings.Contains(got, "fake completion: hello slice") {
		t.Fatalf("completion output missing fake response: %s", got)
	}
	if !strings.Contains(got, "usage: prompt_tokens=3 completion_tokens=5 total_tokens=8") {
		t.Fatalf("usage output missing: %s", got)
	}
}

func signedSliceRuntimeManifest(t *testing.T, privateKey ed25519.PrivateKey) noderuntime.ReleaseManifest {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("test executable: %v", err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatalf("read test executable: %v", err)
	}
	sum := sha256.Sum256(data)
	manifest := noderuntime.ReleaseManifest{
		SchemaVersion: 1,
		Engine:        "llama.cpp",
		Version:       "fake-slice",
		BuildID:       "fake-slice",
		Artifacts: map[string]noderuntime.RuntimeArtifact{
			goruntime.GOOS + "/" + goruntime.GOARCH: {
				URL:            "file://" + filepath.ToSlash(executable),
				SHA256:         "sha256:" + hex.EncodeToString(sum[:]),
				SizeBytes:      int64(len(data)),
				ArchiveType:    "binary",
				ExecutablePath: "fake-llama-server",
			},
		},
		Signature: noderuntime.ManifestSignature{KeyID: "slice-test"},
	}
	body, err := manifest.UnsignedBytes()
	if err != nil {
		t.Fatalf("unsigned runtime manifest: %v", err)
	}
	manifest.Signature.Value = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, body))
	return manifest
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write json: %v", err)
	}
}
