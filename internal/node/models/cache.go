package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Cache struct {
	Dir        string
	QuotaBytes int64
	HTTPClient *http.Client
}

type Artifact struct {
	URL       string
	SHA256    string
	SizeBytes int64
}

func (c Cache) Ensure(ctx context.Context, artifact Artifact, activeSHA256 string) (string, error) {
	if c.Dir == "" {
		return "", fmt.Errorf("model cache directory is required")
	}
	sha := strings.TrimPrefix(artifact.SHA256, "sha256:")
	if sha == "" {
		return "", fmt.Errorf("model artifact sha256 is required")
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return "", fmt.Errorf("create model cache directory: %w", err)
	}
	finalPath := filepath.Join(c.Dir, sha+".gguf")
	if err := verifyExisting(finalPath, sha, artifact.SizeBytes); err == nil {
		now := time.Now()
		_ = os.Chtimes(finalPath, now, now)
		return finalPath, c.evict(activeSHA256)
	}

	partialPath := filepath.Join(c.Dir, sha+".partial")
	if err := c.downloadResumable(ctx, artifact.URL, partialPath, artifact.SizeBytes); err != nil {
		return "", err
	}
	if err := verifyExisting(partialPath, sha, artifact.SizeBytes); err != nil {
		return "", err
	}
	if err := os.Rename(partialPath, finalPath); err != nil {
		return "", fmt.Errorf("promote model artifact: %w", err)
	}
	now := time.Now()
	_ = os.Chtimes(finalPath, now, now)
	return finalPath, c.evict(activeSHA256)
}

func (c Cache) downloadResumable(ctx context.Context, rawurl, dest string, expectedSize int64) error {
	parsed, err := url.Parse(rawurl)
	if err == nil && parsed.Scheme == "file" {
		return copyModelFile(parsed.Path, dest)
	}
	if err == nil && parsed.Scheme == "" {
		return copyModelFile(rawurl, dest)
	}

	var offset int64
	if info, err := os.Stat(dest); err == nil {
		offset = info.Size()
	}

	flags := os.O_CREATE | os.O_WRONLY
	if offset > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	out, err := os.OpenFile(dest, flags, 0o644)
	if err != nil {
		return fmt.Errorf("open partial model: %w", err)
	}
	defer out.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return fmt.Errorf("create model download request: %w", err)
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return fmt.Errorf("download model artifact: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case offset > 0 && resp.StatusCode == http.StatusPartialContent:
	case offset > 0 && resp.StatusCode == http.StatusOK:
		if err := out.Truncate(0); err != nil {
			return fmt.Errorf("restart partial model: %w", err)
		}
		if _, err := out.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("seek partial model: %w", err)
		}
		offset = 0
	case offset == 0 && resp.StatusCode == http.StatusOK:
	default:
		return fmt.Errorf("download model artifact: HTTP %d", resp.StatusCode)
	}

	written, err := io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("write model artifact: %w", err)
	}
	if expectedSize > 0 && offset+written != expectedSize {
		return fmt.Errorf("model artifact is incomplete: got %d bytes, want %d", offset+written, expectedSize)
	}
	return nil
}

func (c Cache) evict(activeSHA256 string) error {
	if c.QuotaBytes <= 0 {
		return nil
	}
	activeSHA256 = strings.TrimPrefix(activeSHA256, "sha256:")
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		return err
	}
	type cacheFile struct {
		path  string
		name  string
		size  int64
		atime time.Time
	}
	var files []cacheFile
	var total int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".gguf") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		files = append(files, cacheFile{
			path:  filepath.Join(c.Dir, entry.Name()),
			name:  strings.TrimSuffix(entry.Name(), ".gguf"),
			size:  info.Size(),
			atime: info.ModTime(),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].atime.Before(files[j].atime)
	})
	for _, file := range files {
		if total <= c.QuotaBytes {
			return nil
		}
		if file.name == activeSHA256 {
			continue
		}
		if err := os.Remove(file.path); err != nil {
			return fmt.Errorf("evict model %s: %w", file.name, err)
		}
		total -= file.size
	}
	return nil
}

func verifyExisting(path, expectedSHA256 string, expectedSize int64) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return err
	}
	if expectedSize > 0 && size != expectedSize {
		return fmt.Errorf("size = %d, want %d", size, expectedSize)
	}
	actual := hex.EncodeToString(hasher.Sum(nil))
	if actual != strings.TrimPrefix(expectedSHA256, "sha256:") {
		return fmt.Errorf("sha256 = %s, want %s", actual, expectedSHA256)
	}
	return nil
}

func copyModelFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open local model artifact: %w", err)
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create partial model: %w", err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy local model artifact: %w", err)
	}
	return nil
}

func (c Cache) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}
