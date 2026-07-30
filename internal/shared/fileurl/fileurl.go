package fileurl

import (
	"fmt"
	"net/url"
	slashpath "path"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

// ToPath converts a parsed file URL back into a local OS path,
// handling Windows drive paths (file:///C:/x) and UNC hosts.
func ToPath(parsed *url.URL) string {
	return toPathFor(goruntime.GOOS, parsed)
}

func toPathFor(goos string, parsed *url.URL) string {
	if parsed.Host != "" && parsed.Host != "localhost" {
		if goos == "windows" {
			return `\\` + parsed.Host + filepath.FromSlash(parsed.Path)
		}
		return filepath.Join(string(filepath.Separator)+parsed.Host, filepath.FromSlash(parsed.Path))
	}
	path := parsed.Path
	if goos == "windows" && len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return filepath.FromSlash(path)
}

// IsLocalRawURL reports whether a raw artifact reference should be
// treated as a local OS path rather than a network URL. Bare paths
// have no scheme; Windows drive paths parse with a one-letter scheme.
func IsLocalRawURL(parsed *url.URL) bool {
	return parsed.Scheme == "" || len(parsed.Scheme) == 1
}

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
