package cache

import (
	"context"
	"fmt"
)

func validateContext(ctx context.Context, operation string) error {
	if ctx == nil {
		return fmt.Errorf("cache context is required for %s", operation)
	}

	return nil
}
