package update

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	noderuntime "github.com/Ani-HQ/thirdshift/internal/node/runtime"
)

func TestUpdateVerifiesManifestPromotesAndRetainsRollback(t *testing.T) {
	publicKey, privateKey := testKeypair(t)
	zipBody := testZip(t, "thirdshift", []byte("new binary"))
	manifest := testManifest(t, privateKey, "darwin/arm64", "thirdshift.zip", "thirdshift", zipBody)
	server := testReleaseServer(t, manifest, zipBody)
	defer server.Close()

	installDir := t.TempDir()
	current := filepath.Join(installDir, "thirdshift")
	if err := os.WriteFile(current, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("write current binary: %v", err)
	}

	result, err := (Manager{
		CurrentBinary: current,
		PublicKey:     publicKey,
		PlatformKey:   "darwin/arm64",
		HTTPClient:    server.Client(),
	}).Update(context.Background(), server.URL+"/release-manifest.json")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if result.Version != "0.1.0-alpha-test" || result.PreviousPath == "" {
		t.Fatalf("bad update result: %#v", result)
	}
	currentBody, err := os.ReadFile(current)
	if err != nil {
		t.Fatalf("read current binary: %v", err)
	}
	if string(currentBody) != "new binary" {
		t.Fatalf("current body = %q, want new binary", string(currentBody))
	}
	previousBody, err := os.ReadFile(result.PreviousPath)
	if err != nil {
		t.Fatalf("read previous binary: %v", err)
	}
	if string(previousBody) != "old binary" {
		t.Fatalf("previous body = %q, want old binary", string(previousBody))
	}
}

func TestUpdateRejectsHashMismatchAndLeavesCurrent(t *testing.T) {
	publicKey, privateKey := testKeypair(t)
	zipBody := testZip(t, "thirdshift", []byte("new binary"))
	manifest := testManifest(t, privateKey, "darwin/arm64", "thirdshift.zip", "thirdshift", zipBody)
	manifest.Artifacts["darwin/arm64"] = noderuntime.RuntimeArtifact{
		URL:            "thirdshift.zip",
		SHA256:         "sha256:" + stringsRepeat("0", 64),
		SizeBytes:      int64(len(zipBody)),
		ArchiveType:    "zip",
		ExecutablePath: "thirdshift",
	}
	signManifest(t, privateKey, &manifest)
	server := testReleaseServer(t, manifest, zipBody)
	defer server.Close()

	installDir := t.TempDir()
	current := filepath.Join(installDir, "thirdshift")
	if err := os.WriteFile(current, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("write current binary: %v", err)
	}
	_, err := (Manager{
		CurrentBinary: current,
		PublicKey:     publicKey,
		PlatformKey:   "darwin/arm64",
		HTTPClient:    server.Client(),
	}).Update(context.Background(), server.URL+"/release-manifest.json")
	if err == nil {
		t.Fatal("update succeeded with bad artifact hash")
	}
	currentBody, err := os.ReadFile(current)
	if err != nil {
		t.Fatalf("read current binary: %v", err)
	}
	if string(currentBody) != "old binary" {
		t.Fatalf("current body changed after rejected update: %q", string(currentBody))
	}
}

func TestVerifyDownloadedArtifactRejectsBadSignature(t *testing.T) {
	publicKey, privateKey := testKeypair(t)
	zipBody := testZip(t, "thirdshift", []byte("new binary"))
	manifest := testManifest(t, privateKey, "darwin/arm64", "thirdshift.zip", "thirdshift", zipBody)
	manifest.Signature.Value = base64.StdEncoding.EncodeToString([]byte("bad signature"))
	artifactPath := filepath.Join(t.TempDir(), "thirdshift.zip")
	if err := os.WriteFile(artifactPath, zipBody, 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := VerifyDownloadedArtifact(manifest, "darwin/arm64", artifactPath, publicKey); err == nil {
		t.Fatal("verification succeeded with bad manifest signature")
	}
}

func testKeypair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	return publicKey, privateKey
}

func testManifest(t *testing.T, privateKey ed25519.PrivateKey, platformKey, artifactURL, executablePath string, artifactBody []byte) noderuntime.ReleaseManifest {
	t.Helper()
	sum := sha256.Sum256(artifactBody)
	manifest := noderuntime.ReleaseManifest{
		SchemaVersion: 1,
		Engine:        "thirdshift",
		Version:       "0.1.0-alpha-test",
		BuildID:       "test-build",
		Artifacts: map[string]noderuntime.RuntimeArtifact{
			platformKey: {
				URL:            artifactURL,
				SHA256:         "sha256:" + hex.EncodeToString(sum[:]),
				SizeBytes:      int64(len(artifactBody)),
				ArchiveType:    "zip",
				ExecutablePath: executablePath,
			},
		},
		Signature: noderuntime.ManifestSignature{KeyID: "test"},
	}
	signManifest(t, privateKey, &manifest)
	return manifest
}

func signManifest(t *testing.T, privateKey ed25519.PrivateKey, manifest *noderuntime.ReleaseManifest) {
	t.Helper()
	body, err := manifest.UnsignedBytes()
	if err != nil {
		t.Fatalf("unsigned manifest: %v", err)
	}
	manifest.Signature.Value = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, body))
}

func testReleaseServer(t *testing.T, manifest noderuntime.ReleaseManifest, artifact []byte) *httptest.Server {
	t.Helper()
	manifestBody, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release-manifest.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(manifestBody)
		case "/thirdshift.zip":
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(artifact)
		default:
			http.NotFound(w, r)
		}
	}))
}

func testZip(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	file, err := writer.Create(name)
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := file.Write(body); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func stringsRepeat(value string, count int) string {
	var buf bytes.Buffer
	for idx := 0; idx < count; idx++ {
		buf.WriteString(value)
	}
	return buf.String()
}
