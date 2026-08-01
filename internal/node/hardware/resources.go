package hardware

import (
	"context"
	"fmt"
)

// HostResources is what the node knows about its own capacity, in megabytes.
// VRAMTotalMB is VRAMUnknown when it could not be established reliably, which
// is a normal outcome on AMD (see AdapterVRAMBytes) and must not be treated as
// "no GPU memory".
type HostResources struct {
	GPUVendor   string
	GPUName     string
	VRAMTotalMB int64
	RAMTotalMB  int64
	DiskFreeMB  int64
}

// VRAMKnown reports whether VRAM was established well enough to gate on.
func (h HostResources) VRAMKnown() bool {
	return h.VRAMTotalMB > 0
}

// DetectHostResources gathers what the node can measure about itself. RAM and
// disk failures are returned as an error because they are measurable on every
// supported platform; VRAM is allowed to be unknown.
func DetectHostResources(ctx context.Context, runner CommandRunner, diskPath, goos string) (HostResources, error) {
	if runner == nil {
		runner = ExecRunner{}
	}
	if diskPath == "" {
		diskPath = "."
	}

	resources := HostResources{VRAMTotalMB: VRAMUnknown, GPUVendor: VendorUnknown}

	if gpus, err := DetectNvidiaGPUs(ctx, runner); err == nil && len(gpus) > 0 {
		resources.GPUVendor = VendorNvidia
		for _, gpu := range gpus {
			if gpu.VRAMTotalMB > resources.VRAMTotalMB {
				resources.VRAMTotalMB = gpu.VRAMTotalMB
				resources.GPUName = gpu.Name
			}
		}
	} else if goos == "windows" {
		if controllers, err := DetectWindowsVideoControllers(ctx, runner); err == nil {
			primary, vendor := SelectPrimaryController(controllers)
			resources.GPUVendor = vendor
			resources.GPUName = primary.Name
			if bytes, ok := AdapterVRAMBytes(primary); ok {
				resources.VRAMTotalMB = bytes / 1024 / 1024
			}
		}
	}

	ram, err := totalMemoryBytes()
	if err != nil {
		return resources, fmt.Errorf("read system memory: %w", err)
	}
	resources.RAMTotalMB = int64(ram / 1024 / 1024)

	disk, err := freeDiskBytes(diskPath)
	if err != nil {
		return resources, fmt.Errorf("read free disk at %s: %w", diskPath, err)
	}
	resources.DiskFreeMB = int64(disk / 1024 / 1024)

	return resources, nil
}
