package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestModelCacheResumesInterruptedDownload(t *testing.T) {
	body := []byte(strings.Repeat("model-data-", 1024))
	sum := sha256.Sum256(body)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := requests.Add(1)
		if request == 1 {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
			_, _ = w.Write(body[:len(body)/2])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			return
		}
		if got := r.Header.Get("Range"); got != fmt.Sprintf("bytes=%d-", len(body)/2) {
			t.Fatalf("Range = %q, want resume from first partial size", got)
		}
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body[len(body)/2:])
	}))
	defer server.Close()

	cache := Cache{Dir: t.TempDir(), QuotaBytes: int64(len(body) * 2), HTTPClient: server.Client()}
	artifact := Artifact{URL: server.URL, SHA256: hex.EncodeToString(sum[:]), SizeBytes: int64(len(body))}
	if _, err := cache.Ensure(context.Background(), artifact, artifact.SHA256); err == nil {
		t.Fatal("first interrupted download succeeded, want incomplete error")
	}
	path, err := cache.Ensure(context.Background(), artifact, artifact.SHA256)
	if err != nil {
		t.Fatalf("resume download: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cached model: %v", err)
	}
	if string(data) != string(body) {
		t.Fatal("cached model contents mismatch")
	}
}

func TestModelCacheLRUEvictionKeepsActiveModel(t *testing.T) {
	dir := t.TempDir()
	activeHash := writeCachedModel(t, dir, "active", []byte("active-model"))
	oldHash := writeCachedModel(t, dir, "old", []byte("old-model"))
	newBody := []byte("new-model")
	newSum := sha256.Sum256(newBody)
	source := filepath.Join(t.TempDir(), "new.gguf")
	if err := os.WriteFile(source, newBody, 0o644); err != nil {
		t.Fatalf("write source model: %v", err)
	}

	oldTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(filepath.Join(dir, oldHash+".gguf"), oldTime, oldTime); err != nil {
		t.Fatalf("chtimes old: %v", err)
	}
	activeTime := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(filepath.Join(dir, activeHash+".gguf"), activeTime, activeTime); err != nil {
		t.Fatalf("chtimes active: %v", err)
	}

	cache := Cache{Dir: dir, QuotaBytes: int64(len("active-model") + len(newBody))}
	_, err := cache.Ensure(context.Background(), Artifact{
		URL:       source,
		SHA256:    hex.EncodeToString(newSum[:]),
		SizeBytes: int64(len(newBody)),
	}, activeHash)
	if err != nil {
		t.Fatalf("ensure new model: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, activeHash+".gguf")); err != nil {
		t.Fatalf("active model was evicted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, oldHash+".gguf")); !os.IsNotExist(err) {
		t.Fatalf("old model still exists, want eviction")
	}
}

func writeCachedModel(t *testing.T, dir, label string, body []byte) string {
	t.Helper()
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	if err := os.WriteFile(filepath.Join(dir, hash+".gguf"), body, 0o644); err != nil {
		t.Fatalf("write %s model: %v", label, err)
	}
	return hash
}

func TestEnsureLicenseWritesTextNextToModel(t *testing.T) {
	dir := t.TempDir()
	cache := Cache{Dir: dir}
	path, err := cache.EnsureLicense("sha256:feedface", "LICENSE BODY")
	if err != nil {
		t.Fatalf("ensure license: %v", err)
	}
	if filepath.Base(path) != "feedface.LICENSE.txt" {
		t.Fatalf("license path = %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "LICENSE BODY" {
		t.Fatalf("license content = %q err=%v", data, err)
	}
	if _, err := cache.EnsureLicense("feedface", "LICENSE BODY"); err != nil {
		t.Fatalf("idempotent ensure failed: %v", err)
	}
	if _, err := cache.EnsureLicense("", "text"); err == nil {
		t.Fatal("empty sha accepted")
	}
	if _, err := cache.EnsureLicense("feedface", ""); err == nil {
		t.Fatal("empty license text accepted")
	}
}
