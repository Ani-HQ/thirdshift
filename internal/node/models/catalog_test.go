package models

import (
	"strings"
	"testing"
)

const catalogDir = "../../../models/catalog"

func TestCatalogManifestParsesPinnedTinyModel(t *testing.T) {
	manifest, _, err := LoadCatalogManifest(catalogDir, "thirdshift-tiny-chat-v1")
	if err != nil {
		t.Fatalf("parse existing model manifest: %v", err)
	}
	if manifest.ModelID != "thirdshift-tiny-chat-v1" {
		t.Fatalf("model id = %q", manifest.ModelID)
	}
	if manifest.Source.SizeBytes != 88201792 {
		t.Fatalf("source size = %d", manifest.Source.SizeBytes)
	}
	if manifest.Runtime.ReleaseManifest == "" {
		t.Fatal("runtime release manifest path is empty")
	}
	if manifest.ListingStatus() != ListingHidden {
		t.Fatalf("tiny model listing status = %q, want hidden", manifest.ListingStatus())
	}
}

func TestWaitlistManifestsCarryPinnedSourcesAndListingCopy(t *testing.T) {
	for _, tc := range []struct {
		modelID    string
		repository string
		revision   string
		sha256     string
		sizeBytes  int64
		license    string
		inputUSD   float64
		outputUSD  float64
		marketIn   float64
		marketOut  float64
		expectedTS float64
	}{
		{
			modelID:    "qwen2.5-7b-instruct",
			repository: "bartowski/Qwen2.5-7B-Instruct-GGUF",
			revision:   "8911e8a47f92bac19d6f5c64a2e2095bd2f7d031",
			sha256:     "65b8fcd92af6b4fefa935c625d1ac27ea29dcb6ee14589c55a8f115ceaaa1423",
			sizeBytes:  4683074240,
			license:    "apache-2.0",
			inputUSD:   0.03,
			outputUSD:  0.08,
			marketIn:   0.04,
			marketOut:  0.10,
			expectedTS: 30,
		},
		{
			modelID:    "qwen2.5-coder-7b-instruct",
			repository: "bartowski/Qwen2.5-Coder-7B-Instruct-GGUF",
			revision:   "1f629da0c8bed16b9e50cee91c70693650e66c35",
			sha256:     "1664fccab734674a50763490a8c6931b70e3f2f8ec10031b54806d30e5f956b6",
			sizeBytes:  4683074336,
			license:    "apache-2.0",
			inputUSD:   0.03,
			outputUSD:  0.08,
			marketIn:   0.04,
			marketOut:  0.10,
			expectedTS: 30,
		},
		{
			modelID:    "llama-3.2-3b-instruct",
			repository: "bartowski/Llama-3.2-3B-Instruct-GGUF",
			revision:   "5ab33fa94d1d04e903623ae72c95d1696f09f9e8",
			sha256:     "6c1a2b41161032677be168d354123594c0e6e67d2b9227c84f296ad037c728ff",
			sizeBytes:  2019377696,
			license:    "llama3.2",
			inputUSD:   0.01,
			outputUSD:  0.02,
			marketIn:   0.015,
			marketOut:  0.025,
			expectedTS: 60,
		},
	} {
		t.Run(tc.modelID, func(t *testing.T) {
			manifest, _, err := LoadCatalogManifest(catalogDir, tc.modelID)
			if err != nil {
				t.Fatalf("parse manifest: %v", err)
			}
			if manifest.ListingStatus() != ListingWaitlist {
				t.Fatalf("listing status = %q, want waitlist", manifest.ListingStatus())
			}
			if manifest.CatalogDescription() == "" {
				t.Fatal("listing description is empty")
			}
			if manifest.Source.Repository != tc.repository || manifest.Source.Revision != tc.revision {
				t.Fatalf("source = %s@%s", manifest.Source.Repository, manifest.Source.Revision)
			}
			if manifest.Source.SHA256 != tc.sha256 {
				t.Fatalf("sha256 = %q", manifest.Source.SHA256)
			}
			if manifest.Source.SizeBytes != tc.sizeBytes {
				t.Fatalf("size_bytes = %d, want %d", manifest.Source.SizeBytes, tc.sizeBytes)
			}
			if manifest.License.Identifier != tc.license {
				t.Fatalf("license = %q, want %q", manifest.License.Identifier, tc.license)
			}
			if manifest.Pricing.CustomerInputPerMillionUSD != tc.inputUSD || manifest.Pricing.CustomerOutputPerMillionUSD != tc.outputUSD {
				t.Fatalf("pricing = %#v", manifest.Pricing)
			}
			if manifest.Listing.ExpectedOutputTokensPerSecond == nil || *manifest.Listing.ExpectedOutputTokensPerSecond != tc.expectedTS {
				t.Fatalf("expected speed = %v, want %v", manifest.Listing.ExpectedOutputTokensPerSecond, tc.expectedTS)
			}
			comparison := manifest.Listing.MarketComparison
			if comparison == nil {
				t.Fatal("market comparison is missing")
			}
			if comparison.TypicalInputPerMillionUSD != tc.marketIn || comparison.TypicalOutputPerMillionUSD != tc.marketOut {
				t.Fatalf("market comparison = %#v", comparison)
			}
			if comparison.SourceNote == "" {
				t.Fatal("market comparison source note is empty")
			}
			if manifest.Hardware.MinVRAMMB != 8192 {
				t.Fatalf("min_vram_mb = %d, want the 8192 platform floor", manifest.Hardware.MinVRAMMB)
			}
			if manifest.Limits.MaxInputTokens+manifest.Limits.MaxOutputTokens > manifest.Runtime.Arguments.ContextSize {
				t.Fatalf("input+output limits %d exceed context size %d",
					manifest.Limits.MaxInputTokens+manifest.Limits.MaxOutputTokens, manifest.Runtime.Arguments.ContextSize)
			}
		})
	}
}

func TestManifestRejectsUnknownListingStatus(t *testing.T) {
	_, err := parseManifest(&sliceScanner{lines: []string{
		"model_id: broken-listing",
		"listing:",
		"  status: coming-soon",
		"source:",
		"  url: https://example.invalid/model.gguf",
		"  sha256: abc",
	}})
	if err == nil {
		t.Fatal("unknown listing status accepted")
	}
}

func TestManifestListingDefaultsToLive(t *testing.T) {
	manifest, err := parseManifest(&sliceScanner{lines: []string{
		"model_id: plain-model",
		"display_name: Plain Model",
		"source:",
		"  url: https://example.invalid/model.gguf",
		"  sha256: abc",
	}})
	if err != nil {
		t.Fatalf("parse manifest without listing block: %v", err)
	}
	if manifest.ListingStatus() != ListingLive {
		t.Fatalf("listing status = %q, want live", manifest.ListingStatus())
	}
	if manifest.CatalogDescription() != "" {
		t.Fatalf("catalog description = %q, want empty", manifest.CatalogDescription())
	}
}

type sliceScanner struct {
	lines []string
	index int
}

func (s *sliceScanner) Scan() bool {
	if s.index >= len(s.lines) {
		return false
	}
	s.index++
	return true
}

func (s *sliceScanner) Text() string {
	return s.lines[s.index-1]
}

func (s *sliceScanner) Err() error {
	return nil
}

func TestManifestParsesLicenseAttributionAndDistribution(t *testing.T) {
	manifest, err := parseManifest(&sliceScanner{lines: []string{
		"model_id: attributed-model",
		"display_name: Attributed Model",
		"license:",
		"  identifier: llama3.2",
		"  distribute_with_model: true",
		"  attribution:",
		"    display_text: Built with Llama",
		"    notice_text: Llama 3.2 is licensed under the Llama 3.2 Community License",
		"    license_url: https://example.invalid/license",
		"    aup_url: https://example.invalid/aup",
		"source:",
		"  url: https://example.invalid/model.gguf",
		"  sha256: abc",
	}})
	if err != nil {
		t.Fatalf("parse manifest with attribution: %v", err)
	}
	if !manifest.License.DistributeWithModel {
		t.Fatal("distribute_with_model not parsed")
	}
	attribution := manifest.License.Attribution
	if attribution == nil || attribution.DisplayText != "Built with Llama" {
		t.Fatalf("attribution = %+v, want Built with Llama", attribution)
	}
	if attribution.LicenseURL != "https://example.invalid/license" || attribution.AUPURL != "https://example.invalid/aup" {
		t.Fatalf("attribution URLs mangled: %+v", attribution)
	}
}

func TestManifestRejectsDistributionWithoutVendoredLicense(t *testing.T) {
	_, err := parseManifest(&sliceScanner{lines: []string{
		"model_id: unlicensed-model",
		"license:",
		"  identifier: some-unknown-license",
		"  distribute_with_model: true",
		"source:",
		"  url: https://example.invalid/model.gguf",
		"  sha256: abc",
	}})
	if err == nil {
		t.Fatal("distribution without vendored license text accepted")
	}
}

func TestManifestRejectsAttributionWithoutDisplayText(t *testing.T) {
	_, err := parseManifest(&sliceScanner{lines: []string{
		"model_id: half-attributed-model",
		"license:",
		"  identifier: apache-2.0",
		"  attribution:",
		"    notice_text: only a notice",
		"source:",
		"  url: https://example.invalid/model.gguf",
		"  sha256: abc",
	}})
	if err == nil {
		t.Fatal("attribution without display_text accepted")
	}
}

func TestVendoredLlamaLicenseTextIsTheRealAgreement(t *testing.T) {
	text, ok := LicenseTextFor("llama3.2")
	if !ok {
		t.Fatal("no vendored text for llama3.2")
	}
	for _, marker := range []string{"LLAMA 3.2 COMMUNITY LICENSE AGREEMENT", "700 million"} {
		if !strings.Contains(text, marker) {
			t.Fatalf("vendored license text missing %q", marker)
		}
	}
	if _, ok := LicenseTextFor("apache-2.0"); ok {
		t.Fatal("apache-2.0 unexpectedly has vendored distribution text")
	}
}
