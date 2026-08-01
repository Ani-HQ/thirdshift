package hardware

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Real-shaped `Get-CimInstance Win32_VideoController | ConvertTo-Json -Compress`
// output. A single adapter serializes as a bare object, several as an array.
const (
	wmiSingleAMD = `{"Name":"AMD Radeon RX 7900 XTX","AdapterRAM":4293918720,"DriverVersion":"31.0.24033.1003","AdapterCompatibility":"Advanced Micro Devices, Inc."}`

	wmiAMDWithIntegrated = `[{"Name":"AMD Radeon(TM) Graphics","AdapterRAM":536870912,"DriverVersion":"31.0.21921.1000","AdapterCompatibility":"Advanced Micro Devices, Inc."},` +
		`{"Name":"AMD Radeon RX 7800 XT","AdapterRAM":4293918720,"DriverVersion":"31.0.24033.1003","AdapterCompatibility":"Advanced Micro Devices, Inc."}]`

	wmiNvidiaAndIntel = `[{"Name":"Intel(R) UHD Graphics 770","AdapterRAM":1073741824,"DriverVersion":"31.0.101.4502","AdapterCompatibility":"Intel Corporation"},` +
		`{"Name":"NVIDIA GeForce RTX 4070","AdapterRAM":4293918720,"DriverVersion":"32.0.15.6094","AdapterCompatibility":"NVIDIA"}]`

	wmiIntelOnly = `{"Name":"Intel(R) Iris(R) Xe Graphics","AdapterRAM":1073741824,"DriverVersion":"31.0.101.4502","AdapterCompatibility":"Intel Corporation"}`

	// A 2 GB card: small enough that AdapterRAM is genuinely representable.
	wmiSmallAMD = `{"Name":"AMD Radeon R7 240","AdapterRAM":2147483648,"DriverVersion":"31.0.14057.1006","AdapterCompatibility":"Advanced Micro Devices, Inc."}`
)

type scriptedRunner struct {
	nvidiaOutput string
	nvidiaErr    error
	wmiOutput    string
	wmiErr       error
	calls        []string
}

func (r *scriptedRunner) Run(_ context.Context, name string, _ ...string) ([]byte, error) {
	r.calls = append(r.calls, name)
	switch name {
	case "nvidia-smi":
		return []byte(r.nvidiaOutput), r.nvidiaErr
	case "powershell":
		return []byte(r.wmiOutput), r.wmiErr
	default:
		return nil, errors.New("unexpected command " + name)
	}
}

func TestParseVideoControllerJSONAcceptsObjectAndArray(t *testing.T) {
	single, err := ParseVideoControllerJSON(wmiSingleAMD)
	if err != nil {
		t.Fatalf("parse single adapter: %v", err)
	}
	if len(single) != 1 || single[0].Name != "AMD Radeon RX 7900 XTX" {
		t.Fatalf("single adapter = %#v", single)
	}

	many, err := ParseVideoControllerJSON(wmiNvidiaAndIntel)
	if err != nil {
		t.Fatalf("parse adapter array: %v", err)
	}
	if len(many) != 2 {
		t.Fatalf("adapter array length = %d, want 2", len(many))
	}

	if _, err := ParseVideoControllerJSON("   "); err == nil {
		t.Fatal("empty output was accepted")
	}
	if _, err := ParseVideoControllerJSON("not json"); err == nil {
		t.Fatal("malformed output was accepted")
	}
}

func TestVendorForController(t *testing.T) {
	for name, tc := range map[string]struct {
		controller VideoController
		want       string
	}{
		"amd by compatibility": {VideoController{Name: "Radeon RX 7900 XTX", AdapterCompatibility: "Advanced Micro Devices, Inc."}, VendorAMD},
		"amd by name only":     {VideoController{Name: "AMD Radeon RX 6800"}, VendorAMD},
		"radeon without amd":   {VideoController{Name: "Radeon Pro W6600"}, VendorAMD},
		"nvidia":               {VideoController{Name: "NVIDIA GeForce RTX 4070", AdapterCompatibility: "NVIDIA"}, VendorNvidia},
		"intel is unknown":     {VideoController{Name: "Intel(R) UHD Graphics 770", AdapterCompatibility: "Intel Corporation"}, VendorUnknown},
		"empty is unknown":     {VideoController{}, VendorUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			if got := VendorForController(tc.controller); got != tc.want {
				t.Fatalf("vendor = %q, want %q", got, tc.want)
			}
		})
	}
}

// AdapterRAM is a 32-bit field. Anything at the ceiling has wrapped and must be
// reported unknown rather than as a 4 GB card.
func TestAdapterVRAMBytesRejectsTruncatedValues(t *testing.T) {
	truncated := int64(4293918720)
	ceiling := int64(4294967295)
	real := int64(2147483648)
	zero := int64(0)

	if _, ok := AdapterVRAMBytes(VideoController{AdapterRAM: &ceiling}); ok {
		t.Fatal("the 32-bit ceiling was treated as a real size")
	}
	if _, ok := AdapterVRAMBytes(VideoController{AdapterRAM: &truncated}); ok {
		t.Fatal("0xFFF00000, the value real AMD cards report, should be distrusted")
	}
	fourGB := int64(4 * 1024 * 1024 * 1024)
	if _, ok := AdapterVRAMBytes(VideoController{AdapterRAM: &fourGB}); ok {
		t.Fatal("a card at the 4 GiB boundary cannot be represented and must be distrusted")
	}
	if _, ok := AdapterVRAMBytes(VideoController{AdapterRAM: &zero}); ok {
		t.Fatal("zero was treated as a real size")
	}
	if _, ok := AdapterVRAMBytes(VideoController{}); ok {
		t.Fatal("absent AdapterRAM was treated as a real size")
	}
	got, ok := AdapterVRAMBytes(VideoController{AdapterRAM: &real})
	if !ok || got != real {
		t.Fatalf("plausible size = (%d, %v), want (%d, true)", got, ok, real)
	}
}

func TestSelectPrimaryControllerPrefersDiscrete(t *testing.T) {
	controllers, err := ParseVideoControllerJSON(wmiAMDWithIntegrated)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	primary, vendor := SelectPrimaryController(controllers)
	if vendor != VendorAMD {
		t.Fatalf("vendor = %q, want amd", vendor)
	}
	// Both adapters are AMD here, so the first match wins; what matters is that
	// an AMD adapter is chosen rather than an unknown one.
	if !strings.Contains(primary.Name, "AMD") {
		t.Fatalf("primary = %q", primary.Name)
	}

	mixed, err := ParseVideoControllerJSON(wmiNvidiaAndIntel)
	if err != nil {
		t.Fatalf("parse mixed: %v", err)
	}
	primary, vendor = SelectPrimaryController(mixed)
	if vendor != VendorNvidia || !strings.Contains(primary.Name, "NVIDIA") {
		t.Fatalf("mixed primary = %q vendor = %q, want the NVIDIA card", primary.Name, vendor)
	}

	intel, err := ParseVideoControllerJSON(wmiIntelOnly)
	if err != nil {
		t.Fatalf("parse intel: %v", err)
	}
	_, vendor = SelectPrimaryController(intel)
	if vendor != VendorUnknown {
		t.Fatalf("intel-only vendor = %q, want unknown", vendor)
	}
}

func TestDetectGPUVendor(t *testing.T) {
	nvidiaCSV := "NVIDIA GeForce RTX 4070, GPU-abc, 12282, 561.09, 45, 30.5, 200.0, 12\n"

	t.Run("nvidia wins when nvidia-smi answers", func(t *testing.T) {
		runner := &scriptedRunner{nvidiaOutput: nvidiaCSV, wmiOutput: wmiSingleAMD}
		if got := DetectGPUVendor(context.Background(), runner, "windows"); got != VendorNvidia {
			t.Fatalf("vendor = %q, want nvidia", got)
		}
		if len(runner.calls) != 1 || runner.calls[0] != "nvidia-smi" {
			t.Fatalf("WMI should not be consulted when nvidia-smi answers: %v", runner.calls)
		}
	})

	t.Run("amd via wmi when nvidia-smi is absent", func(t *testing.T) {
		runner := &scriptedRunner{nvidiaErr: errors.New("exec: nvidia-smi not found"), wmiOutput: wmiSingleAMD}
		if got := DetectGPUVendor(context.Background(), runner, "windows"); got != VendorAMD {
			t.Fatalf("vendor = %q, want amd", got)
		}
	})

	t.Run("unknown when neither answers", func(t *testing.T) {
		runner := &scriptedRunner{nvidiaErr: errors.New("not found"), wmiErr: errors.New("not found")}
		if got := DetectGPUVendor(context.Background(), runner, "windows"); got != VendorUnknown {
			t.Fatalf("vendor = %q, want unknown", got)
		}
	})

	t.Run("unknown off windows without probing wmi", func(t *testing.T) {
		runner := &scriptedRunner{nvidiaErr: errors.New("not found"), wmiOutput: wmiSingleAMD}
		if got := DetectGPUVendor(context.Background(), runner, "darwin"); got != VendorUnknown {
			t.Fatalf("vendor = %q, want unknown", got)
		}
		for _, call := range runner.calls {
			if call == "powershell" {
				t.Fatal("WMI was probed on a non-Windows host")
			}
		}
	})

	t.Run("intel-only host is unknown", func(t *testing.T) {
		runner := &scriptedRunner{nvidiaErr: errors.New("not found"), wmiOutput: wmiIntelOnly}
		if got := DetectGPUVendor(context.Background(), runner, "windows"); got != VendorUnknown {
			t.Fatalf("vendor = %q, want unknown", got)
		}
	})
}

func TestGPUsFromControllersMarksUnknownVRAM(t *testing.T) {
	controllers, err := ParseVideoControllerJSON(wmiSingleAMD)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	gpus := GPUsFromControllers(controllers)
	if len(gpus) != 1 {
		t.Fatalf("gpus = %#v", gpus)
	}
	if gpus[0].VRAMTotalMB != VRAMUnknown {
		t.Fatalf("VRAM = %d, want the unknown sentinel for a truncated report", gpus[0].VRAMTotalMB)
	}
	if gpus[0].DriverVersion != "31.0.24033.1003" {
		t.Fatalf("driver = %q", gpus[0].DriverVersion)
	}

	small, err := ParseVideoControllerJSON(wmiSmallAMD)
	if err != nil {
		t.Fatalf("parse small: %v", err)
	}
	if got := GPUsFromControllers(small)[0].VRAMTotalMB; got != 2048 {
		t.Fatalf("small card VRAM = %d MB, want 2048", got)
	}
}
