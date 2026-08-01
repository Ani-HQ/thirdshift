package models

import (
	"strings"
	"testing"
)

const testCatalogDir = "../../../models/catalog"

func catalogForTest(t *testing.T) []Manifest {
	t.Helper()
	manifests, err := LoadSelectableManifests(testCatalogDir)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	if len(manifests) == 0 {
		t.Fatal("catalog produced no selectable manifests")
	}
	return manifests
}

// Roomy RAM and disk so these cases isolate the VRAM tier.
func hostWithVRAM(vramMB int64) HostCapacity {
	return HostCapacity{VRAMTotalMB: vramMB, RAMTotalMB: 65536, DiskFreeMB: 500000}
}

func TestSelectModelPicksLargestTierThatFits(t *testing.T) {
	manifests := catalogForTest(t)
	for name, tc := range map[string]struct {
		vramMB int64
		want   string
	}{
		"8GB picks a 7B":   {8192, "qwen2.5-7b-instruct"},
		"12GB picks the 14B": {12288, "qwen2.5-14b-instruct"},
		"16GB still 14B":     {16384, "qwen2.5-14b-instruct"},
		"24GB picks the 32B": {24576, "qwen2.5-32b-instruct"},
	} {
		t.Run(name, func(t *testing.T) {
			selection, err := SelectModel(manifests, hostWithVRAM(tc.vramMB))
			if err != nil {
				t.Fatalf("select: %v", err)
			}
			if selection.ModelID != tc.want {
				t.Fatalf("model = %q, want %q (reason: %s)", selection.ModelID, tc.want, selection.Reason)
			}
			if selection.VRAMAssumed {
				t.Fatal("VRAM was measured, so nothing should be assumed")
			}
			if !strings.Contains(selection.Reason, "VRAM available") {
				t.Fatalf("reason should state the measurement: %q", selection.Reason)
			}
		})
	}
}

func TestSelectModelRefusesWhenNothingFits(t *testing.T) {
	manifests := catalogForTest(t)
	_, err := SelectModel(manifests, hostWithVRAM(4096))
	if err == nil {
		t.Fatal("a 4GB host was given a model")
	}
	// The error has to be actionable: what the host has and what the smallest
	// model needs.
	for _, want := range []string{"4096MB VRAM", "smallest model"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error should mention %q: %v", want, err)
		}
	}
}

func TestHiddenModelsAreNeverAutoSelected(t *testing.T) {
	manifests := catalogForTest(t)
	for _, manifest := range manifests {
		if manifest.ModelID == "thirdshift-tiny-chat-v1" {
			t.Fatal("the hidden internal model is selectable")
		}
	}

	// Even on a host where only the hidden model's tiny floor would fit, the
	// answer is a clear refusal rather than the internal test model.
	tiny := HostCapacity{VRAMTotalMB: 512, RAMTotalMB: 4096, DiskFreeMB: 2000}
	if selection, err := SelectModel(manifests, tiny); err == nil {
		t.Fatalf("tiny host selected %q instead of refusing", selection.ModelID)
	}

	// And it cannot be reached by naming it explicitly either.
	if _, err := ResolveModel("thirdshift-tiny-chat-v1", manifests, hostWithVRAM(24576)); err == nil {
		t.Fatal("the hidden model was resolvable by name")
	}
}

func TestSelectionIsDeterministic(t *testing.T) {
	manifests := catalogForTest(t)
	host := hostWithVRAM(8192)
	first, err := SelectModel(manifests, host)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	// Reversed input must not change the answer: ordering is by hardware floor
	// and model id, never by catalog directory order.
	reversed := make([]Manifest, 0, len(manifests))
	for idx := len(manifests) - 1; idx >= 0; idx-- {
		reversed = append(reversed, manifests[idx])
	}
	second, err := SelectModel(reversed, host)
	if err != nil {
		t.Fatalf("select reversed: %v", err)
	}
	if first.ModelID != second.ModelID {
		t.Fatalf("selection depends on input order: %q then %q", first.ModelID, second.ModelID)
	}
}

func TestSelectModelHonorsRAMAndDiskFloors(t *testing.T) {
	manifests := catalogForTest(t)

	// Plenty of VRAM for the 32B, but not enough RAM: must step down a tier
	// rather than pick something the machine cannot hold.
	selection, err := SelectModel(manifests, HostCapacity{VRAMTotalMB: 24576, RAMTotalMB: 16384, DiskFreeMB: 500000})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if selection.ModelID != "qwen2.5-14b-instruct" {
		t.Fatalf("model = %q, want the 14B once RAM rules out the 32B", selection.ModelID)
	}

	// Same again for disk.
	selection, err = SelectModel(manifests, HostCapacity{VRAMTotalMB: 24576, RAMTotalMB: 65536, DiskFreeMB: 15000})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if selection.ModelID != "qwen2.5-14b-instruct" {
		t.Fatalf("model = %q, want the 14B once disk rules out the 32B", selection.ModelID)
	}
}

// The AMD/WMI path often cannot establish VRAM. Selection then rests on RAM
// alone and must say so loudly rather than quietly guessing a tier.
func TestUnknownVRAMFallsBackToRAMAndFlagsIt(t *testing.T) {
	manifests := catalogForTest(t)
	host := HostCapacity{VRAMTotalMB: VRAMUnmeasured, RAMTotalMB: 32768, DiskFreeMB: 500000}

	selection, err := SelectModel(manifests, host)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if !selection.VRAMAssumed {
		t.Fatal("a selection made without a VRAM measurement must be flagged")
	}
	if !strings.Contains(selection.Reason, "VRAM could not be measured") {
		t.Fatalf("reason should say VRAM was not measured: %q", selection.Reason)
	}
	if !strings.Contains(selection.Reason, "unverified") {
		t.Fatalf("reason should mark the VRAM floor unverified: %q", selection.Reason)
	}

	// Nothing fits on RAM either: refuse, and say why the usual check was skipped.
	_, err = SelectModel(manifests, HostCapacity{VRAMTotalMB: VRAMUnmeasured, RAMTotalMB: 2048, DiskFreeMB: 500000})
	if err == nil {
		t.Fatal("a 2GB-RAM host was given a model")
	}
	if !strings.Contains(err.Error(), "VRAM could not be measured") {
		t.Fatalf("error should explain the missing VRAM measurement: %v", err)
	}
}

func TestResolveModelExplicitOverride(t *testing.T) {
	manifests := catalogForTest(t)

	t.Run("a model that fits is honored", func(t *testing.T) {
		selection, err := ResolveModel("qwen2.5-7b-instruct", manifests, hostWithVRAM(24576))
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if selection.ModelID != "qwen2.5-7b-instruct" {
			t.Fatalf("model = %q", selection.ModelID)
		}
		if !strings.Contains(selection.Reason, "explicitly requested") {
			t.Fatalf("reason = %q", selection.Reason)
		}
	})

	t.Run("a model too big for the host is refused with a way forward", func(t *testing.T) {
		_, err := ResolveModel("qwen2.5-32b-instruct", manifests, hostWithVRAM(8192))
		if err == nil {
			t.Fatal("an oversized explicit model was accepted")
		}
		for _, want := range []string{"qwen2.5-32b-instruct", "23552MB VRAM", "8192MB VRAM", "--model auto"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error should mention %q: %v", want, err)
			}
		}
	})

	t.Run("auto and empty both mean auto", func(t *testing.T) {
		for _, requested := range []string{"", "auto", "AUTO", "  auto  "} {
			selection, err := ResolveModel(requested, manifests, hostWithVRAM(12288))
			if err != nil {
				t.Fatalf("resolve %q: %v", requested, err)
			}
			if selection.ModelID != "qwen2.5-14b-instruct" {
				t.Fatalf("resolve %q = %q, want the 14B", requested, selection.ModelID)
			}
		}
	})

	t.Run("an unknown model id is refused", func(t *testing.T) {
		_, err := ResolveModel("not-a-model", manifests, hostWithVRAM(24576))
		if err == nil {
			t.Fatal("an unknown model id was accepted")
		}
		if !strings.Contains(err.Error(), "not-a-model") {
			t.Fatalf("error should name the model: %v", err)
		}
	})
}
