//go:build !darwin

package platform

import (
	"context"

	"github.com/isty2e/daem/internal/platformsupport"
)

// Current returns no Darwin runtime evidence on a non-Darwin target.
func Current(ctx context.Context) (platformsupport.RuntimeObservation, error) {
	if err := ctx.Err(); err != nil {
		return platformsupport.RuntimeObservation{}, err
	}
	return platformsupport.RuntimeObservation{}, nil
}
