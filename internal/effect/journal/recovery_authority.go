package journal

import (
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	outputownership "github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/target"
)

func canonicalRecoveryAuthority(
	journal recoveryJournal,
	operationDir string,
	claimTransitions []ownershipmutation.ClaimTransition,
	provisionalIntents []outputownership.ProvisionalAcquireIntent,
	fingerprint string,
) (recovery.Authority, error) {
	entries := make([]recovery.Entry, 0, len(journal.Entries))
	for index, persisted := range journal.Entries {
		entry, err := canonicalRecoveryEntry(persisted)
		if err != nil {
			return recovery.Authority{}, fmt.Errorf("recovery entries[%d]: %w", index, err)
		}
		entries = append(entries, entry)
	}

	manifestProvenance, err := canonicalRecoveryManifestRootProvenance(journal.ManifestRootProvenance)
	if err != nil {
		return recovery.Authority{}, err
	}
	return recovery.NewAuthority(
		journal.OperationID,
		operationDir,
		entries,
		journal.StatefileBefore,
		journal.StatefileAfter,
		claimTransitions,
		provisionalIntents,
		manifestProvenance,
		fingerprint,
	)
}

func canonicalRecoveryEntry(persisted recoveryEntry) (recovery.Entry, error) {
	subject, err := persisted.Subject.canonical()
	if err != nil {
		return recovery.Entry{}, fmt.Errorf("subject: %w", err)
	}
	var agentTarget target.Target
	if persisted.Target != "" {
		agentTarget, err = target.ParseTarget(persisted.Target)
		if err != nil {
			return recovery.Entry{}, fmt.Errorf("target: %w", err)
		}
	}
	consumerTargets, err := parseRecoveryTargets(persisted.Targets)
	if err != nil {
		return recovery.Entry{}, fmt.Errorf("targets: %w", err)
	}
	scope, err := target.ParseScope(persisted.Scope)
	if err != nil {
		return recovery.Entry{}, fmt.Errorf("scope: %w", err)
	}
	aggregateContract, err := canonicalRecoveryAggregateContract(persisted)
	if err != nil {
		return recovery.Entry{}, err
	}
	return recovery.NewEntry(
		subject,
		agentTarget,
		consumerTargets,
		scope,
		persisted.Path,
		persisted.ContentPath,
		realization.PathProjectionContentKind(persisted.ContentKind),
		persisted.Before.canonical(),
		persisted.ExpectedAfter.canonical(),
		aggregateContract,
	)
}

func parseRecoveryTargets(values []string) ([]target.Target, error) {
	result := make([]target.Target, 0, len(values))
	for _, value := range values {
		parsed, err := target.ParseTarget(value)
		if err != nil {
			return nil, err
		}
		result = append(result, parsed)
	}
	return result, nil
}

func canonicalRecoveryManifestRootProvenance(
	persisted recoveryRootProvenance,
) (recovery.ManifestRootProvenance, error) {
	validated, err := persisted.canonical()
	if err != nil {
		return recovery.ManifestRootProvenance{}, fmt.Errorf("recovery manifest_root_provenance: %w", err)
	}
	canonical, err := recovery.NewManifestRootProvenance(
		validated.PhysicalRoot(),
		validated.ObjectFingerprint(),
		validated.MountFingerprint(),
	)
	if err != nil {
		return recovery.ManifestRootProvenance{}, err
	}
	return canonical, nil
}

func canonicalRecoveryPathEvidence(values []recoveryPathObservation) []recovery.PathEvidence {
	result := make([]recovery.PathEvidence, len(values))
	for index, value := range values {
		result[index] = recovery.PathEvidence{
			Path:          value.Path,
			ContentPath:   value.ContentPath,
			Exists:        value.Exists,
			PathExisted:   value.PathExisted,
			PathMode:      clonePermissionMode(value.PathMode),
			Kind:          value.Kind,
			ContentHash:   value.ContentHash,
			LinkTarget:    value.LinkTarget,
			BlockedReason: value.BlockedReason,
			BlockedDetail: value.BlockedDetail,
			Error:         value.Error,
		}
	}
	return result
}

func canonicalRecoveryBackupEvidence(values []recoveryBackupObservation) []recovery.BackupEvidence {
	result := make([]recovery.BackupEvidence, len(values))
	for index, value := range values {
		result[index] = recovery.BackupEvidence{
			BackupPath:  value.BackupPath,
			Exists:      value.Exists,
			Kind:        value.Kind,
			ContentHash: value.ContentHash,
			Error:       value.Error,
		}
	}
	return result
}
