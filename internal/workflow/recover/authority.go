package recover

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/output"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/internal/target"
)

type recoveryAuthorityEvidence struct {
	domains              []mutation.Domain
	authorityFingerprint mutation.OperationFingerprint
}

type recoveryDestinationKey struct {
	scope       target.Scope
	destination output.Destination
}

func canonicalRecoveryDestination(scope target.Scope, value string) (output.Destination, error) {
	destination, err := output.Parse(value)
	if err != nil {
		return output.Destination{}, err
	}
	if err := destination.ValidateScope(scope); err != nil {
		return output.Destination{}, err
	}
	return destination, nil
}

type recoveryPhysicalOccupancy struct {
	destination recoveryDestinationKey
	aggregate   bool
}

type recoveryRemovalAuthorityPaths struct {
	destination string
	parent      string
	residue     string
	cleanup     string
}

func (occupancy recoveryPhysicalOccupancy) equal(other recoveryPhysicalOccupancy) bool {
	return occupancy.destination == other.destination && occupancy.aggregate == other.aggregate
}

func (occupancy recoveryPhysicalOccupancy) String() string {
	kind := "whole-path"
	if occupancy.aggregate {
		kind = "aggregate"
	}
	return fmt.Sprintf("%s:%s (%s)", occupancy.destination.scope, occupancy.destination.destination, kind)
}

func recoveryOperationFingerprint(
	paths daempaths.Paths,
	selection journal.RecoverablePlan,
	stateDir recoverygate.StateDirAuthority,
	activeStateDir bool,
) (mutation.OperationFingerprint, error) {
	switch selection.AuthorityKind() {
	case journal.RecoveryAuthorityActiveJournal:
		plan, ok := journal.ActiveRecoveryPlan(selection)
		if !ok {
			return mutation.OperationFingerprint{}, fmt.Errorf(
				"active recovery selection is unavailable",
			)
		}
		if !activeStateDir {
			return mutation.OperationFingerprint{}, fmt.Errorf("active recovery StateDir authority is required")
		}
		return activeRecoveryOperationFingerprint(paths, plan, stateDir)
	case journal.RecoveryAuthorityJournalCleanup:
		plan, ok := journal.JournalCleanupPlan(selection)
		if !ok {
			return mutation.OperationFingerprint{}, fmt.Errorf(
				"journal cleanup selection is unavailable",
			)
		}
		return cleanupRecoveryOperationFingerprint(paths, plan)
	default:
		return mutation.OperationFingerprint{}, fmt.Errorf(
			"recovery authority kind %q is unsupported",
			selection.AuthorityKind(),
		)
	}
}

func activeRecoveryOperationFingerprint(
	paths daempaths.Paths,
	plan recovery.Plan,
	stateDir recoverygate.StateDirAuthority,
) (mutation.OperationFingerprint, error) {
	journalAuthorityFingerprint, err := plan.JournalAuthorityFingerprint()
	if err != nil {
		return mutation.OperationFingerprint{}, err
	}
	stateDirFingerprint, err := stateDir.IdentityFingerprint()
	if err != nil {
		return mutation.OperationFingerprint{}, err
	}
	statefileBefore, err := statefile.Marshal(plan.StatefileBefore())
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf("fingerprint recovery statefile before: %w", err)
	}
	actions, err := json.Marshal(plan.Actions())
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf("fingerprint recovery plan: %w", err)
	}
	guardedActions, err := json.Marshal(plan.GuardedActions())
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf("fingerprint recovery plan: %w", err)
	}
	cleanupObligations, err := recoveryCleanupObligationFingerprints(plan.RemovalCleanupObligations())
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf("fingerprint recovery plan: %w", err)
	}
	claimTransitions := make(
		[]operationplan.ActiveRecoveryClaimTransition,
		0,
		len(plan.ClaimTransitions()),
	)
	for _, transition := range plan.ClaimTransitions() {
		claimTransitions = append(claimTransitions, operationplan.ActiveRecoveryClaimTransition{
			Kind:                    string(transition.Kind()),
			PathAuthority:           recoveryPathAuthorityFingerprintFor(transition.Address().PathAuthority()),
			ContentPath:             transition.Address().ContentPath(),
			OwnerStatefileAuthority: recoveryPathAuthorityFingerprintFor(transition.Owner().StatefileAuthority()),
			OwnerManifestPath:       transition.Owner().ManifestPath(),
			OperationID:             transitionOperationID(transition),
		})
	}
	return operationplan.ActiveRecoveryOperationFingerprint(operationplan.ActiveRecoveryIdentityInput{
		ManifestRoot:                 paths.ManifestRoot,
		StatefilePath:                paths.StatefilePath,
		RecoveryDir:                  paths.RecoveryDir,
		OperationID:                  plan.OperationID(),
		OperationDir:                 plan.OperationDir(),
		Classification:               string(plan.Classification()),
		JournalAuthorityFingerprint:  journalAuthorityFingerprint,
		StateDirAuthorityFingerprint: stateDirFingerprint,
		Actions:                      json.RawMessage(actions),
		GuardedActions:               json.RawMessage(guardedActions),
		RemovalCleanupObligations:    cleanupObligations,
		StatefileBefore:              json.RawMessage(statefileBefore),
		ClaimTransitions:             claimTransitions,
	})
}

func recoveryCleanupObligationFingerprints(
	obligations []recovery.RemovalCleanupObligation,
) ([]operationplan.ActiveRecoveryCleanupObligation, error) {
	result := make([]operationplan.ActiveRecoveryCleanupObligation, 0, len(obligations))
	for _, obligation := range obligations {
		destination, err := json.Marshal(obligation.Destination())
		if err != nil {
			return nil, err
		}
		result = append(result, operationplan.ActiveRecoveryCleanupObligation{
			Scope:       string(obligation.Scope()),
			Destination: json.RawMessage(destination),
			Action:      string(obligation.Action()),
			Readiness:   string(obligation.Readiness()),
			Reason:      string(obligation.Reason()),
			Detail:      obligation.Detail(),
		})
	}
	return result, nil
}

func recoveryPathAuthorityFingerprintFor(
	authority pathauthority.Exact,
) operationplan.RecoveryPathAuthorityFingerprint {
	return operationplan.RecoveryPathAuthorityFingerprint{
		Key:              authority.Key(),
		SemanticsWitness: authority.Witness(),
	}
}

func cleanupRecoveryOperationFingerprint(
	paths daempaths.Paths,
	plan retirement.CleanupPlan,
) (mutation.OperationFingerprint, error) {
	authority := plan.Authority()
	return operationplan.CleanupRecoveryOperationFingerprint(
		operationplan.CleanupRecoveryIdentityInput{
			RecoveryDir:                 paths.RecoveryDir,
			OperationID:                 authority.OperationID(),
			Classification:              string(plan.Classification()),
			Action:                      string(plan.Action()),
			JournalAuthorityFingerprint: authority.JournalAuthorityFingerprint(),
			Phase:                       string(authority.Phase()),
			ResiduePresent:              authority.ResiduePresent(),
		},
	)
}

func transitionOperationID(transition ownershipmutation.ClaimTransition) string {
	if claim, present := transition.Prepared().Get(); present {
		return claim.OperationID()
	}
	return ""
}

func buildRecoveryAuthorityEvidence(
	paths daempaths.Paths,
	selection journal.RecoverablePlan,
	stateDir recoverygate.StateDirAuthority,
	activeStateDir bool,
) (recoveryAuthorityEvidence, error) {
	switch selection.AuthorityKind() {
	case journal.RecoveryAuthorityActiveJournal:
		plan, ok := journal.ActiveRecoveryPlan(selection)
		if !ok {
			return recoveryAuthorityEvidence{}, fmt.Errorf(
				"active recovery selection is unavailable",
			)
		}
		if !activeStateDir {
			return recoveryAuthorityEvidence{}, fmt.Errorf("active recovery StateDir authority is required")
		}
		return buildActiveRecoveryAuthorityEvidence(paths, plan, stateDir)
	case journal.RecoveryAuthorityJournalCleanup:
		plan, ok := journal.JournalCleanupPlan(selection)
		if !ok {
			return recoveryAuthorityEvidence{}, fmt.Errorf(
				"journal cleanup selection is unavailable",
			)
		}
		return buildCleanupRecoveryAuthorityEvidence(paths, plan)
	default:
		return recoveryAuthorityEvidence{}, fmt.Errorf(
			"recovery authority kind %q is unsupported",
			selection.AuthorityKind(),
		)
	}
}

func buildActiveRecoveryAuthorityEvidence(
	paths daempaths.Paths,
	plan recovery.Plan,
	stateDir recoverygate.StateDirAuthority,
) (recoveryAuthorityEvidence, error) {
	resolvedDestinations, removalPaths, err := resolveRecoveryAuthorityPaths(
		paths,
		plan.GuardedActions(),
		plan.RemovalIntents(),
	)
	if err != nil {
		return recoveryAuthorityEvidence{}, err
	}
	builder := operationplan.NewBuilder(operationplan.RevisionsOff, nil, 0)
	for _, effect := range []mutation.PathEffect{
		mutation.PathEffectDirectoryEntry,
		mutation.PathEffectReferent,
	} {
		if err := builder.AddLogical(paths.RecoveryDir, mutation.AccessExclusive, effect); err != nil {
			return recoveryAuthorityEvidence{}, err
		}
	}
	if err := builder.AddFingerprintOnly(
		operationplan.FactRecoveryRootIdentity,
		paths.RecoveryDir,
		mutation.AccessExclusive,
		mutation.PathEffectDirectoryEntry,
		"",
	); err != nil {
		return recoveryAuthorityEvidence{}, err
	}
	stateDirFingerprint, err := stateDir.IdentityFingerprint()
	if err != nil {
		return recoveryAuthorityEvidence{}, err
	}
	if err := builder.AddFingerprintOnly(
		operationplan.FactStateDirIdentity,
		paths.StateDir,
		mutation.AccessShared,
		mutation.PathEffectDirectoryEntry,
		stateDirFingerprint,
	); err != nil {
		return recoveryAuthorityEvidence{}, err
	}
	for _, effect := range []mutation.PathEffect{
		mutation.PathEffectDirectoryEntry,
		mutation.PathEffectReferent,
	} {
		if err := builder.AddLogical(paths.StateDir, mutation.AccessShared, effect); err != nil {
			return recoveryAuthorityEvidence{}, err
		}
	}
	for _, effect := range []mutation.PathEffect{
		mutation.PathEffectDirectoryEntry,
		mutation.PathEffectReferent,
	} {
		if err := builder.AddLogical(paths.StatefilePath, mutation.AccessExclusive, effect); err != nil {
			return recoveryAuthorityEvidence{}, err
		}
	}
	if len(plan.ClaimTransitions()) != 0 {
		for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
			if err := builder.AddLogical(paths.OwnershipRegistryPath, mutation.AccessExclusive, effect); err != nil {
				return recoveryAuthorityEvidence{}, err
			}
		}
	}
	for _, action := range plan.GuardedActions() {
		if action.Destination == "" || action.Scope == "" {
			return recoveryAuthorityEvidence{}, fmt.Errorf("recovery guarded action has incomplete destination authority")
		}
		targets := append([]target.Target(nil), action.ConsumerTargets...)
		if action.Target != "" {
			targets = []target.Target{action.Target}
		}
		if len(targets) == 0 {
			return recoveryAuthorityEvidence{}, fmt.Errorf("recovery guarded action has no target authority")
		}
		destination, err := canonicalRecoveryDestination(action.Scope, action.Destination)
		if err != nil {
			return recoveryAuthorityEvidence{}, fmt.Errorf("recovery guarded action destination: %w", err)
		}
		resolved := resolvedDestinations[recoveryDestinationKey{scope: action.Scope, destination: destination}]
		for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
			for _, selected := range targets {
				if err := builder.AddPhysical(
					resolved,
					mutation.AccessExclusive,
					effect,
					string(selected),
					string(action.Scope),
				); err != nil {
					return recoveryAuthorityEvidence{}, err
				}
			}
		}
	}
	for _, intent := range plan.RemovalIntents() {
		key := recoveryDestinationKey{
			scope: intent.Scope(), destination: intent.Destination(),
		}
		resolved, present := removalPaths[key]
		if !present {
			return recoveryAuthorityEvidence{}, fmt.Errorf(
				"recovery removal intent %q has no resolved authority paths",
				intent.Destination(),
			)
		}
		for _, path := range []string{
			resolved.destination,
			resolved.residue,
			resolved.cleanup,
		} {
			for _, effect := range []mutation.PathEffect{
				mutation.PathEffectDirectoryEntry,
				mutation.PathEffectReferent,
			} {
				if err := builder.AddRemovalContinuation(path, effect); err != nil {
					return recoveryAuthorityEvidence{}, err
				}
			}
		}
		// The exact residue and cleanup child domains already conflict with any
		// daem operation that leases their parent as an ancestor. The parent fact
		// remains in the operation fingerprint, while typed cleanup authority owns
		// its effect-time identity checks.
		if err := builder.AddFingerprintOnly(
			operationplan.FactRemovalParentIdentity,
			resolved.parent,
			mutation.AccessExclusive,
			mutation.PathEffectDirectoryEntry,
			"",
		); err != nil {
			return recoveryAuthorityEvidence{}, err
		}
	}
	compiled := builder.Compile()
	fingerprint, err := operationplan.RecoverAuthorityFingerprint(compiled)
	if err != nil {
		return recoveryAuthorityEvidence{}, err
	}
	return recoveryAuthorityEvidence{
		domains:              compiled.Domains(),
		authorityFingerprint: fingerprint,
	}, nil
}

func buildCleanupRecoveryAuthorityEvidence(
	paths daempaths.Paths,
	plan retirement.CleanupPlan,
) (recoveryAuthorityEvidence, error) {
	authority := plan.Authority()
	recoveryPaths := []string{
		paths.RecoveryDir,
		filepath.Join(paths.RecoveryDir, authority.ControlName()),
		filepath.Join(
			paths.RecoveryDir,
			authority.ControlName(),
			retirement.RecordFileName,
		),
		filepath.Join(paths.RecoveryDir, authority.ResidueName()),
		filepath.Join(paths.RecoveryDir, authority.GCName()),
	}
	builder := operationplan.NewBuilder(operationplan.RevisionsOff, nil, 0)
	for _, path := range recoveryPaths {
		for _, effect := range []mutation.PathEffect{
			mutation.PathEffectDirectoryEntry,
			mutation.PathEffectReferent,
		} {
			if err := builder.AddLogical(path, mutation.AccessExclusive, effect); err != nil {
				return recoveryAuthorityEvidence{}, err
			}
		}
	}
	compiled := builder.Compile()
	fingerprint, err := operationplan.RecoverAuthorityFingerprint(compiled)
	if err != nil {
		return recoveryAuthorityEvidence{}, err
	}
	return recoveryAuthorityEvidence{
		domains:              compiled.Domains(),
		authorityFingerprint: fingerprint,
	}, nil
}

func resolveRecoveryAuthorityPaths(
	paths daempaths.Paths,
	actions []recovery.Action,
	removalIntents []recovery.RemovalIntent,
) (
	map[recoveryDestinationKey]string,
	map[recoveryDestinationKey]recoveryRemovalAuthorityPaths,
	error,
) {
	resolver := destinationResolver(paths)
	resolved := make(map[recoveryDestinationKey]string)
	occupancies := make(map[string]recoveryPhysicalOccupancy)
	for _, action := range actions {
		if action.Destination == "" || action.Scope == "" {
			return nil, nil, fmt.Errorf("recovery guarded action has incomplete destination authority")
		}
		destination, err := canonicalRecoveryDestination(action.Scope, action.Destination)
		if err != nil {
			return nil, nil, fmt.Errorf("recovery guarded action destination: %w", err)
		}
		key := recoveryDestinationKey{scope: action.Scope, destination: destination}
		path, present := resolved[key]
		if !present {
			var err error
			path, err = resolver.Resolve(key.destination)
			if err != nil {
				return nil, nil, fmt.Errorf("resolve recovery destination %q: %w", action.Destination, err)
			}
			resolved[key] = path
		}
		physicalKey, err := mutation.CanonicalDirectoryEntryKey(path)
		if err != nil {
			return nil, nil, err
		}
		occupancy := recoveryPhysicalOccupancy{
			destination: key,
			aggregate:   action.ContentPath != "",
		}
		if existing, occupied := occupancies[physicalKey]; occupied && !existing.equal(occupancy) {
			return nil, nil, fmt.Errorf(
				"physical destination %q aliases incompatible logical occupancies %s and %s",
				path,
				existing,
				occupancy,
			)
		}
		occupancies[physicalKey] = occupancy
	}

	removals := make(map[recoveryDestinationKey]recoveryRemovalAuthorityPaths, len(removalIntents))
	for index, intent := range removalIntents {
		if err := intent.Validate(); err != nil {
			return nil, nil, fmt.Errorf("recovery removal intent[%d]: %w", index, err)
		}
		key := recoveryDestinationKey{
			scope: intent.Scope(), destination: intent.Destination(),
		}
		path, present := resolved[key]
		if !present {
			var err error
			path, err = resolver.Resolve(key.destination)
			if err != nil {
				return nil, nil, fmt.Errorf(
					"resolve recovery removal destination %q: %w",
					intent.Destination(),
					err,
				)
			}
			resolved[key] = path
		}
		physicalKey, err := mutation.CanonicalDirectoryEntryKey(path)
		if err != nil {
			return nil, nil, err
		}
		if existing, occupied := occupancies[physicalKey]; occupied &&
			existing.destination != key {
			return nil, nil, fmt.Errorf(
				"physical destination %q aliases incompatible logical occupancies %s and %s",
				path,
				existing,
				recoveryPhysicalOccupancy{destination: key},
			)
		}
		if _, occupied := occupancies[physicalKey]; !occupied {
			occupancies[physicalKey] = recoveryPhysicalOccupancy{destination: key}
		}
		residuePath, cleanupPath, err := journal.RemovalNamespacePaths(intent.Namespace())
		if err != nil {
			return nil, nil, fmt.Errorf(
				"resolve recovery removal intent[%d] slots: %w",
				index,
				err,
			)
		}
		if filepath.Dir(residuePath) != filepath.Dir(cleanupPath) {
			return nil, nil, fmt.Errorf(
				"recovery removal intent[%d] slots do not share one parent",
				index,
			)
		}
		removals[key] = recoveryRemovalAuthorityPaths{
			destination: path,
			parent:      filepath.Dir(residuePath),
			residue:     residuePath,
			cleanup:     cleanupPath,
		}
	}
	return resolved, removals, nil
}
