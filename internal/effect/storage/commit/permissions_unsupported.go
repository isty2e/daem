//go:build !darwin && !linux

package commit

import "context"

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
