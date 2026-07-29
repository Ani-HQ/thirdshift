package runtime

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
)

type rotatingLogWriter struct {
	path       string
	maxBytes   int64
	redactions []string
	mu         sync.Mutex
}

func newRotatingLogWriter(path string, maxBytes int64, redactions []string) (*rotatingLogWriter, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return &rotatingLogWriter{path: path, maxBytes: maxBytes, redactions: redactions}, nil
}

func (w *rotatingLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	redacted := append([]byte(nil), p...)
	for _, secret := range w.redactions {
		if secret == "" {
			continue
		}
		redacted = bytes.ReplaceAll(redacted, []byte(secret), []byte("[REDACTED]"))
	}
	if err := w.rotateIfNeeded(int64(len(redacted))); err != nil {
		return 0, err
	}
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	if _, err := file.Write(redacted); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *rotatingLogWriter) rotateIfNeeded(nextBytes int64) error {
	if w.maxBytes <= 0 {
		return nil
	}
	info, err := os.Stat(w.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Size()+nextBytes <= w.maxBytes {
		return nil
	}
	_ = os.Remove(w.path + ".1")
	return os.Rename(w.path, w.path+".1")
}
