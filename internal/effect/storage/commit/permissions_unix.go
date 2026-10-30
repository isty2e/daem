//go:build darwin || linux

package commit

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// RestrictRootedFilePermissions reads metadata only and never restores removed
// access. ctx must carry the caller's independently admitted safety allowance.
func RestrictRootedFilePermissions(ctx context.Context, request RootedPermissionRestriction) (PermissionRestrictionResult, error) {
	return restrictRootedFilePermissions(ctx, request, faultPlan{})
}

// InspectRootedFilePermissions shares the same-FD qualification but performs no
// chmod, payload read, sync or durable absence confirmation.
func InspectRootedFilePermissions(ctx context.Context, request RootedPermissionRestriction) (PermissionRestrictionResult, error) {
	return inspectOrRestrictRootedPermissions(ctx, request, faultPlan{}, false)
}

func restrictRootedFilePermissions(ctx context.Context, request RootedPermissionRestriction, faults faultPlan) (PermissionRestrictionResult, error) {
	return inspectOrRestrictRootedPermissions(ctx, request, faults, true)
}

func inspectOrRestrictRootedPermissions(ctx context.Context, request RootedPermissionRestriction, faults faultPlan, mutate bool) (PermissionRestrictionResult, error) {
	var result PermissionRestrictionResult
	if request.capability != nil {
		defer request.capability.Close()
	}
	if ctx == nil {
		return result, newFailure(failureUncommitted, phaseValidate, request.path, fmt.Errorf("permission restriction context is required"))
	}
	if err := request.validate(); err != nil {
		return result, failureBeforeVisibility(phaseValidate, request.path, err)
	}
	if err := faults.check(ctx, phaseValidate); err != nil {
		return result, failureBeforeVisibility(phaseValidate, request.path, err)
	}
	if err := request.capability.AdmitPhysicalWork(0, 1, 0); err != nil {
		return result, failureBeforeVisibility(phaseValidate, request.path, err)
	}
	anchor, err := openReadParent(request.path, request.capability)
	if anchor != nil {
		defer anchor.close()
	}
	if err != nil {
		if !mutate && errors.Is(err, unix.ENOENT) {
			return inspectPermissionAbsence(ctx, request, err)
		}
		return result, failureBeforeVisibility(phaseCaptureIdentity, request.path, err)
	}
	expected, _, err := anchor.observe(anchor.base, request.path)
	if err != nil {
		if !mutate && errors.Is(err, unix.ENOENT) {
			return inspectPermissionAbsence(ctx, request, err)
		}
		return result, failureBeforeVisibility(phaseCaptureIdentity, request.path, err)
	}
	if expected.kind == entryKindSymlink {
		return result, failureBeforeVisibility(phaseValidate, request.path, rootedFinalSymlinkFailure(request.path))
	}
	if expected.kind != entryKindRegular {
		return result, failureBeforeVisibility(phaseValidate, request.path, unsupported("permission target is not a regular file", nil))
	}
	fd, err := openExpectedAt(anchor.parentFD(), anchor.base, request.path, expected)
	if err != nil {
		return result, failureBeforeVisibility(phaseCaptureIdentity, request.path, err)
	}
	defer unix.Close(fd)
	if err := request.capability.ValidateDirectoryHandle(uintptr(fd)); err != nil {
		return result, failureBeforeVisibility(phaseValidate, request.path, err)
	}
	if err := faults.check(ctx, phaseCaptureMetadata); err != nil {
		return result, failureBeforeVisibility(phaseCaptureMetadata, request.path, err)
	}
	var before unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil {
		return result, failureBeforeVisibility(phaseCaptureMetadata, request.path, err)
	}
	if err := validatePermissionTargetMetadata(&before); err != nil {
		return result, failureBeforeVisibility(phaseCaptureMetadata, request.path, err)
	}
	metadata, err := capturePreservedMetadata(fd, &before)
	if err != nil {
		return result, failureBeforeVisibility(phaseCaptureMetadata, request.path, err)
	}
	desired, expanded, err := request.restriction(before.Uid, before.Gid, uint32(before.Mode)&0o777)
	if err != nil {
		return result, failureBeforeVisibility(phaseCaptureMetadata, request.path, err)
	}
	result.expanded = expanded
	if !mutate {
		if _, _, err := anchor.requireExpected(anchor.base, request.path, expected); err != nil {
			return result, failureBeforeVisibility(phaseRevalidateEntry, request.path, err)
		}
		if err := anchor.verifyChain(); err != nil {
			return result, failureBeforeVisibility(phaseRevalidateEntry, request.path, err)
		}
		return result, nil
	}
	attempted := false
	fail := func(current phase, cause error) (PermissionRestrictionResult, error) {
		if attempted {
			return result, newFailure(failureIndeterminateCommit, current, request.path, cause)
		}
		return result, failureBeforeVisibility(current, request.path, cause)
	}
	if result.expanded {
		if err := faults.check(ctx, phaseApplyMode); err != nil {
			return fail(phaseApplyMode, err)
		}
		if err := ctx.Err(); err != nil {
			return fail(phaseApplyMode, err)
		}
		if _, _, err := anchor.requireExpected(anchor.base, request.path, expected); err != nil {
			return fail(phaseRevalidateEntry, err)
		}
		if err := anchor.verifyChain(); err != nil {
			return fail(phaseRevalidateEntry, err)
		}
		attempted = true
		if err := unix.Fchmod(fd, desired); err != nil {
			return fail(phaseApplyMode, err)
		}
		result.restricted = true
		if err := faults.effectResult(phaseApplyMode, nil); err != nil {
			return fail(phaseApplyMode, err)
		}
	}
	if err := faults.run(ctx, phaseSyncPayload, func() error { return syncPayload(fd) }); err != nil {
		return fail(phaseSyncPayload, err)
	}
	if err := faults.check(ctx, phaseVerifyEntry); err != nil {
		return fail(phaseVerifyEntry, err)
	}
	var after unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil {
		return fail(phaseVerifyEntry, err)
	}
	if err := validatePermissionTargetMetadata(&after); err != nil {
		return fail(phaseVerifyEntry, err)
	}
	current := identityFromStat(request.path, &after)
	if !expected.sameObject(current) || uint32(after.Mode)&0o777 != desired {
		return fail(phaseVerifyEntry, fmt.Errorf("permission restriction did not verify on its opened file"))
	}
	_, stillExpanded, err := request.restriction(after.Uid, after.Gid, uint32(after.Mode)&0o777)
	if err != nil {
		return fail(phaseVerifyEntry, err)
	}
	if stillExpanded {
		return fail(phaseVerifyEntry, fmt.Errorf("permission policy did not settle after restriction"))
	}
	if err := verifyPreservedMetadata(fd, metadata); err != nil {
		return fail(phaseVerifyEntry, err)
	}
	if _, _, err := anchor.requireExpected(anchor.base, request.path, current); err != nil {
		return fail(phaseVerifyEntry, err)
	}
	if err := anchor.verifyChain(); err != nil {
		return fail(phaseVerifyEntry, err)
	}
	if err := request.capability.ValidateDirectoryHandle(uintptr(fd)); err != nil {
		return fail(phaseVerifyEntry, err)
	}
	return result, nil
}

func inspectPermissionAbsence(ctx context.Context, request RootedPermissionRestriction, cause error) (PermissionRestrictionResult, error) {
	observation, err := observeRootedAbsence(ctx, request.capability)
	if observation != nil {
		defer observation.close()
		if err == nil && observation.absent {
			return PermissionRestrictionResult{absent: true}, nil
		}
	}
	return PermissionRestrictionResult{}, failureBeforeVisibility(phaseCaptureIdentity, request.path, errors.Join(cause, err))
}

func validatePermissionTargetMetadata(stat *unix.Stat_t) error {
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 {
		return unsupported("permission restriction requires one regular-file link", nil)
	}
	if stat.Mode&(unix.S_ISUID|unix.S_ISGID|unix.S_ISVTX) != 0 {
		return unsupported("permission target contains unsupported special permission bits", nil)
	}
	return nil
}
