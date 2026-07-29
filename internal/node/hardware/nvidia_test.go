package hardware

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseNvidiaSMISingleGPU(t *testing.T) {
	gpus := parseFixture(t, "nvidia_smi_single.csv")
	if len(gpus) != 1 {
		t.Fatalf("gpu count = %d, want 1", len(gpus))
	}
	if gpus[0].Name != "NVIDIA GeForce RTX 4070" {
		t.Fatalf("name = %q", gpus[0].Name)
	}
	if gpus[0].VRAMTotalMB != 12282 {
		t.Fatalf("vram = %d", gpus[0].VRAMTotalMB)
	}
	if gpus[0].PowerDrawW != 146.5 {
		t.Fatalf("power draw = %f", gpus[0].PowerDrawW)
	}
}

func TestParseNvidiaSMIMultiGPU(t *testing.T) {
	gpus := parseFixture(t, "nvidia_smi_multi.csv")
	if len(gpus) != 2 {
		t.Fatalf("gpu count = %d, want 2", len(gpus))
	}
	if gpus[1].Name != "NVIDIA GeForce RTX 4090" {
		t.Fatalf("second gpu = %q", gpus[1].Name)
	}
	if gpus[1].VRAMTotalMB != 24564 {
		t.Fatalf("second vram = %d", gpus[1].VRAMTotalMB)
	}
}

func TestParseNvidiaSMIMissingFields(t *testing.T) {
	data := readFixture(t, "nvidia_smi_missing.csv")
	if _, err := ParseNvidiaSMICSV(data); err == nil {
		t.Fatal("missing fields parsed successfully, want error")
	}
}

func TestParseNvidiaSMIGarbage(t *testing.T) {
	data := readFixture(t, "nvidia_smi_garbage.txt")
	if _, err := ParseNvidiaSMICSV(data); err == nil {
		t.Fatal("garbage input parsed successfully, want error")
	}
}

func parseFixture(t *testing.T, name string) []GPU {
	t.Helper()
	gpus, err := ParseNvidiaSMICSV(readFixture(t, name))
	if err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return gpus
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("../../../tests/fixtures", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}
