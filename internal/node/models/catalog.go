package models

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	catalogfs "github.com/Ani-HQ/thirdshift/models/catalog"
)

// Listing status values describe how a model is presented on the public
// catalog. They are independent of the operational lifecycle in Status.
const (
	ListingLive     = "live"
	ListingWaitlist = "waitlist"
	ListingHidden   = "hidden"
)

type Manifest struct {
	SchemaVersion int
	ModelID       string
	DisplayName   string
	Description   string
	Status        string
	Listing       Listing
	Source        Source
	License       License
	Runtime       Runtime
	Hardware      Hardware
	Limits        Limits
	Capabilities  Capabilities
	Policy        Policy
	Pricing       Pricing
	Verification  Verification
}

type Listing struct {
	Status                        string
	Description                   string
	ExpectedOutputTokensPerSecond *float64
	MarketComparison              *MarketComparison
}

type MarketComparison struct {
	TypicalInputPerMillionUSD  float64
	TypicalOutputPerMillionUSD float64
	SourceNote                 string
}

// ListingStatus returns the normalized public listing status, defaulting to
// live so manifests written before the listing block keep their behavior.
func (m Manifest) ListingStatus() string {
	switch m.Listing.Status {
	case ListingWaitlist:
		return ListingWaitlist
	case ListingHidden:
		return ListingHidden
	default:
		return ListingLive
	}
}

// CatalogDescription returns the manifest's public one-liner, preferring the
// listing block over the legacy top-level description. It stays empty when the
// manifest has neither so readers can fall back to the display name.
func (m Manifest) CatalogDescription() string {
	if m.Listing.Description != "" {
		return m.Listing.Description
	}
	return m.Description
}

type Source struct {
	Provider   string
	Repository string
	Revision   string
	File       string
	URL        string
	SHA256     string
	SizeBytes  int64
}

type License struct {
	Identifier          string
	ReviewedAt          string
	Notes               string
	DistributeWithModel bool
	Attribution         *LicenseAttribution
}

// LicenseAttribution carries operator-facing attribution requirements a model
// license imposes on public surfaces (e.g. "Built with Llama").
type LicenseAttribution struct {
	DisplayText string
	NoticeText  string
	LicenseURL  string
	AUPURL      string
}

type Runtime struct {
	Engine          string
	BuildID         string
	ReleaseManifest string
	BinarySHA256    string
	Arguments       RuntimeArguments
}

type RuntimeArguments struct {
	ContextSize int
	BatchSize   int
	GPULayers   string
	Device      string
	Parallel    int
	Host        string
	Port        string
}

type Hardware struct {
	MinVRAMMB          int
	MinRAMMB           int
	MinDiskMB          int
	EligibleGPUClasses []string
}

type Limits struct {
	MaxInputTokens  int
	MaxOutputTokens int
	MaxRequestBytes int
}

type Capabilities struct {
	ChatCompletions bool
	Streaming       bool
	Tools           bool
	Embeddings      bool
}

type Policy struct {
	DataClass            string
	ContentFilterProfile string
}

type Pricing struct {
	CustomerInputPerMillionUSD            float64
	CustomerOutputPerMillionUSD           float64
	HostCreditPerMillionAcceptedOutputUSD float64
}

type Verification struct {
	DuplicateSampleRate float64
	ChallengeRate       float64
}

func LoadCatalogManifest(catalogDir, modelID string) (Manifest, string, error) {
	data, source, err := ReadCatalogFile(catalogDir, modelID+".yaml")
	if err != nil {
		return Manifest{}, source, err
	}
	manifest, err := parseManifest(fileScanner{Scanner: bufio.NewScanner(bytes.NewReader(data))})
	return manifest, source, err
}

// ReadCatalogFile reads a catalog file from disk when present, falling back
// to the catalog embedded in the binary. Released binaries run from arbitrary
// working directories with no repo checkout, so the embedded copy is what
// makes `thirdshift start` work after a plain install.
func ReadCatalogFile(catalogDir, name string) ([]byte, string, error) {
	path := filepath.Join(catalogDir, name)
	if data, err := os.ReadFile(path); err == nil {
		return data, path, nil
	}
	if data, err := catalogfs.FS.ReadFile(name); err == nil {
		return data, "embedded:" + name, nil
	}
	return nil, path, fmt.Errorf("catalog file %s not found on disk (%s) or in the embedded catalog", name, path)
}

func ParseManifestFile(path string) (Manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("open model manifest %s: %w", path, err)
	}
	defer file.Close()
	return parseManifest(fileScanner{Scanner: bufio.NewScanner(file)})
}

type scanner interface {
	Scan() bool
	Text() string
	Err() error
}

type fileScanner struct {
	*bufio.Scanner
}

func parseManifest(s scanner) (Manifest, error) {
	var manifest Manifest
	var section string
	var subsection string
	for s.Scan() {
		raw := s.Text()
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		if strings.HasPrefix(line, "- ") {
			if section == "hardware" && subsection == "eligible_gpu_classes" {
				manifest.Hardware.EligibleGPUClasses = append(manifest.Hardware.EligibleGPUClasses, strings.TrimSpace(strings.TrimPrefix(line, "- ")))
			}
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return Manifest{}, fmt.Errorf("manifest line %q is not key: value", line)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, "\"'")

		switch indent {
		case 0:
			subsection = ""
			if value == "" {
				section = key
				continue
			}
			section = ""
			if err := assignTopLevel(&manifest, key, value); err != nil {
				return Manifest{}, err
			}
		case 2:
			if value == "" {
				subsection = key
				continue
			}
			if err := assignSection(&manifest, section, key, value); err != nil {
				return Manifest{}, err
			}
		case 4:
			if err := assignSubsection(&manifest, section, subsection, key, value); err != nil {
				return Manifest{}, err
			}
		}
	}
	if err := s.Err(); err != nil {
		return Manifest{}, err
	}
	if manifest.ModelID == "" {
		return Manifest{}, fmt.Errorf("model manifest is missing model_id")
	}
	if manifest.Source.URL == "" {
		return Manifest{}, fmt.Errorf("model manifest %s is missing source.url", manifest.ModelID)
	}
	if manifest.Source.SHA256 == "" {
		return Manifest{}, fmt.Errorf("model manifest %s is missing source.sha256", manifest.ModelID)
	}
	switch manifest.Listing.Status {
	case "", ListingLive, ListingWaitlist, ListingHidden:
	default:
		return Manifest{}, fmt.Errorf("model manifest %s has unknown listing.status %q", manifest.ModelID, manifest.Listing.Status)
	}
	if comparison := manifest.Listing.MarketComparison; comparison != nil {
		if comparison.TypicalInputPerMillionUSD <= 0 || comparison.TypicalOutputPerMillionUSD <= 0 {
			return Manifest{}, fmt.Errorf("model manifest %s listing.market_comparison needs positive typical prices", manifest.ModelID)
		}
	}
	if manifest.License.DistributeWithModel {
		if _, ok := LicenseTextFor(manifest.License.Identifier); !ok {
			return Manifest{}, fmt.Errorf("model manifest %s requires license distribution but no vendored license text exists for identifier %q", manifest.ModelID, manifest.License.Identifier)
		}
	}
	if attribution := manifest.License.Attribution; attribution != nil && attribution.DisplayText == "" {
		return Manifest{}, fmt.Errorf("model manifest %s license.attribution needs display_text", manifest.ModelID)
	}
	return manifest, nil
}

func assignTopLevel(manifest *Manifest, key, value string) error {
	switch key {
	case "schema_version":
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("schema_version must be an integer: %w", err)
		}
		manifest.SchemaVersion = parsed
	case "model_id":
		manifest.ModelID = value
	case "display_name":
		manifest.DisplayName = value
	case "description":
		manifest.Description = value
	case "status":
		manifest.Status = value
	}
	return nil
}

func assignSection(manifest *Manifest, section, key, value string) error {
	switch section {
	case "source":
		switch key {
		case "provider":
			manifest.Source.Provider = value
		case "repository":
			manifest.Source.Repository = value
		case "revision":
			manifest.Source.Revision = value
		case "file":
			manifest.Source.File = value
		case "url":
			manifest.Source.URL = value
		case "sha256":
			manifest.Source.SHA256 = strings.TrimPrefix(value, "sha256:")
		case "size_bytes":
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return fmt.Errorf("source.size_bytes must be an integer: %w", err)
			}
			manifest.Source.SizeBytes = parsed
		}
	case "listing":
		switch key {
		case "status":
			manifest.Listing.Status = value
		case "description":
			manifest.Listing.Description = value
		case "expected_output_tokens_per_second":
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return fmt.Errorf("listing.expected_output_tokens_per_second must be a number: %w", err)
			}
			manifest.Listing.ExpectedOutputTokensPerSecond = &parsed
		}
	case "license":
		switch key {
		case "identifier":
			manifest.License.Identifier = value
		case "reviewed_at":
			manifest.License.ReviewedAt = value
		case "notes":
			manifest.License.Notes = value
		case "distribute_with_model":
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("license.distribute_with_model must be a boolean: %w", err)
			}
			manifest.License.DistributeWithModel = parsed
		}
	case "runtime":
		switch key {
		case "engine":
			manifest.Runtime.Engine = value
		case "build_id":
			manifest.Runtime.BuildID = value
		case "release_manifest":
			manifest.Runtime.ReleaseManifest = value
		case "binary_sha256":
			manifest.Runtime.BinarySHA256 = strings.TrimPrefix(value, "sha256:")
		}
	case "hardware":
		parsed, err := parseIntByKey(key, value)
		if err != nil {
			return err
		}
		switch key {
		case "min_vram_mb":
			manifest.Hardware.MinVRAMMB = parsed
		case "min_ram_mb":
			manifest.Hardware.MinRAMMB = parsed
		case "min_disk_mb":
			manifest.Hardware.MinDiskMB = parsed
		}
	case "limits":
		parsed, err := parseIntByKey(key, value)
		if err != nil {
			return err
		}
		switch key {
		case "max_input_tokens":
			manifest.Limits.MaxInputTokens = parsed
		case "max_output_tokens":
			manifest.Limits.MaxOutputTokens = parsed
		case "max_request_bytes":
			manifest.Limits.MaxRequestBytes = parsed
		}
	case "capabilities":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("capability %s must be a boolean: %w", key, err)
		}
		switch key {
		case "chat_completions":
			manifest.Capabilities.ChatCompletions = parsed
		case "streaming":
			manifest.Capabilities.Streaming = parsed
		case "tools":
			manifest.Capabilities.Tools = parsed
		case "embeddings":
			manifest.Capabilities.Embeddings = parsed
		}
	case "policy":
		switch key {
		case "data_class":
			manifest.Policy.DataClass = value
		case "content_filter_profile":
			manifest.Policy.ContentFilterProfile = value
		}
	case "pricing":
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("pricing %s must be a number: %w", key, err)
		}
		switch key {
		case "customer_input_per_million_usd":
			manifest.Pricing.CustomerInputPerMillionUSD = parsed
		case "customer_output_per_million_usd":
			manifest.Pricing.CustomerOutputPerMillionUSD = parsed
		case "host_credit_per_million_accepted_output_usd":
			manifest.Pricing.HostCreditPerMillionAcceptedOutputUSD = parsed
		}
	case "verification":
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("verification %s must be a number: %w", key, err)
		}
		switch key {
		case "duplicate_sample_rate":
			manifest.Verification.DuplicateSampleRate = parsed
		case "challenge_rate":
			manifest.Verification.ChallengeRate = parsed
		}
	}
	return nil
}

func assignSubsection(manifest *Manifest, section, subsection, key, value string) error {
	if section == "listing" && subsection == "market_comparison" {
		return assignMarketComparison(manifest, key, value)
	}
	if section == "license" && subsection == "attribution" {
		if manifest.License.Attribution == nil {
			manifest.License.Attribution = &LicenseAttribution{}
		}
		attribution := manifest.License.Attribution
		switch key {
		case "display_text":
			attribution.DisplayText = value
		case "notice_text":
			attribution.NoticeText = value
		case "license_url":
			attribution.LicenseURL = value
		case "aup_url":
			attribution.AUPURL = value
		default:
			return fmt.Errorf("unknown license.attribution key %q", key)
		}
		return nil
	}
	if section != "runtime" || subsection != "arguments" {
		return nil
	}
	switch key {
	case "context_size":
		parsed, err := parseIntByKey(key, value)
		if err != nil {
			return err
		}
		manifest.Runtime.Arguments.ContextSize = parsed
	case "batch_size":
		parsed, err := parseIntByKey(key, value)
		if err != nil {
			return err
		}
		manifest.Runtime.Arguments.BatchSize = parsed
	case "gpu_layers":
		manifest.Runtime.Arguments.GPULayers = value
	case "device":
		manifest.Runtime.Arguments.Device = value
	case "parallel":
		parsed, err := parseIntByKey(key, value)
		if err != nil {
			return err
		}
		manifest.Runtime.Arguments.Parallel = parsed
	case "host":
		manifest.Runtime.Arguments.Host = value
	case "port":
		manifest.Runtime.Arguments.Port = value
	}
	return nil
}

func assignMarketComparison(manifest *Manifest, key, value string) error {
	if manifest.Listing.MarketComparison == nil {
		manifest.Listing.MarketComparison = &MarketComparison{}
	}
	comparison := manifest.Listing.MarketComparison
	if key == "source_note" {
		comparison.SourceNote = value
		return nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fmt.Errorf("listing.market_comparison.%s must be a number: %w", key, err)
	}
	switch key {
	case "typical_input_per_million_usd":
		comparison.TypicalInputPerMillionUSD = parsed
	case "typical_output_per_million_usd":
		comparison.TypicalOutputPerMillionUSD = parsed
	}
	return nil
}

func parseIntByKey(key, value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return parsed, nil
}
