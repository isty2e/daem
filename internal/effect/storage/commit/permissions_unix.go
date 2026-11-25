//go:build darwin || linux

package commit

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/unix"
)

// ObserveRootedFilePermissionMetadata consumes the capability and returns actual
// same-FD facts without changing permissions, reading payload or syncing.
func ObserveRootedFilePermissionMetadata(ctx context.Context, capability rootedpath.CommitCapability) (FilePermissionMetadata, error) {
	return observeRootedFilePermissionMetadata(ctx, capability, faultPlan{})
}

func observeRootedFilePermissionMetadata(ctx context.Context, capability rootedpath.CommitCapability, faults faultPlan) (FilePermissionMetadata, error) {
	var metadata FilePermissionMetadata
	request, err := NewRootedPermissionRestriction(capability, func(uid, gid, mode uint32) (uint32, bool, error) {
		metadata = FilePermissionMetadata{UserID: uid, GroupID: gid, Mode: mode}
		return mode, false, nil
	})
	if err != nil {
		if capability != nil {
			err = errors.Join(err, capability.Close())
		}
		return FilePermissionMetadata{}, err
	}
	result, err := inspectOrRestrictRootedPermissions(ctx, request, faults, false)
	if err != nil {
		return FilePermissionMetadata{}, err
	}
	if result.ObservedAbsent() {
		return FilePermissionMetadata{}, failureBeforeVisibility(phaseCaptureMetadata, request.path, fs.ErrNotExist)
	}
	return metadata, nil
}

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
	fd, err := unix.Openat(anchor.parentFD(), anchor.base, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return result, failureBeforeVisibility(phaseCaptureIdentity, request.path, err)
	}
	defer unix.Close(fd)
	if err := faults.check(ctx, phaseCaptureMetadata); err != nil {
		return result, failureBeforeVisibility(phaseCaptureMetadata, request.path, err)
	}
	_, expanded, err := request.inspectOpenedPermissions(anchor, fd, expected)
	if err != nil {
		return result, failureBeforeVisibility(phaseCaptureMetadata, request.path, err)
	}
	result.expanded = expanded
	if !mutate {
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
		desired, stillExpanded, err := request.inspectOpenedPermissions(anchor, fd, expected)
		if err != nil {
			return fail(phaseRevalidateEntry, err)
		}
		if stillExpanded {
			attempted = true
			if err := unix.Fchmod(fd, desired); err != nil {
				return fail(phaseApplyMode, err)
			}
			result.restricted = true
			if err := faults.effectResult(phaseApplyMode, nil); err != nil {
				return fail(phaseApplyMode, err)
			}
		}
	}

	if err := faults.run(ctx, phaseSyncPayload, func() error { return syncPayload(fd) }); err != nil {
		return fail(phaseSyncPayload, err)
	}
	if err := faults.check(ctx, phaseVerifyEntry); err != nil {
		return fail(phaseVerifyEntry, err)
	}
	_, stillExpanded, err := request.inspectOpenedPermissions(anchor, fd, expected)
	if err != nil {
		return fail(phaseVerifyEntry, err)
	}
	if stillExpanded {
		return fail(phaseVerifyEntry, fmt.Errorf("permission policy did not settle after restriction"))
	}
	return result, nil
}

func (request RootedPermissionRestriction) inspectOpenedPermissions(anchor *anchoredParent, fd int, expected EntryIdentity) (uint32, bool, error) {
	if err := request.capability.ValidateDirectoryHandle(uintptr(fd)); err != nil {
		return 0, false, err
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return 0, false, err
	}
	opened := identityFromStat(request.path, &stat)
	if !expected.sameObject(opened) {
		return 0, false, fmt.Errorf("permission target changed while opening %q", request.path)
	}
	desired, expanded, err := request.restrictionOnFile(fd, &stat)
	if err != nil {
		return 0, false, err
	}
	if err := anchor.verifyChain(); err != nil {
		return 0, false, err
	}
	named, current, err := anchor.observe(anchor.base, request.path)
	if err != nil {
		return 0, false, err
	}
	if !opened.sameObject(named) {
		return 0, false, fmt.Errorf("permission target at %q no longer names the opened file", request.path)
	}
	if !opened.sameEntry(named) {
		return request.restrictionOnFile(fd, &current)
	}
	return desired, expanded, nil
}

func (request RootedPermissionRestriction) restrictionOnFile(fd int, stat *unix.Stat_t) (uint32, bool, error) {
	if err := validateOwnedStat(request.path, stat); err != nil {
		return 0, false, err
	}
	if err := validatePermissionTargetMetadata(stat); err != nil {
		return 0, false, err
	}
	desired, expanded, err := request.restriction(stat.Uid, stat.Gid, uint32(stat.Mode)&0o777)
	if err != nil {
		return 0, false, err
	}
	if err := validateInPlacePermissionAccess(fd, stat, expanded); err != nil {
		return 0, false, err
	}
	return desired, expanded, nil
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
