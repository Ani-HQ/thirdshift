package runtime

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeManagerRejectsTamperedBinary(t *testing.T) {
	publicKey, privateKey := testKey(t)
	artifact := writeBinaryArtifact(t, []byte("good binary"))
	manifest := signedManifest(t, privateKey, "v1", artifact, "sha256:"+hex.EncodeToString(sha256Bytes([]byte("different"))))

	manager := Manager{Root: t.TempDir(), PublicKey: publicKey, PlatformKey: "test/os"}
	if _, err := manager.Ensure(context.Background(), manifest); err == nil {
		t.Fatal("tampered binary installed successfully, want error")
	}
}

func TestRuntimeManagerRejectsBadSignature(t *testing.T) {
	publicKey, privateKey := testKey(t)
	artifact := writeBinaryArtifact(t, []byte("good binary"))
	sum := "sha256:" + hex.EncodeToString(sha256Bytes([]byte("good binary")))
	manifest := signedManifest(t, privateKey, "v1", artifact, sum)
	manifest.Signature.Value = base64.StdEncoding.EncodeToString([]byte("bad signature"))

	manager := Manager{Root: t.TempDir(), PublicKey: publicKey, PlatformKey: "test/os"}
	if _, err := manager.Ensure(context.Background(), manifest); err == nil {
		t.Fatal("bad signature installed successfully, want error")
	}
}

func TestRuntimeManagerRollback(t *testing.T) {
	publicKey, privateKey := testKey(t)
	root := t.TempDir()
	manager := Manager{Root: root, PublicKey: publicKey, PlatformKey: "test/os"}

	v1Artifact := writeTarGzRuntime(t, "runtime v1")
	v1 := unsignedManifest("v1", v1Artifact.path, "sha256:"+v1Artifact.sha256)
	v1RuntimeArtifact := v1.Artifacts["test/os"]
	v1RuntimeArtifact.ArchiveType = "tar.gz"
	v1RuntimeArtifact.ExecutablePath = "runtime-v1/llama-server"
	v1.Artifacts["test/os"] = v1RuntimeArtifact
	signManifest(t, &v1, privateKey)
	installedV1, err := manager.Ensure(context.Background(), v1)
	if err != nil {
		t.Fatalf("install v1: %v", err)
	}

	v2Artifact := writeTarGzRuntime(t, "runtime v2")
	v2 := unsignedManifest("v2", v2Artifact.path, "sha256:"+v2Artifact.sha256)
	v2RuntimeArtifact := v2.Artifacts["test/os"]
	v2RuntimeArtifact.ArchiveType = "tar.gz"
	v2RuntimeArtifact.ExecutablePath = "runtime-v1/llama-server"
	v2.Artifacts["test/os"] = v2RuntimeArtifact
	signManifest(t, &v2, privateKey)
	installedV2, err := manager.Ensure(context.Background(), v2)
	if err != nil {
		t.Fatalf("install v2: %v", err)
	}
	if installedV2.Version != "v2" {
		t.Fatalf("current version = %s, want v2", installedV2.Version)
	}

	rolledBack, err := manager.Rollback()
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if rolledBack.Version != installedV1.Version {
		t.Fatalf("rolled back to %s, want %s", rolledBack.Version, installedV1.Version)
	}
	current, ok := readCurrent(root)
	if !ok {
		t.Fatal("current runtime metadata missing")
	}
	if current.Path != installedV1.Path {
		t.Fatalf("current path = %s, want %s", current.Path, installedV1.Path)
	}
}

func TestPinnedRuntimeManifestVerifies(t *testing.T) {
	manifest, err := LoadReleaseManifest("../../../models/catalog/llama-cpp-b10180.runtime.json")
	if err != nil {
		t.Fatalf("load pinned runtime manifest: %v", err)
	}
	key, err := DefaultRuntimePublicKey()
	if err != nil {
		t.Fatalf("decode default public key: %v", err)
	}
	if err := manifest.VerifySignature(key); err != nil {
		t.Fatalf("verify pinned runtime manifest: %v", err)
	}
	if _, ok := manifest.Artifacts["darwin/arm64"]; !ok {
		t.Fatal("pinned runtime manifest missing darwin/arm64 artifact")
	}
	if _, ok := manifest.Artifacts["windows/amd64"]; !ok {
		t.Fatal("pinned runtime manifest missing windows/amd64 artifact")
	}
}

type artifactFile struct {
	path   string
	sha256 string
	size   int64
}

func testKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return publicKey, privateKey
}

func writeBinaryArtifact(t *testing.T, contents []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "llama-server")
	if err := os.WriteFile(path, contents, 0o755); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	return path
}

func writeTarGzRuntime(t *testing.T, contents string) artifactFile {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runtime.tar.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create tar.gz: %v", err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	body := []byte(contents)
	if err := tw.WriteHeader(&tar.Header{Name: "runtime-v1/llama-server", Mode: 0o755, Size: int64(len(body))}); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatalf("write tar body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read tar.gz: %v", err)
	}
	return artifactFile{path: path, sha256: hex.EncodeToString(sha256Bytes(data)), size: int64(len(data))}
}

func signedManifest(t *testing.T, privateKey ed25519.PrivateKey, version, artifactPath, sha string) ReleaseManifest {
	t.Helper()
	manifest := unsignedManifest(version, artifactPath, sha)
	signManifest(t, &manifest, privateKey)
	return manifest
}

func unsignedManifest(version, artifactPath, sha string) ReleaseManifest {
	info, err := os.Stat(artifactPath)
	if err != nil {
		panic(err)
	}
	return ReleaseManifest{
		SchemaVersion: 1,
		Engine:        "llama.cpp",
		Version:       version,
		BuildID:       "llama-cpp-" + version,
		Artifacts: map[string]RuntimeArtifact{
			"test/os": {
				URL:            fileURL(artifactPath),
				SHA256:         sha,
				SizeBytes:      info.Size(),
				ArchiveType:    "binary",
				ExecutablePath: filepath.Base(artifactPath),
			},
		},
		Signature: ManifestSignature{KeyID: "test"},
	}
}

func signManifest(t *testing.T, manifest *ReleaseManifest, privateKey ed25519.PrivateKey) {
	t.Helper()
	body, err := manifest.UnsignedBytes()
	if err != nil {
		t.Fatalf("unsigned bytes: %v", err)
	}
	manifest.Signature.Value = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, body))
}

func sha256Bytes(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

func fileURL(path string) string {
	return "file://" + filepath.ToSlash(path)
}
