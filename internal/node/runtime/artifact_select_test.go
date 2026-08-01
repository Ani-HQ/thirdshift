package runtime

import (
	"strings"
	"testing"
)

func vendorManifest() ReleaseManifest {
	return ReleaseManifest{
		SchemaVersion: 1,
		Engine:        "llama.cpp",
		Version:       "b10182",
		BuildID:       "llama-cpp-b10182",
		Artifacts: map[string]RuntimeArtifact{
			"darwin/arm64":         {URL: "mac", SHA256: "sha256:mac", ExecutablePath: "llama-server"},
			"windows/amd64":        {URL: "cuda-fallback", SHA256: "sha256:cuda", ExecutablePath: "llama-server.exe"},
			"windows/amd64/cuda":   {URL: "cuda", SHA256: "sha256:cuda", ExecutablePath: "llama-server.exe"},
			"windows/amd64/vulkan": {URL: "vulkan", SHA256: "sha256:vulkan", ExecutablePath: "llama-server.exe"},
		},
	}
}

func TestArtifactKeysPreferVendorThenPlatform(t *testing.T) {
	for name, tc := range map[string]struct {
		platform string
		vendor   string
		want     []string
	}{
		"amd maps to vulkan":  {"windows/amd64", "amd", []string{"windows/amd64/vulkan", "windows/amd64"}},
		"nvidia maps to cuda": {"windows/amd64", "nvidia", []string{"windows/amd64/cuda", "windows/amd64"}},
		// An undetected vendor prefers Vulkan: it runs on NVIDIA too, while the
		// bare (CUDA) artifact cannot run on a Radeon at all.
		"unknown vendor":  {"windows/amd64", "unknown", []string{"windows/amd64/vulkan", "windows/amd64"}},
		"empty vendor":    {"windows/amd64", "", []string{"windows/amd64/vulkan", "windows/amd64"}},
		"vendor is cased": {"windows/amd64", "AMD", []string{"windows/amd64/vulkan", "windows/amd64"}},
		"no platform key": {"", "amd", nil},
	} {
		t.Run(name, func(t *testing.T) {
			got := ArtifactKeys(tc.platform, tc.vendor)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("keys = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestSelectArtifactByVendor(t *testing.T) {
	manifest := vendorManifest()

	t.Run("amd picks the vulkan build", func(t *testing.T) {
		artifact, key, err := manifest.SelectArtifact("windows/amd64", VendorAMD)
		if err != nil {
			t.Fatalf("select: %v", err)
		}
		if key != "windows/amd64/vulkan" || artifact.URL != "vulkan" {
			t.Fatalf("key = %q url = %q, want the vulkan artifact", key, artifact.URL)
		}
	})

	t.Run("nvidia picks the cuda build", func(t *testing.T) {
		artifact, key, err := manifest.SelectArtifact("windows/amd64", VendorNvidia)
		if err != nil {
			t.Fatalf("select: %v", err)
		}
		if key != "windows/amd64/cuda" || artifact.URL != "cuda" {
			t.Fatalf("key = %q url = %q, want the cuda artifact", key, artifact.URL)
		}
	})

	t.Run("unknown vendor prefers vulkan over the cuda fallback", func(t *testing.T) {
		// Asymmetric failure modes: CUDA on a Radeon cannot run, Vulkan on an
		// NVIDIA card merely runs slower. Guess the one that degrades.
		artifact, key, err := manifest.SelectArtifact("windows/amd64", VendorUnknown)
		if err != nil {
			t.Fatalf("select: %v", err)
		}
		if key != "windows/amd64/vulkan" {
			t.Fatalf("key = %q url = %q, want the vulkan artifact", key, artifact.URL)
		}
	})

	t.Run("unknown vendor still falls back when no vulkan build exists", func(t *testing.T) {
		bare := ReleaseManifest{Artifacts: map[string]RuntimeArtifact{
			"windows/amd64": {URL: "cuda-fallback"},
		}}
		artifact, key, err := bare.SelectArtifact("windows/amd64", VendorUnknown)
		if err != nil {
			t.Fatalf("select: %v", err)
		}
		if key != "windows/amd64" || artifact.URL != "cuda-fallback" {
			t.Fatalf("key = %q url = %q, want the bare platform artifact", key, artifact.URL)
		}
	})

	// A manifest that never gained vendor keys must keep working for a node
	// that now reports one; this is the older-manifest, newer-node direction.
	t.Run("vendor falls back when the manifest has no vendor key", func(t *testing.T) {
		legacy := ReleaseManifest{Artifacts: map[string]RuntimeArtifact{
			"windows/amd64": {URL: "only", SHA256: "sha256:only", ExecutablePath: "llama-server.exe"},
		}}
		artifact, key, err := legacy.SelectArtifact("windows/amd64", VendorAMD)
		if err != nil {
			t.Fatalf("select: %v", err)
		}
		if key != "windows/amd64" || artifact.URL != "only" {
			t.Fatalf("key = %q url = %q, want the bare platform artifact", key, artifact.URL)
		}
	})

	t.Run("missing platform is an error naming both keys tried", func(t *testing.T) {
		_, _, err := manifest.SelectArtifact("linux/amd64", VendorAMD)
		if err == nil {
			t.Fatal("missing platform was accepted")
		}
		if !strings.Contains(err.Error(), "linux/amd64/vulkan") || !strings.Contains(err.Error(), "linux/amd64") {
			t.Fatalf("error should name the keys tried: %v", err)
		}
	})
}

// The shipped manifest must actually carry the vendor builds the node expects.
func TestShippedManifestCarriesVendorArtifacts(t *testing.T) {
	manifest, err := LoadReleaseManifest("../../../models/catalog/llama-cpp-b10182.runtime.json")
	if err != nil {
		t.Fatalf("load shipped manifest: %v", err)
	}
	for _, key := range []string{"windows/amd64", "windows/amd64/cuda", "windows/amd64/vulkan", "darwin/arm64"} {
		if _, ok := manifest.Artifacts[key]; !ok {
			t.Fatalf("shipped manifest is missing artifact key %q", key)
		}
	}

	vulkan := manifest.Artifacts["windows/amd64/vulkan"]
	if vulkan.ArchiveType != "zip" || vulkan.ExecutablePath != "llama-server.exe" {
		t.Fatalf("vulkan artifact = %#v", vulkan)
	}
	if !strings.Contains(vulkan.URL, "win-vulkan-x64.zip") {
		t.Fatalf("vulkan url = %q", vulkan.URL)
	}

	// The CUDA build and the bare fallback must stay the same binary, or a node
	// that falls back would install something different from what it reports.
	if manifest.Artifacts["windows/amd64"].SHA256 != manifest.Artifacts["windows/amd64/cuda"].SHA256 {
		t.Fatal("the bare windows/amd64 fallback and the cuda artifact must be the same build")
	}
}
