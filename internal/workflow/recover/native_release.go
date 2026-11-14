package recover

import (
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/fileset"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/operationplan"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
)

func nativeReleaseRecoveryFingerprint(paths daempaths.Paths, plan journal.NativeReleaseRecoveryPlan, authority recoverygate.StateDirAuthority) (mutation.OperationFingerprint, error) {
	state, err := authority.IdentityFingerprint()
	if err != nil {
		return mutation.OperationFingerprint{}, err
	}
	local := plan.LocalPaths()
	return mutation.NewOperationFingerprint([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s", paths.RecoveryDir, local.Manifest, local.DefaultLock, local.State, plan.OperationID(), plan.ObservationFingerprint(), state))), nil
}

func buildNativeReleaseRecoveryAuthority(paths daempaths.Paths, plan journal.NativeReleaseRecoveryPlan, authority recoverygate.StateDirAuthority) (recoveryAuthorityEvidence, error) {
	builder, err := newRecoveryAuthorityBuilder(paths)
	if err != nil {
		return recoveryAuthorityEvidence{}, err
	}
	state, err := authority.IdentityFingerprint()
	if err != nil {
		return recoveryAuthorityEvidence{}, err
	}
	if err := builder.AddFingerprintOnly(operationplan.FactStateDirIdentity, paths.StateDir, mutation.AccessShared, mutation.PathEffectDirectoryEntry, state); err != nil {
		return recoveryAuthorityEvidence{}, err
	}
	marker, err := fileset.FileSetAuthorityPath(paths.StateDir)
	if err != nil {
		return recoveryAuthorityEvidence{}, err
	}
	local := plan.LocalPaths()
	for _, path := range []string{paths.ManifestPath, paths.LockfilePath} {
		if err := builder.AddLogicalPair(path, mutation.AccessExclusive, mutation.AccessExclusive); err != nil {
			return recoveryAuthorityEvidence{}, err
		}
	}
	for _, path := range []string{local.Manifest, local.DefaultLock, local.State, marker, plan.OperationDir(), filepath.Join(plan.OperationDir(), journalrecord.FileName), paths.OwnershipRegistryPath} {
		if err := builder.AddLogicalPair(path, mutation.AccessExclusive, mutation.AccessExclusive); err != nil {
			return recoveryAuthorityEvidence{}, err
		}
		if err := builder.AddPhysicalPair(path, mutation.AccessExclusive, "metadata", "project"); err != nil {
			return recoveryAuthorityEvidence{}, err
		}
	}
	if err := builder.AddLogicalPair(paths.RecoveryDir, mutation.AccessExclusive, mutation.AccessExclusive); err != nil {
		return recoveryAuthorityEvidence{}, err
	}
	if err := builder.AddLogicalPair(paths.StateDir, mutation.AccessShared, mutation.AccessShared); err != nil {
		return recoveryAuthorityEvidence{}, err
	}
	compiled := builder.Compile()
	domains, err := lowerRecoveryAuthorityDomainSteps(compiled.DomainSteps())
	if err != nil {
		return recoveryAuthorityEvidence{}, err
	}
	fingerprint, err := operationplan.RecoverAuthorityFingerprint(compiled)
	if err != nil {
		return recoveryAuthorityEvidence{}, err
	}
	return recoveryAuthorityEvidence{domains: domains, authorityFingerprint: fingerprint}, nil
}
