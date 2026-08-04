package hardware

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// GPU vendors the host node can serve on. Anything we cannot positively
// identify stays VendorUnknown rather than being guessed at.
const (
	VendorNvidia  = "nvidia"
	VendorAMD     = "amd"
	VendorApple   = "apple"
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
	if goos == "darwin" {
		if gpus, err := DetectAppleGPUs(ctx, runner); err == nil {
			if _, ok := SelectAppleGPU(gpus); ok {
				return VendorApple
			}
		}
		return VendorUnknown
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

// Apple Silicon detection. macOS exposes the GPU through system_profiler and
// unified memory through sysctl; both run behind CommandRunner so this is
// testable on any OS.
var (
	AppleDisplaysArgs = []string{"SPDisplaysDataType", "-json"}
	AppleMemsizeArgs  = []string{"-n", "hw.memsize"}
)

// AppleGPU is one entry from SPDisplaysDataType.
type AppleGPU struct {
	Name       string `json:"_name"`
	Model      string `json:"sppci_model"`
	Cores      string `json:"sppci_cores"`
	Vendor     string `json:"spdisplays_vendor"`
	DeviceType string `json:"sppci_device_type"`
}

type appleDisplaysReport struct {
	Displays []AppleGPU `json:"SPDisplaysDataType"`
}

// DetectAppleGPUs reads the macOS display adapter inventory.
func DetectAppleGPUs(ctx context.Context, runner CommandRunner) ([]AppleGPU, error) {
	output, err := runner.Run(ctx, "system_profiler", AppleDisplaysArgs...)
	if err != nil {
		return nil, fmt.Errorf("query SPDisplaysDataType: %w", err)
	}
	return ParseAppleDisplaysJSON(string(output))
}

func ParseAppleDisplaysJSON(input string) ([]AppleGPU, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil, fmt.Errorf("SPDisplaysDataType returned no output")
	}
	var report appleDisplaysReport
	if err := json.Unmarshal([]byte(trimmed), &report); err != nil {
		return nil, fmt.Errorf("parse SPDisplaysDataType: %w", err)
	}
	if len(report.Displays) == 0 {
		return nil, fmt.Errorf("SPDisplaysDataType returned no GPUs")
	}
	return report.Displays, nil
}

// IsAppleGPU reports whether an entry is an Apple Silicon integrated GPU.
func IsAppleGPU(gpu AppleGPU) bool {
	haystack := strings.ToLower(gpu.Vendor + " " + gpu.Model + " " + gpu.Name)
	return strings.Contains(haystack, "apple")
}

// SelectAppleGPU returns the Apple Silicon GPU from the inventory, if present.
func SelectAppleGPU(gpus []AppleGPU) (AppleGPU, bool) {
	for _, gpu := range gpus {
		if IsAppleGPU(gpu) {
			return gpu, true
		}
	}
	return AppleGPU{}, false
}

// DetectAppleUnifiedMemoryBytes reads total unified memory.
func DetectAppleUnifiedMemoryBytes(ctx context.Context, runner CommandRunner) (int64, error) {
	output, err := runner.Run(ctx, "sysctl", AppleMemsizeArgs...)
	if err != nil {
		return 0, fmt.Errorf("read hw.memsize: %w", err)
	}
	return ParseMemsize(string(output))
}

func ParseMemsize(input string) (int64, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return 0, fmt.Errorf("hw.memsize returned no output")
	}
	parsed, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("hw.memsize %q is not an integer", trimmed)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("hw.memsize %d is not a plausible memory size", parsed)
	}
	return parsed, nil
}

// AppleGPUMemoryBudgetPercent is the share of unified memory treated as usable
// GPU working set. Apple Silicon has no discrete VRAM: the GPU and the OS share
// one pool, and Metal's own recommended working set on a 16 GB machine is
// roughly 70 percent. We sit deliberately below that because a host Mac is
// somebody's daily-driver laptop, not a headless rig — auto-selection must not
// choose a model that makes the machine unusable while it serves. Hosts who
// want to push further can pin a larger model with --model.
const AppleGPUMemoryBudgetPercent = 65

// AppleGPUMemoryBudgetMB converts total unified memory into the working-set
// budget that gates model selection.
func AppleGPUMemoryBudgetMB(totalBytes int64) int64 {
	if totalBytes <= 0 {
		return VRAMUnknown
	}
	totalMB := totalBytes / 1024 / 1024
	return totalMB * AppleGPUMemoryBudgetPercent / 100
}
