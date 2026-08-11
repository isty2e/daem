package journal

import (
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
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

	manifestProvenance, err := canonicalRecoveryRootProvenance(journal.ManifestRootProvenance)
	if err != nil {
		return recovery.Authority{}, fmt.Errorf("recovery manifest_root_provenance: %w", err)
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
	if len(values) > recovery.MaximumRemovalIntents {
		return nil, fmt.Errorf(
			"recovery removal intent count %d exceeds operation maximum %d",
			len(values),
			recovery.MaximumRemovalIntents,
		)
	}
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
		names, err := mutationfs.NewLogicalRemovalNames(
			persisted.Namespace.ResidueName,
			persisted.Namespace.CleanupName,
		)
		if err != nil {
			return nil, fmt.Errorf("recovery removal intents[%d].namespace.names: %w", index, err)
		}
		namespace, err := canonicalRemovalNamespace(persisted.Namespace, names)
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
		canonicalStates := demand.States()
		for stateIndex := range states {
			if !states[stateIndex].Equal(canonicalStates[stateIndex]) {
				return nil, fmt.Errorf(
					"recovery removal intents[%d].states order is not canonical",
					index,
				)
			}
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
			if err := validateRemovalStateContentHash(
				state.Before.ContentHash,
				"before.content_hash",
			); err != nil {
				return nil, fmt.Errorf(
					"recovery removal intents[%d].states[%d]: %w",
					intentIndex,
					stateIndex,
					err,
				)
			}
			removalState, err = recovery.NewBeforeRemovalState(state.Before.canonical())
		} else {
			if err := validateRemovalStateContentHash(
				state.ExpectedAfter.ContentHash,
				"expected_after.content_hash",
			); err != nil {
				return nil, fmt.Errorf(
					"recovery removal intents[%d].states[%d]: %w",
					intentIndex,
					stateIndex,
					err,
				)
			}
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
	names mutationfs.LogicalRemovalNames,
) (recovery.RemovalNamespaceAuthority, error) {
	switch recovery.RemovalNamespaceVariant(persisted.Variant) {
	case recovery.RemovalNamespaceExistingParent:
		if persisted.ParentProvenance == nil || persisted.RetainedAncestorProvenance != nil || persisted.MissingSuffix != "" {
			return recovery.RemovalNamespaceAuthority{}, fmt.Errorf("existing parent authority has invalid variant fields")
		}
		parent, err := canonicalRecoveryRootProvenance(*persisted.ParentProvenance)
		if err != nil {
			return recovery.RemovalNamespaceAuthority{}, fmt.Errorf("existing parent provenance: %w", err)
		}
		return recovery.NewExistingParentAuthority(parent, names)
	case recovery.RemovalNamespaceInitiallyAbsentParent:
		if persisted.RetainedAncestorProvenance == nil || persisted.ParentProvenance != nil || persisted.MissingSuffix == "" {
			return recovery.RemovalNamespaceAuthority{}, fmt.Errorf("initially absent parent authority has invalid variant fields")
		}
		ancestor, err := canonicalRecoveryRootProvenance(*persisted.RetainedAncestorProvenance)
		if err != nil {
			return recovery.RemovalNamespaceAuthority{}, fmt.Errorf("retained ancestor provenance: %w", err)
		}
		return recovery.NewInitiallyAbsentParentAuthority(ancestor, persisted.MissingSuffix, names)
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
			Variant:     string(namespace.Variant()),
			ResidueName: namespace.Names().Residue(),
			CleanupName: namespace.Names().Cleanup(),
		}
		switch namespace.Variant() {
		case recovery.RemovalNamespaceExistingParent:
			parent, present := namespace.ParentProvenance()
			if !present {
				return nil, fmt.Errorf("recovery removal intents[%d] existing parent provenance is missing", index)
			}
			persistedNamespace.ParentProvenance = persistedRecoveryRootProvenanceFromCanonical(parent)
		case recovery.RemovalNamespaceInitiallyAbsentParent:
			ancestor, present := namespace.RetainedAncestorProvenance()
			if !present {
				return nil, fmt.Errorf("recovery removal intents[%d] retained ancestor provenance is missing", index)
			}
			persistedNamespace.RetainedAncestorProvenance = persistedRecoveryRootProvenanceFromCanonical(ancestor)
			persistedNamespace.MissingSuffix = namespace.MissingSuffix()
		default:
			return nil, fmt.Errorf("recovery removal intents[%d] has unsupported namespace variant %q", index, namespace.Variant())
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
	provenance recovery.RootProvenance,
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

func canonicalRecoveryRootProvenance(
	persisted recoveryRootProvenance,
) (recovery.RootProvenance, error) {
	validated, err := persisted.canonical()
	if err != nil {
		return recovery.RootProvenance{}, err
	}
	canonical, err := recovery.NewRootProvenance(
		validated.PhysicalRoot(),
		validated.ObjectFingerprint(),
		validated.MountFingerprint(),
	)
	if err != nil {
		return recovery.RootProvenance{}, err
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
			Work:        value.Work,
		}
	}
	return result
}
