package hardware

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// GPU vendors the host node can serve on. Anything we cannot positively
// identify stays VendorUnknown rather than being guessed at.
const (
	VendorNvidia  = "nvidia"
	VendorAMD     = "amd"
	VendorUnknown = "unknown"
)

// VRAMUnknown marks a card whose memory size could not be established
// reliably. It is deliberately distinct from zero, which would read as "no
// memory" and silently fail eligibility comparisons.
const VRAMUnknown int64 = -1

// PowerShell is the no-extra-dependency route to the Windows display adapter
// inventory. ConvertTo-Json keeps parsing honest; -Compress avoids line wraps.
var WindowsVideoControllerArgs = []string{
	"-NoProfile",
	"-NonInteractive",
	"-Command",
	"Get-CimInstance Win32_VideoController | Select-Object Name, AdapterRAM, DriverVersion, AdapterCompatibility | ConvertTo-Json -Compress",
}

// VideoController is one display adapter as Windows reports it.
type VideoController struct {
	Name                 string `json:"Name"`
	AdapterRAM           *int64 `json:"AdapterRAM"`
	DriverVersion        string `json:"DriverVersion"`
	AdapterCompatibility string `json:"AdapterCompatibility"`
}

// DetectWindowsVideoControllers queries WMI through PowerShell.
func DetectWindowsVideoControllers(ctx context.Context, runner CommandRunner) ([]VideoController, error) {
	output, err := runner.Run(ctx, "powershell", WindowsVideoControllerArgs...)
	if err != nil {
		return nil, fmt.Errorf("query Win32_VideoController: %w", err)
	}
	return ParseVideoControllerJSON(string(output))
}

// ParseVideoControllerJSON accepts either a single object or an array, because
// ConvertTo-Json emits a bare object when exactly one adapter is present.
func ParseVideoControllerJSON(input string) ([]VideoController, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil, fmt.Errorf("Win32_VideoController returned no output")
	}
	var controllers []VideoController
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal([]byte(trimmed), &controllers); err != nil {
			return nil, fmt.Errorf("parse Win32_VideoController array: %w", err)
		}
	} else {
		var single VideoController
		if err := json.Unmarshal([]byte(trimmed), &single); err != nil {
			return nil, fmt.Errorf("parse Win32_VideoController object: %w", err)
		}
		controllers = []VideoController{single}
	}
	if len(controllers) == 0 {
		return nil, fmt.Errorf("Win32_VideoController returned no adapters")
	}
	return controllers, nil
}

// VendorForController classifies an adapter from its vendor string and name.
func VendorForController(controller VideoController) string {
	haystack := strings.ToLower(controller.AdapterCompatibility + " " + controller.Name)
	switch {
	case strings.Contains(haystack, "nvidia"):
		return VendorNvidia
	case strings.Contains(haystack, "advanced micro devices"),
		strings.Contains(haystack, "amd"),
		strings.Contains(haystack, "radeon"),
		strings.Contains(haystack, "ati "):
		return VendorAMD
	default:
		return VendorUnknown
	}
}

// AdapterVRAMBytes converts the WMI AdapterRAM field, which is a 32-bit value
// in practice. Anything at or above the 4 GiB boundary has wrapped and cannot
// be trusted; a modern 8, 16 or 24 GB card commonly reports exactly 4294967295.
// Reporting unknown is the honest answer: the alternative is telling a host
// with a 24 GB card that it has 4 GB, which would wrongly fail eligibility.
func AdapterVRAMBytes(controller VideoController) (int64, bool) {
	if controller.AdapterRAM == nil {
		return VRAMUnknown, false
	}
	ram := *controller.AdapterRAM
	if ram <= 0 {
		return VRAMUnknown, false
	}
	// Values at or near the 4 GiB boundary have wrapped. Real cards report
	// 0xFFFFFFFF (4294967295) or 0xFFF00000 (4293918720) here, and even a
	// genuine 4 GB card cannot be represented, so the whole top of the range is
	// untrustworthy rather than just the exact ceiling.
	const trustworthyCeiling = int64(4*1024*1024*1024 - 64*1024*1024)
	if ram >= trustworthyCeiling {
		return VRAMUnknown, false
	}
	return ram, true
}

// SelectPrimaryController picks the adapter the node should serve on: a
// discrete NVIDIA or AMD card wins over integrated or unknown adapters, which
// is what a gaming host actually has plugged in.
func SelectPrimaryController(controllers []VideoController) (VideoController, string) {
	var fallback VideoController
	var fallbackVendor = VendorUnknown
	for idx, controller := range controllers {
		vendor := VendorForController(controller)
		if vendor == VendorNvidia || vendor == VendorAMD {
			return controller, vendor
		}
		if idx == 0 {
			fallback = controller
		}
	}
	if len(controllers) > 0 && fallback.Name == "" {
		fallback = controllers[0]
	}
	return fallback, fallbackVendor
}

// GPUsFromControllers converts adapters into the report shape. VRAM that could
// not be established is left as VRAMUnknown for the caller to describe.
func GPUsFromControllers(controllers []VideoController) []GPU {
	gpus := make([]GPU, 0, len(controllers))
	for _, controller := range controllers {
		vram, ok := AdapterVRAMBytes(controller)
		vramMB := VRAMUnknown
		if ok {
			vramMB = vram / 1024 / 1024
		}
		gpus = append(gpus, GPU{
			Name:          strings.TrimSpace(controller.Name),
			VRAMTotalMB:   vramMB,
			DriverVersion: strings.TrimSpace(controller.DriverVersion),
		})
	}
	return gpus
}

// DetectGPUVendor resolves the host GPU vendor for runtime selection. NVIDIA is
// probed first because nvidia-smi is authoritative when present; the WMI
// inventory is the fallback and is Windows-only. Anything unresolved is
// VendorUnknown, which makes callers use the bare platform artifact.
func DetectGPUVendor(ctx context.Context, runner CommandRunner, goos string) string {
	if runner == nil {
		runner = ExecRunner{}
	}
	if gpus, err := DetectNvidiaGPUs(ctx, runner); err == nil && len(gpus) > 0 {
		return VendorNvidia
	}
	if goos != "windows" {
		return VendorUnknown
	}
	controllers, err := DetectWindowsVideoControllers(ctx, runner)
	if err != nil {
		return VendorUnknown
	}
	_, vendor := SelectPrimaryController(controllers)
	return vendor
}
