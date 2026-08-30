package recoverygate

import (
	"context"
	"fmt"
)

func requireBarrierContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("recovery barrier context is required")
	}
	return ctx.Err()
}
