//go:build !darwin && !linux

package commit

import (
	"context"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func ObserveRootedFilePermissionMetadata(ctx context.Context, capability rootedpath.CommitCapability) (FilePermissionMetadata, error) {
	if capability != nil {
		defer capability.Close()
	}
	return FilePermissionMetadata{}, newFailure(failureUnsupportedGuarantee, phaseUnsupported, "",
		unsupported("rooted permission observation is unsupported on this platform", nil))
}

func InspectRootedFilePermissions(ctx context.Context, request RootedPermissionRestriction) (PermissionRestrictionResult, error) {
	return RestrictRootedFilePermissions(ctx, request)
}

func RestrictRootedFilePermissions(ctx context.Context, request RootedPermissionRestriction) (PermissionRestrictionResult, error) {
	if request.capability != nil {
		defer request.capability.Close()
	}
	return PermissionRestrictionResult{}, newFailure(failureUnsupportedGuarantee, phaseUnsupported, request.path,
		unsupported("rooted permission restriction is unsupported on this platform", nil))
}
