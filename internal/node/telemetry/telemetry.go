package telemetry

import (
	"context"

	"github.com/Ani-HQ/thirdshift/internal/shared/protocol"
)

type Provider interface {
	GPUStatus(ctx context.Context) (protocol.GPUStatus, error)
}
