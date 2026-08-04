package runtime

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const DefaultRuntimePublicKeyBase64 = "/Bgu4fQntQGB6q3j18Z+du1L1/yOzU2/wNmSAQWVCMo="

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
	return ParseReleaseManifest(data, path)
}

// ParseReleaseManifest decodes a runtime release manifest from bytes; source
// is used only for error messages.
func ParseReleaseManifest(data []byte, source string) (ReleaseManifest, error) {
	var manifest ReleaseManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return ReleaseManifest{}, fmt.Errorf("parse runtime manifest %s: %w", source, err)
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

// GPU vendor identifiers used to pick a vendor-specific runtime build. These
// mirror hardware.Vendor* but are duplicated here so the runtime package does
// not depend on hardware detection.
const (
	VendorNvidia  = "nvidia"
	VendorAMD     = "amd"
	VendorApple   = "apple"
	VendorUnknown = "unknown"
)

// llama.cpp compute backends. Manifest artifact keys are named by backend, not
// by vendor, because the backend is what actually differs between builds and
// several vendors can share one (Vulkan runs on AMD and Intel alike).
const (
	BackendCUDA   = "cuda"
	BackendVulkan = "vulkan"
)

// BackendForVendor maps a detected GPU vendor onto the runtime build it needs.
//
// An undetected vendor resolves to Vulkan rather than to the bare platform
// artifact, because the failure modes are not symmetric: the bare Windows
// artifact is the CUDA build, which cannot run at all on a Radeon, while the
// Vulkan build runs on NVIDIA too, only slower. When detection fails the
// safe guess is the one that degrades instead of breaking.
func BackendForVendor(vendor string) string {
	switch strings.ToLower(strings.TrimSpace(vendor)) {
	case VendorNvidia:
		return BackendCUDA
	case VendorApple:
		// macOS ships one llama.cpp build with Metal compiled in, so the bare
		// platform key is the Metal build and there is no suffix to select.
		return ""
	default:
		// Unknown and AMD both take Vulkan, which runs on every Windows GPU we
		// might not have identified.
		return BackendVulkan
	}
}

// ArtifactKeys lists the manifest keys to try, most specific first. A
// backend-specific build is preferred when the manifest carries one; the bare
// platform key stays as the fallback so nodes that predate backend keys, and
// manifests that never gained them, keep working unchanged.
func ArtifactKeys(platformKey, vendor string) []string {
	if platformKey == "" {
		return nil
	}
	backend := BackendForVendor(vendor)
	if backend == "" {
		return []string{platformKey}
	}
	return []string{platformKey + "/" + backend, platformKey}
}

// SelectArtifact resolves the artifact for a platform and GPU vendor, returning
// the artifact and the key it was found under.
func (m ReleaseManifest) SelectArtifact(platformKey, vendor string) (RuntimeArtifact, string, error) {
	keys := ArtifactKeys(platformKey, vendor)
	if len(keys) == 0 {
		return RuntimeArtifact{}, "", fmt.Errorf("runtime artifact platform key is required")
	}
	for _, key := range keys {
		if artifact, ok := m.Artifacts[key]; ok {
			return artifact, key, nil
		}
	}
	return RuntimeArtifact{}, "", fmt.Errorf("runtime artifact for platform %s is not in manifest", strings.Join(keys, " or "))
}
