package fileurl

import (
	"fmt"
	"net/url"
	slashpath "path"
	"path/filepath"
	"strings"
)

// FromPath converts a local OS path into a standards-compliant file URL.
func FromPath(filename string) (string, error) {
	if strings.TrimSpace(filename) == "" {
		return "", fmt.Errorf("file path is required")
	}

	normalized := strings.ReplaceAll(filename, "\\", "/")
	if isWindowsDrivePath(normalized) {
		return format("file", "", "/"+slashpath.Clean(normalized)), nil
	}
	if strings.HasPrefix(normalized, "//") {
		withoutPrefix := strings.TrimLeft(normalized, "/")
		parts := strings.SplitN(withoutPrefix, "/", 2)
		if parts[0] == "" {
			return "", fmt.Errorf("file path %q has an empty UNC host", filename)
		}
		pathPart := "/"
		if len(parts) == 2 && parts[1] != "" {
			pathPart = "/" + slashpath.Clean(parts[1])
		}
		return format("file", parts[0], pathPart), nil
	}

	if !strings.HasPrefix(normalized, "/") {
		absolute, err := filepath.Abs(filename)
		if err != nil {
			return "", fmt.Errorf("absolute file path: %w", err)
		}
		normalized = strings.ReplaceAll(filepath.ToSlash(absolute), "\\", "/")
		if isWindowsDrivePath(normalized) {
			return format("file", "", "/"+slashpath.Clean(normalized)), nil
		}
	}
	if !strings.HasPrefix(normalized, "/") {
		normalized = "/" + normalized
	}
	return format("file", "", slashpath.Clean(normalized)), nil
}

func format(scheme, host, path string) string {
	u := url.URL{Scheme: scheme, Host: host, Path: path}
	return u.String()
}

func isWindowsDrivePath(filename string) bool {
	if len(filename) < 3 || filename[1] != ':' || filename[2] != '/' {
		return false
	}
	return (filename[0] >= 'A' && filename[0] <= 'Z') || (filename[0] >= 'a' && filename[0] <= 'z')
}
