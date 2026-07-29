package telemetry

import (
	"context"

	"github.com/anianroid/thirdshift/internal/shared/protocol"
)

type Provider interface {
	GPUStatus(ctx context.Context) (protocol.GPUStatus, error)
}
