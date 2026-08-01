package hardware

import "time"

const (
	StatusPass        = "pass"
	StatusFail        = "fail"
	StatusWarn        = "warn"
	StatusUnsupported = "unsupported"
	StatusSkipped     = "skipped"
)

type CheckResult struct {
	Name        string         `json:"name"`
	Status      string         `json:"status"`
	Message     string         `json:"message"`
	Remediation string         `json:"remediation,omitempty"`
	Details     map[string]any `json:"details,omitempty"`
}

type GPU struct {
	Name               string  `json:"name"`
	UUID               string  `json:"uuid"`
	VRAMTotalMB        int64   `json:"vram_total_mb"`
	DriverVersion      string  `json:"driver_version"`
	TemperatureC       int     `json:"temperature_c"`
	PowerDrawW         float64 `json:"power_draw_w"`
	PowerLimitW        float64 `json:"power_limit_w"`
	UtilizationPercent int     `json:"utilization_percent"`
}

type DoctorReport struct {
	GeneratedAt time.Time     `json:"generated_at"`
	OS          string        `json:"os"`
	Arch        string        `json:"arch"`
	Overall     string        `json:"overall"`
	GPUVendor   string        `json:"gpu_vendor,omitempty"`
	Checks      []CheckResult `json:"checks"`
	GPUs        []GPU         `json:"gpus,omitempty"`
}
