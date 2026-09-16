package fileset

import (
	"context"
	"fmt"
)

// RecoveryDisposition distinguishes completion from restoration; neither
// outcome authorizes retrying the original operation automatically.
type RecoveryDisposition string

const (
	RecoveryFinalize RecoveryDisposition = "finalize"
	RecoveryRollback RecoveryDisposition = "rollback"
)

// InspectFileSetRecovery previews recovery for an exact operation-owned target
// set. The caller must revalidate its evidence under leases before recovery.
func InspectFileSetRecovery(ctx context.Context, stateDir string, exactPaths []string) (RecoveryDisposition, error) {
	if ctx == nil {
		return "", fmt.Errorf("file-set recovery context is required")
	}
	canonical, err := canonicalStateDir(stateDir)
	if err != nil {
		return "", err
	}
	marker, err := loadMarker(ctx, markerPath(canonical))
	if err != nil {
		return "", err
	}
	allowed, err := canonicalAllowedPaths(exactPaths)
	if err != nil {
		return "", err
	}
	if len(allowed) != len(marker.Targets) {
		return "", fmt.Errorf("interrupted file-set target set does not match this operation")
	}
	for _, target := range marker.Targets {
		if _, ok := allowed[target.Path]; !ok {
			return "", fmt.Errorf("interrupted file-set target %q does not belong to this operation", target.Path)
		}
	}
	if err := rejectAbandonedFileSetResidue(ctx, canonical); err != nil {
		return "", err
	}
	classification, err := classifyTargets(ctx, marker)
	if err != nil {
		return "", err
	}
	if classification.cleanAfter {
		return RecoveryFinalize, nil
	}
	if !classification.recoverable {
		return "", fmt.Errorf("interrupted file-set contains state outside its before/after evidence")
	}
	if err := preflightRestorableBackups(ctx, marker); err != nil {
		return "", err
	}
	return RecoveryRollback, nil
}
