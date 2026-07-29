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
	MinimumRAMBytes  = uint64(16 * 1024 * 1024 * 1024)
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
	gpus, nvidiaChecks := checkNvidia(ctx, opts)
	report.GPUs = gpus
	report.Checks = append(report.Checks, nvidiaChecks...)
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
		Remediation: "Run the host node on Windows 11 x64 with an NVIDIA GPU. macOS can still run local fake and CPU demo paths.",
	}
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
		result.Remediation = "Use a host with at least 16 GB RAM."
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
