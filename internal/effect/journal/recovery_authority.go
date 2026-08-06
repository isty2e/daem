package journal

import (
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/residue"
	"github.com/isty2e/daem/internal/output"
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
	removalIntents, err := canonicalRecoveryRemovalIntents(journal.RemovalIntents)
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
		removalIntents,
	)
}

func canonicalRecoveryRemovalIntents(values []recoveryRemovalIntent) ([]recovery.RemovalIntent, error) {
	result := make([]recovery.RemovalIntent, 0, len(values))
	for index, persisted := range values {
		scope, err := target.ParseScope(persisted.Scope)
		if err != nil {
			return nil, fmt.Errorf("recovery removal intents[%d].scope: %w", index, err)
		}
		destination, err := output.Parse(persisted.Destination)
		if err != nil {
			return nil, fmt.Errorf("recovery removal intents[%d].destination: %w", index, err)
		}
		residueName, err := residue.NewLogicalRemovalResidueName(persisted.Namespace.ResidueName)
		if err != nil {
			return nil, fmt.Errorf("recovery removal intents[%d].namespace.residue_name: %w", index, err)
		}
		namespace, err := canonicalRemovalNamespace(persisted.Namespace, residueName)
		if err != nil {
			return nil, fmt.Errorf("recovery removal intents[%d].namespace: %w", index, err)
		}
		states, err := canonicalRecoveryRemovalStates(persisted.States, index)
		if err != nil {
			return nil, fmt.Errorf("recovery removal intents[%d]: %w", index, err)
		}
		demand, err := recovery.NewRemovalDemand(scope, destination, states)
		if err != nil {
			return nil, fmt.Errorf("recovery removal intents[%d]: %w", index, err)
		}
		intent, err := recovery.NewRemovalIntent(demand, namespace)
		if err != nil {
			return nil, fmt.Errorf("recovery removal intents[%d]: %w", index, err)
		}
		result = append(result, intent)
	}
	return result, nil
}

func canonicalRecoveryRemovalStates(
	persistedStates []recoveryRemovalState,
	intentIndex int,
) ([]recovery.RemovalState, error) {
	states := make([]recovery.RemovalState, 0, len(persistedStates))
	var err error
	for stateIndex, state := range persistedStates {
		if (state.Before == nil) == (state.ExpectedAfter == nil) {
			return nil, fmt.Errorf(
				"recovery removal intents[%d].states[%d] must contain exactly one state variant",
				intentIndex,
				stateIndex,
			)
		}
		var removalState recovery.RemovalState
		if state.Before != nil {
			removalState, err = recovery.NewBeforeRemovalState(state.Before.canonical())
		} else {
			removalState, err = recovery.NewExpectedRemovalState(state.ExpectedAfter.canonical())
		}
		if err != nil {
			return nil, fmt.Errorf(
				"recovery removal intents[%d].states[%d]: %w",
				intentIndex,
				stateIndex,
				err,
			)
		}
		states = append(states, removalState)
	}
	return states, nil
}

func canonicalRemovalNamespace(
	persisted recoveryRemovalNamespaceAuthority,
	residueName residue.LogicalRemovalResidueName,
) (recovery.RemovalNamespaceAuthority, error) {
	switch recovery.RemovalNamespaceVariant(persisted.Variant) {
	case recovery.RemovalNamespaceExistingParent:
		if persisted.ParentProvenance == nil || persisted.RetainedAncestorProvenance == nil || persisted.MissingSuffix == "" {
			return recovery.RemovalNamespaceAuthority{}, fmt.Errorf("existing parent authority has invalid variant fields")
		}
		parent, err := canonicalRecoveryManifestRootProvenance(*persisted.ParentProvenance)
		if err != nil {
			return recovery.RemovalNamespaceAuthority{}, err
		}
		retained, err := canonicalRecoveryManifestRootProvenance(*persisted.RetainedAncestorProvenance)
		if err != nil {
			return recovery.RemovalNamespaceAuthority{}, err
		}
		return recovery.NewExistingParentAuthority(parent, retained, persisted.MissingSuffix, residueName)
	case recovery.RemovalNamespaceInitiallyAbsentParent:
		if persisted.RetainedAncestorProvenance == nil || persisted.ParentProvenance != nil || persisted.MissingSuffix == "" {
			return recovery.RemovalNamespaceAuthority{}, fmt.Errorf("initially absent parent authority has invalid variant fields")
		}
		ancestor, err := canonicalRecoveryManifestRootProvenance(*persisted.RetainedAncestorProvenance)
		if err != nil {
			return recovery.RemovalNamespaceAuthority{}, err
		}
		return recovery.NewInitiallyAbsentParentAuthority(ancestor, persisted.MissingSuffix, residueName)
	default:
		return recovery.RemovalNamespaceAuthority{}, fmt.Errorf("unsupported namespace variant %q", persisted.Variant)
	}
}

func persistedRecoveryRemovalIntents(intents []recovery.RemovalIntent) ([]recoveryRemovalIntent, error) {
	if intents == nil {
		return []recoveryRemovalIntent{}, nil
	}
	result := make([]recoveryRemovalIntent, 0, len(intents))
	for index, intent := range intents {
		if err := intent.Validate(); err != nil {
			return nil, fmt.Errorf("recovery removal intents[%d]: %w", index, err)
		}
		namespace := intent.Namespace()
		persistedNamespace := recoveryRemovalNamespaceAuthority{
			Variant:       string(namespace.Variant()),
			MissingSuffix: namespace.MissingSuffix(),
			ResidueName:   namespace.ResidueName().String(),
		}
		if parent, present := namespace.ParentProvenance(); present {
			persistedNamespace.ParentProvenance = persistedRecoveryRootProvenanceFromCanonical(parent)
		}
		if ancestor, present := namespace.RetainedAncestorProvenance(); present {
			persistedNamespace.RetainedAncestorProvenance = persistedRecoveryRootProvenanceFromCanonical(ancestor)
		}
		states := intent.States()
		persistedStates := make([]recoveryRemovalState, 0, len(states))
		for _, state := range states {
			if before, present := state.Before(); present {
				persistedStates = append(persistedStates, recoveryRemovalState{Before: pointerToRecoveryBeforePathDTO(before)})
				continue
			}
			expected, present := state.Expected()
			if !present {
				return nil, fmt.Errorf("recovery removal intents[%d] contains an empty state", index)
			}
			persistedStates = append(persistedStates, recoveryRemovalState{ExpectedAfter: pointerToRecoveryExpectedPathDTO(expected)})
		}
		result = append(result, recoveryRemovalIntent{
			Scope:       string(intent.Scope()),
			Destination: intent.Destination().String(),
			Namespace:   persistedNamespace,
			States:      persistedStates,
		})
	}
	return result, nil
}

func persistedRecoveryRootProvenanceFromCanonical(
	provenance recovery.ManifestRootProvenance,
) *recoveryRootProvenance {
	return &recoveryRootProvenance{
		PhysicalRoot:      provenance.PhysicalRoot(),
		ObjectFingerprint: provenance.ObjectFingerprint(),
		MountFingerprint:  provenance.MountFingerprint(),
	}
}

func pointerToRecoveryBeforePathDTO(state recovery.BeforePathState) *recoveryBeforePathDTO {
	persisted := persistedBeforePathState(state)
	return &persisted
}

func pointerToRecoveryExpectedPathDTO(state recovery.ExpectedPathState) *recoveryExpectedPathDTO {
	persisted := persistedExpectedPathState(state)
	return &persisted
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
