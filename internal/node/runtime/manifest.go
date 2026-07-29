package runtime

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
)

const DefaultRuntimePublicKeyBase64 = "45PVSQThoHK/EAnCaRg4Iwhz+DrlM+lx8LrapJ2NtBA="

type ReleaseManifest struct {
	SchemaVersion int                        `json:"schema_version"`
	Engine        string                     `json:"engine"`
	Version       string                     `json:"version"`
	BuildID       string                     `json:"build_id"`
	Artifacts     map[string]RuntimeArtifact `json:"artifacts"`
	Signature     ManifestSignature          `json:"signature"`
}

type RuntimeArtifact struct {
	URL              string            `json:"url"`
	SHA256           string            `json:"sha256"`
	SizeBytes        int64             `json:"size_bytes"`
	ArchiveType      string            `json:"archive_type"`
	ExecutablePath   string            `json:"executable_path"`
	SupportArtifacts []SupportArtifact `json:"support_artifacts,omitempty"`
}

type SupportArtifact struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	SHA256      string `json:"sha256"`
	SizeBytes   int64  `json:"size_bytes"`
	ArchiveType string `json:"archive_type"`
}

type ManifestSignature struct {
	KeyID string `json:"key_id"`
	Value string `json:"value"`
}

type unsignedReleaseManifest struct {
	SchemaVersion int                        `json:"schema_version"`
	Engine        string                     `json:"engine"`
	Version       string                     `json:"version"`
	BuildID       string                     `json:"build_id"`
	Artifacts     map[string]RuntimeArtifact `json:"artifacts"`
}

func LoadReleaseManifest(path string) (ReleaseManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ReleaseManifest{}, fmt.Errorf("read runtime manifest %s: %w", path, err)
	}
	var manifest ReleaseManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return ReleaseManifest{}, fmt.Errorf("parse runtime manifest %s: %w", path, err)
	}
	return manifest, nil
}

func (m ReleaseManifest) UnsignedBytes() ([]byte, error) {
	return json.Marshal(unsignedReleaseManifest{
		SchemaVersion: m.SchemaVersion,
		Engine:        m.Engine,
		Version:       m.Version,
		BuildID:       m.BuildID,
		Artifacts:     m.Artifacts,
	})
}

func (m ReleaseManifest) VerifySignature(publicKey ed25519.PublicKey) error {
	if len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("runtime manifest public key has %d bytes, want %d", len(publicKey), ed25519.PublicKeySize)
	}
	signature, err := base64.StdEncoding.DecodeString(m.Signature.Value)
	if err != nil {
		return fmt.Errorf("decode runtime manifest signature: %w", err)
	}
	body, err := m.UnsignedBytes()
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, body, signature) {
		return fmt.Errorf("runtime manifest signature verification failed")
	}
	return nil
}

func DefaultRuntimePublicKey() (ed25519.PublicKey, error) {
	return DecodePublicKey(DefaultRuntimePublicKeyBase64)
}

func DecodePublicKey(encoded string) (ed25519.PublicKey, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode runtime public key: %w", err)
	}
	if len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("runtime public key has %d bytes, want %d", len(key), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(key), nil
}
