package hardware

import (
	"encoding/json"
	"fmt"
	"io"
)

func WriteJSON(w io.Writer, report DoctorReport) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func WriteHuman(w io.Writer, report DoctorReport) {
	fmt.Fprintf(w, "Thirdshift doctor\n")
	fmt.Fprintf(w, "Platform: %s/%s\n", report.OS, report.Arch)
	fmt.Fprintf(w, "Overall: %s\n\n", report.Overall)
	for _, check := range report.Checks {
		fmt.Fprintf(w, "[%s] %s: %s\n", check.Status, check.Name, check.Message)
		if check.Remediation != "" {
			fmt.Fprintf(w, "  fix: %s\n", check.Remediation)
		}
	}
	if len(report.GPUs) > 0 {
		fmt.Fprintln(w, "\nGPUs:")
		for _, gpu := range report.GPUs {
			fmt.Fprintf(w, "- %s, %d MB VRAM, driver %s, temp %d C, power %.1f/%.1f W, util %d%%\n",
				gpu.Name,
				gpu.VRAMTotalMB,
				gpu.DriverVersion,
				gpu.TemperatureC,
				gpu.PowerDrawW,
				gpu.PowerLimitW,
				gpu.UtilizationPercent,
			)
		}
	}
}
