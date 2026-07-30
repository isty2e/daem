package recover

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
)

type recoveryAuthorityEvidence struct {
	domains              []mutation.Domain
	revisions            []mutation.RevisionRequest
	authorityFingerprint mutation.OperationFingerprint
}

type recoveryAuthorityFact struct {
	Kind   string
	Path   string
	Access mutation.AccessMode
	Effect mutation.PathEffect
	Target string
	Scope  string
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

type recoveryFingerprintFacts struct {
	ManifestRoot                string
	StatefilePath               string
	RecoveryDir                 string
	OperationID                 string
	OperationDir                string
	Classification              recovery.Classification
	JournalAuthorityFingerprint string
	Actions                     []recovery.Action
	GuardedActions              []recovery.Action
	StatefileBefore             json.RawMessage
	ClaimTransitions            []journalClaimTransitionFingerprint
}

type cleanupFingerprintFacts struct {
	RecoveryDir                 string
	OperationID                 string
	Classification              retirement.CleanupClassification
	Action                      retirement.CleanupActionKind
	JournalAuthorityFingerprint string
	Phase                       retirement.Phase
	ResiduePresent              bool
}

type journalClaimTransitionFingerprint struct {
	Kind              string
	Path              string
	ContentPath       string
	OwnerStatefileKey string
	OwnerManifestPath string
	OperationID       string
}

func recoveryOperationFingerprint(
	paths daempaths.Paths,
	selection journal.RecoverablePlan,
) (mutation.OperationFingerprint, error) {
	switch selection.AuthorityKind() {
	case journal.RecoveryAuthorityActiveJournal:
		plan, ok := journal.ActiveRecoveryPlan(selection)
		if !ok {
			return mutation.OperationFingerprint{}, fmt.Errorf(
				"active recovery selection is unavailable",
			)
		}
		return activeRecoveryOperationFingerprint(paths, plan)
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
) (mutation.OperationFingerprint, error) {
	journalAuthorityFingerprint, err := plan.JournalAuthorityFingerprint()
	if err != nil {
		return mutation.OperationFingerprint{}, err
	}
	statefileBefore, err := statefile.Marshal(plan.StatefileBefore())
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf("fingerprint recovery statefile before: %w", err)
	}
	claimTransitions := make([]journalClaimTransitionFingerprint, 0, len(plan.ClaimTransitions()))
	for _, transition := range plan.ClaimTransitions() {
		claimTransitions = append(claimTransitions, journalClaimTransitionFingerprint{
			Kind:              string(transition.Kind()),
			Path:              transition.Address().Path(),
			ContentPath:       transition.Address().ContentPath(),
			OwnerStatefileKey: transition.Owner().StatefileKey(),
			OwnerManifestPath: transition.Owner().ManifestPath(),
			OperationID:       transitionOperationID(transition),
		})
	}
	canonical, err := json.Marshal(recoveryFingerprintFacts{
		ManifestRoot:                paths.ManifestRoot,
		StatefilePath:               paths.StatefilePath,
		RecoveryDir:                 paths.RecoveryDir,
		OperationID:                 plan.OperationID(),
		OperationDir:                plan.OperationDir(),
		Classification:              plan.Classification(),
		JournalAuthorityFingerprint: journalAuthorityFingerprint,
		Actions:                     plan.Actions(),
		GuardedActions:              plan.GuardedActions(),
		StatefileBefore:             json.RawMessage(statefileBefore),
		ClaimTransitions:            claimTransitions,
	})
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf("fingerprint recovery plan: %w", err)
	}
	return mutation.NewOperationFingerprint(canonical), nil
}

func cleanupRecoveryOperationFingerprint(
	paths daempaths.Paths,
	plan retirement.CleanupPlan,
) (mutation.OperationFingerprint, error) {
	authority := plan.Authority()
	canonical, err := json.Marshal(cleanupFingerprintFacts{
		RecoveryDir:                 paths.RecoveryDir,
		OperationID:                 authority.OperationID(),
		Classification:              plan.Classification(),
		Action:                      plan.Action(),
		JournalAuthorityFingerprint: authority.JournalAuthorityFingerprint(),
		Phase:                       authority.Phase(),
		ResiduePresent:              authority.ResiduePresent(),
	})
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf(
			"fingerprint journal cleanup plan: %w",
			err,
		)
	}
	return mutation.NewOperationFingerprint(canonical), nil
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
) (recoveryAuthorityEvidence, error) {
	switch selection.AuthorityKind() {
	case journal.RecoveryAuthorityActiveJournal:
		plan, ok := journal.ActiveRecoveryPlan(selection)
		if !ok {
			return recoveryAuthorityEvidence{}, fmt.Errorf(
				"active recovery selection is unavailable",
			)
		}
		return buildActiveRecoveryAuthorityEvidence(paths, plan)
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
) (recoveryAuthorityEvidence, error) {
	facts := make([]recoveryAuthorityFact, 0)
	domains := make([]mutation.Domain, 0)
	revisions := make(map[string]mutation.RevisionRequest)
	resolvedDestinations, err := resolveRecoveryGuardedDestinations(paths, plan.GuardedActions())
	if err != nil {
		return recoveryAuthorityEvidence{}, err
	}
	addLogical := func(path string, access mutation.AccessMode, effect mutation.PathEffect) error {
		domain, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{Path: path, Access: access, Effect: effect})
		if err != nil {
			return err
		}
		facts = append(facts, recoveryAuthorityFact{Kind: "logical", Path: path, Access: access, Effect: effect})
		domains = append(domains, domain)
		revisions[recoveryRevisionKey(path, effect)] = mutation.RevisionRequest{Path: path, Effect: effect}
		return nil
	}
	addPhysical := func(path string, target string, scope string, effect mutation.PathEffect) error {
		domain, err := mutation.NewPhysicalPathDomain(mutation.PhysicalPathRequest{
			Path: path, Access: mutation.AccessExclusive, Effect: effect, Target: target, Scope: scope,
		})
		if err != nil {
			return err
		}
		facts = append(facts, recoveryAuthorityFact{
			Kind: "physical", Path: path, Access: mutation.AccessExclusive, Effect: effect, Target: target, Scope: scope,
		})
		domains = append(domains, domain)
		revisions[recoveryRevisionKey(path, effect)] = mutation.RevisionRequest{Path: path, Effect: effect}
		return nil
	}

	for _, path := range []string{paths.RecoveryDir, paths.StatefilePath} {
		if err := addLogical(path, mutation.AccessExclusive, mutation.PathEffectDirectoryEntry); err != nil {
			return recoveryAuthorityEvidence{}, err
		}
		if err := addLogical(path, mutation.AccessExclusive, mutation.PathEffectReferent); err != nil {
			return recoveryAuthorityEvidence{}, err
		}
	}
	if len(plan.ClaimTransitions()) != 0 {
		for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
			if err := addLogical(paths.OwnershipRegistryPath, mutation.AccessExclusive, effect); err != nil {
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
				if err := addPhysical(resolved, string(selected), string(action.Scope), effect); err != nil {
					return recoveryAuthorityEvidence{}, err
				}
			}
		}
	}
	if plan.Classification() == recovery.ClassificationNeedsRollback {
		for _, action := range plan.Actions() {
			if action.Kind != recovery.ActionKindRestoreWrite || action.BackupPath == "" {
				continue
			}
			backupPath := filepath.Join(plan.OperationDir(), action.BackupPath)
			for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
				if err := addLogical(backupPath, mutation.AccessShared, effect); err != nil {
					return recoveryAuthorityEvidence{}, err
				}
			}
		}
	}

	sort.Slice(facts, func(left int, right int) bool {
		return recoveryAuthorityFactKey(facts[left]) < recoveryAuthorityFactKey(facts[right])
	})
	canonical, err := json.Marshal(facts)
	if err != nil {
		return recoveryAuthorityEvidence{}, fmt.Errorf("fingerprint recovery authority: %w", err)
	}
	return recoveryAuthorityEvidence{
		domains:              domains,
		revisions:            recoverySortedRevisionRequests(revisions),
		authorityFingerprint: mutation.NewOperationFingerprint(canonical),
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
	facts := make([]recoveryAuthorityFact, 0, len(recoveryPaths)*2)
	domains := make([]mutation.Domain, 0, len(recoveryPaths)*2)
	revisions := make(map[string]mutation.RevisionRequest, len(recoveryPaths)*2)
	for _, path := range recoveryPaths {
		for _, effect := range []mutation.PathEffect{
			mutation.PathEffectDirectoryEntry,
			mutation.PathEffectReferent,
		} {
			domain, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{
				Path:   path,
				Access: mutation.AccessExclusive,
				Effect: effect,
			})
			if err != nil {
				return recoveryAuthorityEvidence{}, err
			}
			facts = append(facts, recoveryAuthorityFact{
				Kind:   "logical",
				Path:   path,
				Access: mutation.AccessExclusive,
				Effect: effect,
			})
			domains = append(domains, domain)
			revisions[recoveryRevisionKey(path, effect)] = mutation.RevisionRequest{
				Path:   path,
				Effect: effect,
			}
		}
	}
	sort.Slice(facts, func(left int, right int) bool {
		return recoveryAuthorityFactKey(facts[left]) <
			recoveryAuthorityFactKey(facts[right])
	})
	canonical, err := json.Marshal(facts)
	if err != nil {
		return recoveryAuthorityEvidence{}, fmt.Errorf(
			"fingerprint journal cleanup authority: %w",
			err,
		)
	}
	return recoveryAuthorityEvidence{
		domains:              domains,
		revisions:            recoverySortedRevisionRequests(revisions),
		authorityFingerprint: mutation.NewOperationFingerprint(canonical),
	}, nil
}

func resolveRecoveryGuardedDestinations(
	paths daempaths.Paths,
	actions []recovery.Action,
) (map[recoveryDestinationKey]string, error) {
	resolver := destinationResolver(paths)
	resolved := make(map[recoveryDestinationKey]string)
	occupancies := make(map[string]recoveryPhysicalOccupancy)
	for _, action := range actions {
		if action.Destination == "" || action.Scope == "" {
			return nil, fmt.Errorf("recovery guarded action has incomplete destination authority")
		}
		destination, err := canonicalRecoveryDestination(action.Scope, action.Destination)
		if err != nil {
			return nil, fmt.Errorf("recovery guarded action destination: %w", err)
		}
		key := recoveryDestinationKey{scope: action.Scope, destination: destination}
		path, present := resolved[key]
		if !present {
			var err error
			path, err = resolver.Resolve(key.destination)
			if err != nil {
				return nil, fmt.Errorf("resolve recovery destination %q: %w", action.Destination, err)
			}
			resolved[key] = path
		}
		physicalKey, err := mutation.CanonicalDirectoryEntryKey(path)
		if err != nil {
			return nil, err
		}
		occupancy := recoveryPhysicalOccupancy{
			destination: key,
			aggregate:   action.ContentPath != "",
		}
		if existing, occupied := occupancies[physicalKey]; occupied && !existing.equal(occupancy) {
			return nil, fmt.Errorf(
				"physical destination %q aliases incompatible logical occupancies %s and %s",
				path,
				existing,
				occupancy,
			)
		}
		occupancies[physicalKey] = occupancy
	}
	return resolved, nil
}

func recoveryRevisionKey(path string, effect mutation.PathEffect) string {
	return strconv.Itoa(int(effect)) + ":" + path
}

func recoverySortedRevisionRequests(requests map[string]mutation.RevisionRequest) []mutation.RevisionRequest {
	keys := make([]string, 0, len(requests))
	for key := range requests {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]mutation.RevisionRequest, 0, len(keys))
	for _, key := range keys {
		result = append(result, requests[key])
	}
	return result
}

func recoveryAuthorityFactKey(fact recoveryAuthorityFact) string {
	return fact.Kind + "\x00" + fact.Path + "\x00" + strconv.Itoa(int(fact.Access)) + "\x00" + strconv.Itoa(int(fact.Effect)) + "\x00" + fact.Target + "\x00" + fact.Scope
}
