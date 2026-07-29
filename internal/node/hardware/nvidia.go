package hardware

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

var NvidiaSMIQueryArgs = []string{
	"--query-gpu=name,uuid,memory.total,driver_version,temperature.gpu,power.draw,power.limit,utilization.gpu",
	"--format=csv,noheader,nounits",
}

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

func DetectNvidiaGPUs(ctx context.Context, runner CommandRunner) ([]GPU, error) {
	output, err := runner.Run(ctx, "nvidia-smi", NvidiaSMIQueryArgs...)
	if err != nil {
		return nil, fmt.Errorf("run nvidia-smi: %w", err)
	}
	return ParseNvidiaSMICSV(string(output))
}

func ParseNvidiaSMICSV(input string) ([]GPU, error) {
	reader := csv.NewReader(strings.NewReader(input))
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse nvidia-smi csv: %w", err)
	}

	var gpus []GPU
	for idx, record := range records {
		if len(record) == 1 && strings.TrimSpace(record[0]) == "" {
			continue
		}
		if len(record) != 8 {
			return nil, fmt.Errorf("nvidia-smi row %d has %d fields, want 8", idx+1, len(record))
		}
		for fieldIdx, value := range record {
			if strings.TrimSpace(value) == "" {
				return nil, fmt.Errorf("nvidia-smi row %d field %d is empty", idx+1, fieldIdx+1)
			}
		}

		vram, err := parseIntField(record[2], "memory.total", idx)
		if err != nil {
			return nil, err
		}
		temp, err := parseIntField(record[4], "temperature.gpu", idx)
		if err != nil {
			return nil, err
		}
		powerDraw, err := parseFloatField(record[5], "power.draw", idx)
		if err != nil {
			return nil, err
		}
		powerLimit, err := parseFloatField(record[6], "power.limit", idx)
		if err != nil {
			return nil, err
		}
		utilization, err := parseIntField(record[7], "utilization.gpu", idx)
		if err != nil {
			return nil, err
		}

		gpus = append(gpus, GPU{
			Name:               strings.TrimSpace(record[0]),
			UUID:               strings.TrimSpace(record[1]),
			VRAMTotalMB:        int64(vram),
			DriverVersion:      strings.TrimSpace(record[3]),
			TemperatureC:       temp,
			PowerDrawW:         powerDraw,
			PowerLimitW:        powerLimit,
			UtilizationPercent: utilization,
		})
	}
	if len(gpus) == 0 {
		return nil, errors.New("nvidia-smi returned no GPU rows")
	}
	return gpus, nil
}

func parseIntField(value, name string, row int) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("nvidia-smi row %d %s=%q is not an integer", row+1, name, value)
	}
	return parsed, nil
}

func parseFloatField(value, name string, row int) (float64, error) {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0, fmt.Errorf("nvidia-smi row %d %s=%q is not a number", row+1, name, value)
	}
	return parsed, nil
}
