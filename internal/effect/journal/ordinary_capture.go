package journal

import (
	"context"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
)

func buildRecoveryJournal(ctx context.Context, paths Paths, operationDir, operationID string, createdAt time.Time, currentState, nextState durable.Snapshot, options CaptureOptions) (recoveryJournal, error) {
	material, err := buildOrdinaryCaptureMaterial(ctx, paths, operationDir, operationID, createdAt, currentState, nextState, options, &ordinaryCaptureWork{})
	if err != nil {
		return recoveryJournal{}, err
	}
	return material.ordinaryJournal()
}
