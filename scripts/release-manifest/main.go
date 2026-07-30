package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	noderuntime "github.com/Ani-HQ/thirdshift/internal/node/runtime"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	version := flag.String("version", "", "release version")
	buildID := flag.String("build-id", "", "build id or commit")
	distDir := flag.String("dist", "dist", "directory containing release zip files")
	baseURL := flag.String("base-url", "", "download base URL")
	outPath := flag.String("out", "dist/release-manifest.json", "manifest output path")
	signingKey := flag.String("signing-key", os.Getenv("RELEASE_SIGNING_KEY"), "base64 Ed25519 private key or seed")
	keyID := flag.String("key-id", os.Getenv("RELEASE_SIGNING_KEY_ID"), "release signing key id")
	flag.Parse()

	if *version == "" {
		return fmt.Errorf("--version is required")
	}
	if *buildID == "" {
		return fmt.Errorf("--build-id is required")
	}
	if *baseURL == "" {
		return fmt.Errorf("--base-url is required")
	}
	artifacts, err := releaseArtifacts(*distDir, strings.TrimRight(*baseURL, "/"))
	if err != nil {
		return err
	}
	manifest := noderuntime.ReleaseManifest{
		SchemaVersion: 1,
		Engine:        "thirdshift",
		Version:       *version,
		BuildID:       *buildID,
		Artifacts:     artifacts,
		Signature:     noderuntime.ManifestSignature{KeyID: firstNonEmpty(*keyID, "unsigned")},
	}
	if *signingKey != "" {
		privateKey, err := decodePrivateKey(*signingKey)
		if err != nil {
			return err
		}
		body, err := manifest.UnsignedBytes()
		if err != nil {
			return err
		}
		manifest.Signature.Value = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, body))
	} else {
		fmt.Fprintln(os.Stderr, "warning: RELEASE_SIGNING_KEY is unset; release manifest will be unsigned")
	}
	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(*outPath, data, 0o644)
}

func releaseArtifacts(distDir, baseURL string) (map[string]noderuntime.RuntimeArtifact, error) {
	entries, err := os.ReadDir(distDir)
	if err != nil {
		return nil, fmt.Errorf("read dist dir: %w", err)
	}
	artifacts := make(map[string]noderuntime.RuntimeArtifact)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "thirdshift-") || !strings.HasSuffix(entry.Name(), ".zip") {
			continue
		}
		platformKey, executablePath, ok := platformFromArtifact(entry.Name())
		if !ok {
			continue
		}
		path := filepath.Join(distDir, entry.Name())
		sum, size, err := fileSHA256(path)
		if err != nil {
			return nil, err
		}
		artifacts[platformKey] = noderuntime.RuntimeArtifact{
			URL:            baseURL + "/" + entry.Name(),
			SHA256:         "sha256:" + sum,
			SizeBytes:      size,
			ArchiveType:    "zip",
			ExecutablePath: executablePath,
		}
	}
	if len(artifacts) == 0 {
		return nil, fmt.Errorf("no thirdshift-*.zip artifacts found in %s", distDir)
	}
	return artifacts, nil
}

func platformFromArtifact(name string) (string, string, bool) {
	trimmed := strings.TrimSuffix(strings.TrimPrefix(name, "thirdshift-"), ".zip")
	parts := strings.Split(trimmed, "-")
	if len(parts) != 2 {
		return "", "", false
	}
	executable := "thirdshift"
	if parts[0] == "windows" {
		executable = "thirdshift.exe"
	}
	return parts[0] + "/" + parts[1], executable, true
}

func fileSHA256(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("open artifact: %w", err)
	}
	defer file.Close()
	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return "", 0, fmt.Errorf("hash artifact: %w", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), size, nil
}

func decodePrivateKey(encoded string) (ed25519.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode release signing key: %w", err)
	}
	switch len(raw) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(raw), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(raw), nil
	default:
		return nil, fmt.Errorf("release signing key has %d bytes, want %d-byte seed or %d-byte private key", len(raw), ed25519.SeedSize, ed25519.PrivateKeySize)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
