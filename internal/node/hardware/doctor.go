package hardware

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"
)

const (
	MinimumVRAMBytes = uint64(8 * 1024 * 1024 * 1024)
	// MinimumRAMBytes is the "16 GB installed" floor. Windows reports usable
	// RAM after hardware reservations, so a real 16 GB machine shows ~15.9 GB;
	// a strict 16 GiB compare would fail every 16 GB gaming PC. 15 GiB usable
	// only admits machines with at least 16 GB installed in practice.
	MinimumRAMBytes  = uint64(15 * 1024 * 1024 * 1024)
	MinimumDiskBytes = uint64(40 * 1024 * 1024 * 1024)
)

type DoctorOptions struct {
	DiskPath string
	HTTPSURL string
	WSSURL   string
	Timeout  time.Duration
	Runner   CommandRunner
}

func RunDoctor(ctx context.Context, opts DoctorOptions) DoctorReport {
	if opts.DiskPath == "" {
		opts.DiskPath = "."
	}
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Second
	}
	if opts.Runner == nil {
		opts.Runner = ExecRunner{}
	}

	report := DoctorReport{
		GeneratedAt: time.Now().UTC(),
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
	}

	report.Checks = append(report.Checks, checkPlatform())
	gpus, vendor, gpuChecks := checkGPU(ctx, opts)
	report.GPUs = gpus
	report.GPUVendor = vendor
	report.Checks = append(report.Checks, gpuChecks...)
	report.Checks = append(report.Checks, checkRAM())
	report.Checks = append(report.Checks, checkDisk(opts.DiskPath))
	report.Checks = append(report.Checks, checkHTTPS(ctx, opts.HTTPSURL, opts.Timeout))
	report.Checks = append(report.Checks, checkWSS(ctx, opts.WSSURL, opts.Timeout))
	report.Overall = overallStatus(report.Checks)

	return report
}

func checkPlatform() CheckResult {
	if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
		return CheckResult{
			Name:    "platform",
			Status:  StatusPass,
			Message: "Windows x64 host platform detected.",
		}
	}
	return CheckResult{
		Name:        "platform",
		Status:      StatusUnsupported,
		Message:     fmt.Sprintf("%s/%s is not a supported host platform for v0.1.", runtime.GOOS, runtime.GOARCH),
		Remediation: "Run the host node on Windows 11 x64 with an NVIDIA or AMD GPU. macOS can still run local fake and CPU demo paths.",
	}
}

// checkGPU resolves the host GPU. NVIDIA is attempted first and its result
// path is unchanged; AMD is only consulted when nvidia-smi does not answer, so
// an NVIDIA host produces byte-identical output to before AMD support existed.
func checkGPU(ctx context.Context, opts DoctorOptions) ([]GPU, string, []CheckResult) {
	gpus, checks := checkNvidia(ctx, opts)
	if len(gpus) > 0 {
		return gpus, VendorNvidia, checks
	}
	if runtime.GOOS != "windows" {
		return gpus, VendorUnknown, checks
	}
	amdGPUs, amdChecks, ok := checkAMD(ctx, opts)
	if !ok {
		return gpus, VendorUnknown, checks
	}
	return amdGPUs, VendorAMD, amdChecks
}

// checkAMD reports whether an AMD adapter was found, and the checks to use in
// place of the NVIDIA failure pair when one was.
func checkAMD(ctx context.Context, opts DoctorOptions) ([]GPU, []CheckResult, bool) {
	controllers, err := DetectWindowsVideoControllers(ctx, opts.Runner)
	if err != nil {
		return nil, nil, false
	}
	primary, vendor := SelectPrimaryController(controllers)
	if vendor != VendorAMD {
		return nil, nil, false
	}

	gpus := GPUsFromControllers(controllers)
	gpuCheck := CheckResult{
		Name:    "gpu",
		Status:  StatusPass,
		Message: fmt.Sprintf("AMD GPU detected (%s); llama.cpp will use the Vulkan backend.", strings.TrimSpace(primary.Name)),
		Details: map[string]any{
			"vendor":         VendorAMD,
			"backend":        "vulkan",
			"driver_version": strings.TrimSpace(primary.DriverVersion),
		},
	}

	// AdapterRAM is a 32-bit field, so it cannot be trusted above 4 GB. Say so
	// rather than inventing a number that could wrongly pass or fail the floor.
	vramBytes, known := AdapterVRAMBytes(primary)
	vram := CheckResult{
		Name:   "vram",
		Status: StatusWarn,
		Message: "AMD VRAM could not be established reliably: Windows reports AdapterRAM as a 32-bit value, " +
			"so cards above 4 GB are truncated.",
		Remediation: "Confirm the card has at least 8 GB VRAM. Reported capacity is verified from the Vulkan device report when the runtime starts.",
		Details: map[string]any{
			"vendor":     VendorAMD,
			"minimum_gb": bytesToGiB(MinimumVRAMBytes),
			"source":     "win32_videocontroller",
		},
	}
	if known {
		vram.Details["reported_gb"] = bytesToGiB(uint64(vramBytes))
		if uint64(vramBytes) >= MinimumVRAMBytes {
			vram.Status = StatusPass
			vram.Message = fmt.Sprintf("AMD GPU reports %.1f GB VRAM.", bytesToGiB(uint64(vramBytes)))
			vram.Remediation = ""
		} else {
			vram.Message = fmt.Sprintf(
				"AMD GPU reports %.1f GB VRAM, below the 8 GB floor, but AdapterRAM truncates above 4 GB so this may understate the card.",
				bytesToGiB(uint64(vramBytes)))
		}
	}

	return gpus, []CheckResult{gpuCheck, vram}, true
}

func checkNvidia(ctx context.Context, opts DoctorOptions) ([]GPU, []CheckResult) {
	if runtime.GOOS != "windows" {
		return nil, []CheckResult{
			{
				Name:        "nvidia",
				Status:      StatusUnsupported,
				Message:     "NVIDIA host detection is only required on the Windows target platform.",
				Remediation: "Use the fake runtime or macOS CPU demo path locally; run this check on Windows 11 x64 before hosting.",
			},
			{
				Name:        "vram",
				Status:      StatusUnsupported,
				Message:     "VRAM eligibility is evaluated through nvidia-smi on Windows.",
				Remediation: "Run on a Windows NVIDIA host with at least 8 GB VRAM.",
			},
		}
	}

	gpus, err := DetectNvidiaGPUs(ctx, opts.Runner)
	if err != nil {
		return nil, []CheckResult{
			{
				Name:        "nvidia",
				Status:      StatusFail,
				Message:     err.Error(),
				Remediation: "Install or repair the NVIDIA driver and ensure nvidia-smi is on PATH.",
			},
			{
				Name:        "vram",
				Status:      StatusFail,
				Message:     "No NVIDIA GPU data was available.",
				Remediation: "Run nvidia-smi successfully before starting the host node.",
			},
		}
	}

	var maxVRAM uint64
	for _, gpu := range gpus {
		if uint64(gpu.VRAMTotalMB)*1024*1024 > maxVRAM {
			maxVRAM = uint64(gpu.VRAMTotalMB) * 1024 * 1024
		}
	}

	vram := CheckResult{
		Name:    "vram",
		Status:  StatusPass,
		Message: fmt.Sprintf("Largest NVIDIA GPU has %.1f GB VRAM.", bytesToGiB(maxVRAM)),
		Details: map[string]any{
			"minimum_gb": bytesToGiB(MinimumVRAMBytes),
			"largest_gb": bytesToGiB(maxVRAM),
		},
	}
	if maxVRAM < MinimumVRAMBytes {
		vram.Status = StatusFail
		vram.Remediation = "Use an NVIDIA GPU with at least 8 GB VRAM."
	}

	return gpus, []CheckResult{
		{
			Name:    "nvidia",
			Status:  StatusPass,
			Message: fmt.Sprintf("%d NVIDIA GPU(s) detected with nvidia-smi CSV query.", len(gpus)),
		},
		vram,
	}
}

func checkRAM() CheckResult {
	total, err := totalMemoryBytes()
	if err != nil {
		return CheckResult{
			Name:        "ram",
			Status:      StatusWarn,
			Message:     err.Error(),
			Remediation: "Confirm the host has at least 16 GB RAM.",
		}
	}
	result := CheckResult{
		Name:    "ram",
		Status:  StatusPass,
		Message: fmt.Sprintf("System RAM is %.1f GB.", bytesToGiB(total)),
		Details: map[string]any{
			"minimum_gb": bytesToGiB(MinimumRAMBytes),
			"total_gb":   bytesToGiB(total),
		},
	}
	if total < MinimumRAMBytes {
		result.Status = StatusFail
		result.Remediation = "Use a host with at least 16 GB installed RAM (the check allows for OS-reserved memory)."
	}
	return result
}

func checkDisk(path string) CheckResult {
	free, err := freeDiskBytes(path)
	if err != nil {
		return CheckResult{
			Name:        "disk",
			Status:      StatusWarn,
			Message:     err.Error(),
			Remediation: "Confirm the model cache path has at least 40 GB free.",
		}
	}
	result := CheckResult{
		Name:    "disk",
		Status:  StatusPass,
		Message: fmt.Sprintf("Free disk at %s is %.1f GB.", path, bytesToGiB(free)),
		Details: map[string]any{
			"path":       path,
			"minimum_gb": bytesToGiB(MinimumDiskBytes),
			"free_gb":    bytesToGiB(free),
		},
	}
	if free < MinimumDiskBytes {
		result.Status = StatusFail
		result.Remediation = "Free at least 40 GB on the configured model cache volume."
	}
	return result
}

func checkHTTPS(ctx context.Context, rawurl string, timeout time.Duration) CheckResult {
	if rawurl == "" {
		return CheckResult{
			Name:        "https",
			Status:      StatusSkipped,
			Message:     "No HTTPS endpoint configured for reachability check.",
			Remediation: "Set THIRDSHIFT_DOCTOR_HTTPS_URL or pass --https-url.",
		}
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodHead, rawurl, nil)
	if err != nil {
		return CheckResult{Name: "https", Status: StatusFail, Message: err.Error(), Remediation: "Use a valid HTTPS URL."}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return CheckResult{Name: "https", Status: StatusFail, Message: err.Error(), Remediation: "Allow outbound HTTPS traffic."}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 500 {
		return CheckResult{Name: "https", Status: StatusPass, Message: fmt.Sprintf("Reached %s with HTTP %d.", rawurl, resp.StatusCode)}
	}
	return CheckResult{Name: "https", Status: StatusFail, Message: fmt.Sprintf("HTTP status %d from %s.", resp.StatusCode, rawurl), Remediation: "Verify outbound HTTPS access."}
}

func checkWSS(ctx context.Context, rawurl string, timeout time.Duration) CheckResult {
	if rawurl == "" {
		return CheckResult{
			Name:        "wss",
			Status:      StatusSkipped,
			Message:     "No WSS endpoint configured for reachability check.",
			Remediation: "Set THIRDSHIFT_DOCTOR_WSS_URL or pass --wss-url.",
		}
	}

	parsed, err := url.Parse(rawurl)
	if err != nil || parsed.Scheme != "wss" || parsed.Host == "" {
		return CheckResult{Name: "wss", Status: StatusFail, Message: "WSS URL is invalid.", Remediation: "Use a wss:// URL."}
	}
	host := parsed.Host
	if !strings.Contains(host, ":") {
		host += ":443"
	}

	dialer := tls.Dialer{NetDialer: &net.Dialer{Timeout: timeout}}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := dialer.DialContext(reqCtx, "tcp", host)
	if err != nil {
		return CheckResult{Name: "wss", Status: StatusFail, Message: err.Error(), Remediation: "Allow outbound TLS traffic to the coordinator WebSocket endpoint."}
	}
	defer conn.Close()

	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		return CheckResult{Name: "wss", Status: StatusFail, Message: err.Error()}
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	if parsed.RawQuery != "" {
		path += "?" + parsed.RawQuery
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	_, err = fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", path, parsed.Host, key)
	if err != nil {
		return CheckResult{Name: "wss", Status: StatusFail, Message: err.Error(), Remediation: "Allow outbound WSS traffic."}
	}
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return CheckResult{Name: "wss", Status: StatusFail, Message: err.Error(), Remediation: "Allow outbound WSS traffic."}
	}
	if strings.Contains(line, " 101 ") {
		return CheckResult{Name: "wss", Status: StatusPass, Message: fmt.Sprintf("Reached %s and received WebSocket upgrade.", rawurl)}
	}
	return CheckResult{Name: "wss", Status: StatusFail, Message: strings.TrimSpace(line), Remediation: "Verify the endpoint supports WebSocket upgrades."}
}

func overallStatus(checks []CheckResult) string {
	unsupported := false
	warn := false
	for _, check := range checks {
		switch check.Status {
		case StatusFail:
			return StatusFail
		case StatusUnsupported:
			unsupported = true
		case StatusWarn, StatusSkipped:
			warn = true
		}
	}
	if unsupported {
		return StatusUnsupported
	}
	if warn {
		return StatusWarn
	}
	return StatusPass
}

func bytesToGiB(bytes uint64) float64 {
	return float64(bytes) / 1024 / 1024 / 1024
}
