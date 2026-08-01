package models

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// AutoModelID is the explicit way to ask for hardware-based selection. An empty
// model id means the same thing.
const AutoModelID = "auto"

// HostCapacity is the measured host, in megabytes. VRAMTotalMB of zero or less
// means VRAM could not be established; selection then falls back to RAM alone
// and says so, rather than assuming a size.
type HostCapacity struct {
	VRAMTotalMB int64
	RAMTotalMB  int64
	DiskFreeMB  int64
}

func (h HostCapacity) vramKnown() bool {
	return h.VRAMTotalMB > 0
}

// Selection is the outcome of auto-selection, carrying the reason so the node
// can log why this model was chosen.
type Selection struct {
	ModelID  string
	Manifest Manifest
	Reason   string
	// VRAMAssumed is true when VRAM could not be measured and the choice rests
	// on RAM alone. Callers should surface this prominently.
	VRAMAssumed bool
}

// LoadSelectableManifests reads every manifest in the catalog directory that
// parses and is not hidden. Unparseable manifests (placeholders) are skipped,
// matching catalog sync behavior.
func LoadSelectableManifests(catalogDir string) ([]Manifest, error) {
	entries, err := os.ReadDir(catalogDir)
	if err != nil {
		return nil, fmt.Errorf("read catalog dir: %w", err)
	}
	var manifests []Manifest
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		manifest, err := ParseManifestFile(filepath.Join(catalogDir, entry.Name()))
		if err != nil {
			continue
		}
		if manifest.ListingStatus() == ListingHidden {
			continue
		}
		manifests = append(manifests, manifest)
	}
	return manifests, nil
}

// bySizeDescending orders candidates largest first. min_vram_mb is the size
// proxy; RAM and then model id break ties so the choice is deterministic.
func bySizeDescending(manifests []Manifest) {
	sort.SliceStable(manifests, func(i, j int) bool {
		if manifests[i].Hardware.MinVRAMMB != manifests[j].Hardware.MinVRAMMB {
			return manifests[i].Hardware.MinVRAMMB > manifests[j].Hardware.MinVRAMMB
		}
		if manifests[i].Hardware.MinRAMMB != manifests[j].Hardware.MinRAMMB {
			return manifests[i].Hardware.MinRAMMB > manifests[j].Hardware.MinRAMMB
		}
		return manifests[i].ModelID < manifests[j].ModelID
	})
}

// Fits reports whether the host satisfies a manifest's hardware floor. When
// VRAM is unknown the VRAM floor cannot be checked and is skipped; the caller
// is responsible for telling the operator that happened.
func Fits(manifest Manifest, host HostCapacity) bool {
	if host.vramKnown() && int64(manifest.Hardware.MinVRAMMB) > host.VRAMTotalMB {
		return false
	}
	if host.RAMTotalMB > 0 && int64(manifest.Hardware.MinRAMMB) > host.RAMTotalMB {
		return false
	}
	if host.DiskFreeMB > 0 && int64(manifest.Hardware.MinDiskMB) > host.DiskFreeMB {
		return false
	}
	return true
}

// SelectModel picks the largest catalog model the host can run.
func SelectModel(manifests []Manifest, host HostCapacity) (Selection, error) {
	candidates := make([]Manifest, 0, len(manifests))
	for _, manifest := range manifests {
		if manifest.ListingStatus() == ListingHidden {
			continue
		}
		candidates = append(candidates, manifest)
	}
	if len(candidates) == 0 {
		return Selection{}, fmt.Errorf("no selectable models in the catalog")
	}
	bySizeDescending(candidates)

	for _, manifest := range candidates {
		if !Fits(manifest, host) {
			continue
		}
		if host.vramKnown() {
			return Selection{
				ModelID:  manifest.ModelID,
				Manifest: manifest,
				Reason: fmt.Sprintf("%dMB VRAM available, needs %dMB",
					host.VRAMTotalMB, manifest.Hardware.MinVRAMMB),
			}, nil
		}
		return Selection{
			ModelID:     manifest.ModelID,
			Manifest:    manifest,
			VRAMAssumed: true,
			Reason: fmt.Sprintf("VRAM could not be measured, so this was chosen on RAM alone: %dMB RAM available, needs %dMB RAM and %dMB VRAM (unverified)",
				host.RAMTotalMB, manifest.Hardware.MinRAMMB, manifest.Hardware.MinVRAMMB),
		}, nil
	}

	smallest := candidates[len(candidates)-1]
	if host.vramKnown() {
		return Selection{}, fmt.Errorf(
			"no catalog model fits this host: %dMB VRAM, %dMB RAM, %dMB free disk; the smallest model %s needs %dMB VRAM, %dMB RAM, %dMB disk",
			host.VRAMTotalMB, host.RAMTotalMB, host.DiskFreeMB,
			smallest.ModelID, smallest.Hardware.MinVRAMMB, smallest.Hardware.MinRAMMB, smallest.Hardware.MinDiskMB)
	}
	return Selection{}, fmt.Errorf(
		"no catalog model fits this host on RAM and disk alone (VRAM could not be measured): %dMB RAM, %dMB free disk; the smallest model %s needs %dMB RAM and %dMB disk",
		host.RAMTotalMB, host.DiskFreeMB,
		smallest.ModelID, smallest.Hardware.MinRAMMB, smallest.Hardware.MinDiskMB)
}

// ResolveModel applies the model flag. An explicit id is honored but still
// checked against the hardware, so a host fails fast with an actionable message
// instead of starting something that will exhaust VRAM mid-job.
func ResolveModel(requested string, manifests []Manifest, host HostCapacity) (Selection, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" || strings.EqualFold(requested, AutoModelID) {
		return SelectModel(manifests, host)
	}
	for _, manifest := range manifests {
		if manifest.ModelID != requested {
			continue
		}
		if Fits(manifest, host) {
			return Selection{
				ModelID:     manifest.ModelID,
				Manifest:    manifest,
				VRAMAssumed: !host.vramKnown(),
				Reason:      "explicitly requested with --model",
			}, nil
		}
		return Selection{}, fmt.Errorf(
			"model %s does not fit this host: it needs %dMB VRAM, %dMB RAM and %dMB free disk; this host has %s, %dMB RAM and %dMB free disk. Pass --model auto to use the largest model that fits, or choose a smaller model",
			manifest.ModelID, manifest.Hardware.MinVRAMMB, manifest.Hardware.MinRAMMB, manifest.Hardware.MinDiskMB,
			describeVRAM(host), host.RAMTotalMB, host.DiskFreeMB)
	}
	return Selection{}, fmt.Errorf("model %s is not in the catalog, or is not selectable", requested)
}

func describeVRAM(host HostCapacity) string {
	if !host.vramKnown() {
		return "an unmeasurable amount of VRAM"
	}
	return fmt.Sprintf("%dMB VRAM", host.VRAMTotalMB)
}
