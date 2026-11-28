package recover

import (
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
	input := operationplan.NativeReleaseRecoveryIdentityInput{
		RecoveryDir: paths.RecoveryDir, LocalManifest: local.Manifest, LocalDefaultLock: local.DefaultLock, LocalState: local.State,
		OperationID: plan.OperationID(), Observation: plan.ObservationFingerprint(), StateDirIdentity: state,
	}
	return input.Fingerprint(), nil
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
	localWrites := make(map[string]struct{})
	if plan.FileSetFence() == fileset.FileSetFencePublishedTransaction {
		for _, target := range plan.LocalTargets() {
			if target.Write {
				localWrites[target.Path] = struct{}{}
			}
		}
	}
	for _, path := range []string{paths.ManifestPath, paths.LockfilePath} {
		if err := builder.AddLogicalPair(path, mutation.AccessExclusive, mutation.AccessExclusive); err != nil {
			return recoveryAuthorityEvidence{}, err
		}
	}
	for _, path := range []string{local.Manifest, local.DefaultLock, local.State, marker, plan.OperationDir(), filepath.Join(plan.OperationDir(), journalrecord.FileName), paths.OwnershipRegistryPath} {
		if err := builder.AddLogicalPair(path, mutation.AccessExclusive, mutation.AccessExclusive); err != nil {
			return recoveryAuthorityEvidence{}, err
		}
		if _, writes := localWrites[path]; writes {
			if err := builder.AddDocumentWritePair(path, "metadata", "project"); err != nil {
				return recoveryAuthorityEvidence{}, err
			}
		} else if err := builder.AddPhysicalPair(path, mutation.AccessExclusive, "metadata", "project"); err != nil {
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
	documentWriteRequests := compiled.DocumentWriteRequests()
	documentWrites, err := mutation.NewPhysicalAuthoritySet(documentWriteRequests...)
	if err != nil {
		return recoveryAuthorityEvidence{}, err
	}
	domains, err := recoverygate.LowerDomainSteps(compiled.DomainSteps())
	if err != nil {
		return recoveryAuthorityEvidence{}, err
	}
	fingerprint, err := operationplan.RecoverAuthorityFingerprint(compiled)
	if err != nil {
		return recoveryAuthorityEvidence{}, err
	}
	return recoveryAuthorityEvidence{domains: domains, initialDocumentWrites: documentWrites, documentWriteRequests: documentWriteRequests, authorityFingerprint: fingerprint}, nil
}
