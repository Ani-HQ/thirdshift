package hardware

import (
	"context"
	"errors"
	"testing"
)

// Captured verbatim from `system_profiler SPDisplaysDataType -json` on an
// Apple M4 MacBook Pro (10-core GPU, 16 GB unified memory), trimmed to the
// fields this code reads plus enough context to keep the shape realistic.
const spDisplaysM4 = `{
  "SPDisplaysDataType" : [
    {
      "_name" : "Apple M4",
      "spdisplays_mtlgpufamilysupport" : "spdisplays_metal4",
      "spdisplays_vendor" : "sppci_vendor_Apple",
      "sppci_bus" : "spdisplays_builtin",
      "sppci_cores" : "10",
      "sppci_device_type" : "spdisplays_gpu",
      "sppci_model" : "Apple M4"
    }
  ]
}`

// A larger Apple Silicon machine, for the 32B tier.
const spDisplaysM3Max = `{
  "SPDisplaysDataType" : [
    {
      "_name" : "Apple M3 Max",
      "spdisplays_vendor" : "sppci_vendor_Apple",
      "sppci_bus" : "spdisplays_builtin",
      "sppci_cores" : "40",
      "sppci_device_type" : "spdisplays_gpu",
      "sppci_model" : "Apple M3 Max"
    }
  ]
}`

// An Intel Mac with a discrete AMD card: not Apple Silicon.
const spDisplaysIntelAMD = `{
  "SPDisplaysDataType" : [
    {
      "_name" : "AMD Radeon Pro 5500M",
      "spdisplays_vendor" : "sppci_vendor_amd",
      "sppci_device_type" : "spdisplays_gpu",
      "sppci_model" : "AMD Radeon Pro 5500M"
    }
  ]
}`

const (
	memsize16GB = "17179869184\n"
	memsize64GB = "68719476736\n"
)

type appleRunner struct {
	displays    string
	displaysErr error
	memsize     string
	memsizeErr  error
	calls       []string
}

func (r *appleRunner) Run(_ context.Context, name string, _ ...string) ([]byte, error) {
	r.calls = append(r.calls, name)
	switch name {
	case "nvidia-smi":
		return nil, errors.New("exec: nvidia-smi not found")
	case "system_profiler":
		return []byte(r.displays), r.displaysErr
	case "sysctl":
		return []byte(r.memsize), r.memsizeErr
	default:
		return nil, errors.New("unexpected command " + name)
	}
}

func TestParseAppleDisplaysJSON(t *testing.T) {
	gpus, err := ParseAppleDisplaysJSON(spDisplaysM4)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(gpus) != 1 {
		t.Fatalf("gpus = %#v", gpus)
	}
	if gpus[0].Model != "Apple M4" || gpus[0].Cores != "10" {
		t.Fatalf("gpu = %#v", gpus[0])
	}
	if _, err := ParseAppleDisplaysJSON("  "); err == nil {
		t.Fatal("empty output accepted")
	}
	if _, err := ParseAppleDisplaysJSON("{}"); err == nil {
		t.Fatal("a report with no GPUs was accepted")
	}
}

func TestSelectAppleGPUIgnoresNonApple(t *testing.T) {
	apple, err := ParseAppleDisplaysJSON(spDisplaysM4)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if gpu, ok := SelectAppleGPU(apple); !ok || gpu.Model != "Apple M4" {
		t.Fatalf("apple gpu = %#v ok=%v", gpu, ok)
	}

	// An Intel Mac with a discrete AMD card is not an Apple Silicon host.
	intel, err := ParseAppleDisplaysJSON(spDisplaysIntelAMD)
	if err != nil {
		t.Fatalf("parse intel: %v", err)
	}
	if _, ok := SelectAppleGPU(intel); ok {
		t.Fatal("a discrete AMD card on an Intel Mac was treated as Apple Silicon")
	}
}

func TestParseMemsize(t *testing.T) {
	got, err := ParseMemsize(memsize16GB)
	if err != nil || got != 17179869184 {
		t.Fatalf("memsize = (%d, %v)", got, err)
	}
	for _, bad := range []string{"", "   ", "not-a-number", "0", "-1"} {
		if _, err := ParseMemsize(bad); err == nil {
			t.Fatalf("memsize %q was accepted", bad)
		}
	}
}

// The budget is a share of one shared pool, never the whole thing: the machine
// still has to run macOS while it serves.
func TestAppleGPUMemoryBudget(t *testing.T) {
	for name, tc := range map[string]struct {
		totalBytes int64
		wantMB     int64
	}{
		"16GB M4":  {17179869184, 10649},
		"64GB":     {68719476736, 42598},
		"8GB":      {8589934592, 5324},
		"unknown":  {0, VRAMUnknown},
		"nonsense": {-5, VRAMUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			if got := AppleGPUMemoryBudgetMB(tc.totalBytes); got != tc.wantMB {
				t.Fatalf("budget = %d MB, want %d MB", got, tc.wantMB)
			}
		})
	}

	// Never hand the GPU the entire pool.
	total := int64(17179869184)
	if AppleGPUMemoryBudgetMB(total) >= total/1024/1024 {
		t.Fatal("budget must be strictly less than total unified memory")
	}
}

func TestDetectGPUVendorOnDarwin(t *testing.T) {
	t.Run("apple silicon", func(t *testing.T) {
		runner := &appleRunner{displays: spDisplaysM4, memsize: memsize16GB}
		if got := DetectGPUVendor(context.Background(), runner, "darwin"); got != VendorApple {
			t.Fatalf("vendor = %q, want apple", got)
		}
	})

	t.Run("intel mac with amd card is not apple", func(t *testing.T) {
		runner := &appleRunner{displays: spDisplaysIntelAMD}
		if got := DetectGPUVendor(context.Background(), runner, "darwin"); got != VendorUnknown {
			t.Fatalf("vendor = %q, want unknown", got)
		}
	})

	t.Run("system_profiler failure is unknown, not a crash", func(t *testing.T) {
		runner := &appleRunner{displaysErr: errors.New("not found")}
		if got := DetectGPUVendor(context.Background(), runner, "darwin"); got != VendorUnknown {
			t.Fatalf("vendor = %q, want unknown", got)
		}
	})

	t.Run("darwin never probes windows WMI", func(t *testing.T) {
		runner := &appleRunner{displays: spDisplaysM4, memsize: memsize16GB}
		DetectGPUVendor(context.Background(), runner, "darwin")
		for _, call := range runner.calls {
			if call == "powershell" {
				t.Fatal("WMI was probed on darwin")
			}
		}
	})
}

func TestDetectHostResourcesOnAppleSilicon(t *testing.T) {
	for name, tc := range map[string]struct {
		memsize    string
		wantVRAMMB int64
		wantName   string
	}{
		"16GB M4":     {memsize16GB, 10649, "Apple M4"},
		"64GB M3 Max": {memsize64GB, 42598, "Apple M3 Max"},
	} {
		t.Run(name, func(t *testing.T) {
			displays := spDisplaysM4
			if tc.wantName == "Apple M3 Max" {
				displays = spDisplaysM3Max
			}
			runner := &appleRunner{displays: displays, memsize: tc.memsize}
			resources, err := DetectHostResources(context.Background(), runner, ".", "darwin")
			if err != nil {
				t.Fatalf("detect: %v", err)
			}
			if resources.GPUVendor != VendorApple {
				t.Fatalf("vendor = %q", resources.GPUVendor)
			}
			if resources.GPUName != tc.wantName {
				t.Fatalf("gpu name = %q, want %q", resources.GPUName, tc.wantName)
			}
			if resources.VRAMTotalMB != tc.wantVRAMMB {
				t.Fatalf("gpu budget = %d MB, want %d MB", resources.VRAMTotalMB, tc.wantVRAMMB)
			}
			if !resources.VRAMKnown() {
				t.Fatal("a measured unified-memory budget should count as known")
			}
		})
	}

	// Unified memory unreadable: vendor still apple, budget unknown, and the
	// caller must not be handed a fabricated number.
	runner := &appleRunner{displays: spDisplaysM4, memsizeErr: errors.New("boom")}
	resources, err := DetectHostResources(context.Background(), runner, ".", "darwin")
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if resources.GPUVendor != VendorApple {
		t.Fatalf("vendor = %q, want apple", resources.GPUVendor)
	}
	if resources.VRAMKnown() {
		t.Fatalf("budget = %d, want unknown when hw.memsize fails", resources.VRAMTotalMB)
	}
}
