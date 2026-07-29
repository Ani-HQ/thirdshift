//go:build !windows

package telemetry

import (
	"context"

	"github.com/anianroid/thirdshift/internal/shared/protocol"
)

func DefaultProvider() Provider {
	return FakeProvider{}
}

type FakeProvider struct{}

func (FakeProvider) GPUStatus(context.Context) (protocol.GPUStatus, error) {
	return protocol.GPUStatus{
		Name:               "unsupported-platform-fake",
		VRAMTotalMB:        0,
		VRAMFreeMB:         0,
		TemperatureC:       0,
		PowerW:             0,
		PowerLimitW:        0,
		UtilizationPercent: 0,
	}, nil
}
