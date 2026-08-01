package models

import (
	"path/filepath"
	"testing"
)

// A released binary runs from an arbitrary working directory with no repo
// checkout, so `thirdshift start` with no --model must select from the
// embedded catalog. Listing the directory alone failed with "read catalog dir:
// no such file or directory" on every installed host.
func TestAutoSelectionWorksWithoutAnOnDiskCatalog(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-catalog-here")

	manifests, err := LoadSelectableManifests(missing)
	if err != nil {
		t.Fatalf("load from embedded catalog: %v", err)
	}
	if len(manifests) == 0 {
		t.Fatal("embedded catalog produced no selectable manifests")
	}
	for _, manifest := range manifests {
		if manifest.ListingStatus() == ListingHidden {
			t.Fatalf("hidden model %s is selectable", manifest.ModelID)
		}
	}

	selection, err := SelectModel(manifests, HostCapacity{VRAMTotalMB: 8192, RAMTotalMB: 16384, DiskFreeMB: 200000})
	if err != nil {
		t.Fatalf("select from embedded catalog: %v", err)
	}
	if selection.ModelID == "" {
		t.Fatal("no model selected from the embedded catalog")
	}
}
