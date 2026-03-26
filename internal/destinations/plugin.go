package destinations

import (
"context"

"github.com/shawnsammartano-hub/bubblefish/internal/core"
)

type DestinationPlugin interface {
Name() string
Connect(config map[string]string) error
Write(ctx context.Context, p core.TranslatedPayload) error
Read(ctx context.Context, q core.QueryRequest) ([]core.TranslatedPayload, error)
HealthCheck(ctx context.Context) error
Close() error
}
