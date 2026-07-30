//go:build !darwin && !linux

package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func acquireRootedAdvisoryLock(
	_ context.Context,
	capability rootedpath.CommitCapability,
	_ time.Duration,
) (lockReleaser, error) {
	if capability != nil {
		_ = capability.Close()
	}
	return nil, fmt.Errorf("rooted cache locks are unsupported on this platform")
}
