//go:build windows

package telemetry

import (
	"context"
	"encoding/csv"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/Ani-HQ/thirdshift/internal/shared/protocol"
)

func DefaultProvider() Provider {
	return NvidiaSMIProvider{}
}

type NvidiaSMIProvider struct{}

func (NvidiaSMIProvider) GPUStatus(ctx context.Context) (protocol.GPUStatus, error) {
	cmd := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=name,memory.total,memory.free,temperature.gpu,power.draw,power.limit,utilization.gpu",
		"--format=csv,noheader,nounits",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return protocol.GPUStatus{}, fmt.Errorf("run nvidia-smi telemetry query: %w", err)
	}
	reader := csv.NewReader(strings.NewReader(string(output)))
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return protocol.GPUStatus{}, fmt.Errorf("parse nvidia-smi telemetry csv: %w", err)
	}
	for _, record := range records {
		if len(record) == 1 && strings.TrimSpace(record[0]) == "" {
			continue
		}
		if len(record) != 7 {
			return protocol.GPUStatus{}, fmt.Errorf("nvidia-smi telemetry row has %d fields, want 7", len(record))
		}
		total, err := atoi(record[1])
		if err != nil {
			return protocol.GPUStatus{}, err
		}
		free, err := atoi(record[2])
		if err != nil {
			return protocol.GPUStatus{}, err
		}
		temp, err := atoi(record[3])
		if err != nil {
			return protocol.GPUStatus{}, err
		}
		power, err := atofToInt(record[4])
		if err != nil {
			return protocol.GPUStatus{}, err
		}
		limit, err := atofToInt(record[5])
		if err != nil {
			return protocol.GPUStatus{}, err
		}
		util, err := atoi(record[6])
		if err != nil {
			return protocol.GPUStatus{}, err
		}
		return protocol.GPUStatus{
			Name:               strings.TrimSpace(record[0]),
			VRAMTotalMB:        int64(total),
			VRAMFreeMB:         int64(free),
			TemperatureC:       temp,
			PowerW:             power,
			PowerLimitW:        limit,
			UtilizationPercent: util,
		}, nil
	}
	return protocol.GPUStatus{}, fmt.Errorf("nvidia-smi telemetry query returned no GPU rows")
}

func atoi(value string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("parse telemetry integer %q: %w", value, err)
	}
	return parsed, nil
}

func atofToInt(value string) (int, error) {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0, fmt.Errorf("parse telemetry number %q: %w", value, err)
	}
	return int(parsed), nil
}
