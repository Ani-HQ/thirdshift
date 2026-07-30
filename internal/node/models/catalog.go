package models

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Manifest struct {
	SchemaVersion int
	ModelID       string
	DisplayName   string
	Description   string
	Status        string
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
	Identifier string
	ReviewedAt string
	Notes      string
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
	path := filepath.Join(catalogDir, modelID+".yaml")
	manifest, err := ParseManifestFile(path)
	return manifest, path, err
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
	case "license":
		switch key {
		case "identifier":
			manifest.License.Identifier = value
		case "reviewed_at":
			manifest.License.ReviewedAt = value
		case "notes":
			manifest.License.Notes = value
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

func parseIntByKey(key, value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return parsed, nil
}
