package execute

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

type removalRelationKey struct {
	scope       target.Scope
	destination output.Destination
}

// removalCleanupError keeps retirement failures typed and truthful without
// copying machine-local residue paths into CLI-facing error strings.
type removalCleanupError struct {
	destination  output.Destination
	readiness    recovery.RemovalCleanupReadiness
	reason       recovery.RemovalCleanupReason
	detail       string
	outcome      mutationfs.CommitOutcome
	outcomeKnown bool
	cause        error
}

func (failure *removalCleanupError) Error() string {
	if failure == nil {
		return "removal cleanup failed"
	}
	message := fmt.Sprintf(
		"removal cleanup for %q is %s (%s): %s",
		failure.destination,
		failure.readiness,
		failure.reason,
		failure.detail,
	)
	if failure.outcomeKnown {
		message += fmt.Sprintf(" (storage outcome: %s)", failure.outcome.State())
	}
	return message
}

func (failure *removalCleanupError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}

func (authority *mutationAuthority) bindRemovalIntents(plan recovery.Plan) error {
	if authority == nil {
		return fmt.Errorf("removal mutation authority is unavailable")
	}
	intents := plan.RemovalIntents()
	next := make(map[removalRelationKey]recovery.RemovalIntent, len(intents))
	for index, intent := range intents {
		if err := intent.Validate(); err != nil {
			return fmt.Errorf("removal intent[%d]: %w", index, err)
		}
		// Recovery selection may omit every semantic action for this relation,
		// but the complete retirement gate still has to bind and re-observe it.
		// Establish the physical destination binding from the intent itself;
		// rooted removal execution will still require the exact relation below.
		alreadyBound := false
		if intent.Scope() == target.ScopeGlobal {
			_, alreadyBound = authority.globalDestinationBindings[intent.Destination()]
		}
		if !alreadyBound {
			if err := authority.bindScopedDestination(intent.Scope(), intent.Destination()); err != nil {
				return fmt.Errorf("removal intent[%d] destination authority: %w", index, err)
			}
		}
		key := removalRelationKey{scope: intent.Scope(), destination: intent.Destination()}
		if _, duplicate := next[key]; duplicate {
			return fmt.Errorf("removal authority contains duplicate relation %q", intent.Destination())
		}
		next[key] = intent
	}
	if authority.removalAuthorityBound {
		if len(next) != len(authority.removalIntents) {
			return fmt.Errorf("reloaded removal authority changed intent cardinality")
		}
		for key, intent := range next {
			previous, present := authority.removalIntents[key]
			if !present || !previous.Equal(intent) {
				return fmt.Errorf("reloaded removal authority changed relation %q", intent.Destination())
			}
		}
		return nil
	}
	authority.removalIntents = next
	authority.removalAuthorityBound = true
	return nil
}

func (authority *mutationAuthority) removalIntentFor(
	scope target.Scope,
	destination output.Destination,
) (recovery.RemovalIntent, error) {
	if authority == nil || !authority.removalAuthorityBound {
		return recovery.RemovalIntent{}, fmt.Errorf("complete removal authority is not bound before rooted removal")
	}
	intent, present := authority.removalIntents[removalRelationKey{scope: scope, destination: destination}]
	if !present {
		return recovery.RemovalIntent{}, fmt.Errorf("journalized rooted removal %q has no exact removal intent", destination)
	}
	return intent, nil
}

func (authority *mutationAuthority) removeJournaledRootedEntry(
	ctx context.Context,
	destination mutationDestination,
	capability rootedpath.CommitCapability,
	expected mutationfs.EntryIdentity,
) (mutationfs.CommitOutcome, error) {
	if !destination.isRooted() {
		return uncommittedRemovalFailure(fmt.Errorf("mutation destination is invalid"))
	}
	intent, err := authority.removalIntentFor(destination.scope, destination.logical)
	if err != nil {
		if capability != nil {
			_ = capability.Close()
		}
		return uncommittedRemovalFailure(err)
	}
	if err := validateRemovalNamespace(ctx, destination, intent.Namespace()); err != nil {
		if capability != nil {
			_ = capability.Close()
		}
		return uncommittedRemovalFailure(err)
	}
	outcome, err := authority.filesystem.RemoveRootedEntryWithResidue(
		ctx,
		capability,
		expected,
		intent.Namespace().Names(),
	)
	if err != nil {
		return outcome, &rootedRemovalCommitError{outcome: outcome, cause: err}
	}
	return outcome, nil
}

// rootedRemovalCommitError preserves the storage conclusion across the
// execute boundary. The storage layer owns the outcome; execute only uses it
// to choose truthful rollback progress and diagnostics.
type rootedRemovalCommitError struct {
	outcome mutationfs.CommitOutcome
	cause   error
}

func (failure *rootedRemovalCommitError) Error() string {
	if failure == nil {
		return "rooted removal commit failed"
	}
	return fmt.Sprintf("rooted removal commit outcome %s", failure.outcome.State())
}

func uncommittedRemovalFailure(cause error) (mutationfs.CommitOutcome, error) {
	outcome, err := mutationfs.NewCommitOutcome(mutationfs.CommitOutcomeUncommitted, nil)
	if err != nil {
		return mutationfs.CommitOutcome{}, &rootedRemovalCommitError{cause: cause}
	}
	return outcome, &rootedRemovalCommitError{outcome: outcome, cause: cause}
}

func (failure *rootedRemovalCommitError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}

func (failure *rootedRemovalCommitError) Outcome() mutationfs.CommitOutcome {
	if failure == nil {
		return mutationfs.CommitOutcome{}
	}
	return failure.outcome
}

func validateRemovalNamespace(
	ctx context.Context,
	destination mutationDestination,
	namespace recovery.RemovalNamespaceAuthority,
) error {
	observation, err := observeRemovalNamespace(ctx, destination, namespace)
	if err != nil {
		return err
	}
	if observation.Status() != recovery.RemovalNamespaceMatched {
		return fmt.Errorf("removal namespace is %s: %s", observation.Status(), observation.Detail())
	}
	return nil
}

func observeRemovalNamespace(
	ctx context.Context,
	destination mutationDestination,
	namespace recovery.RemovalNamespaceAuthority,
) (recovery.RemovalNamespaceObservation, error) {
	if err := ctx.Err(); err != nil {
		return recovery.RemovalNamespaceObservation{}, err
	}
	if destination.hostPath == "" {
		return recovery.RemovalNamespaceObservation{}, fmt.Errorf("removal namespace destination path is required")
	}
	switch namespace.Variant() {
	case recovery.RemovalNamespaceExistingParent:
		parent, parentPresent := namespace.ParentProvenance()
		ancestor, ancestorPresent := namespace.RetainedAncestorProvenance()
		if !parentPresent || !ancestorPresent {
			return recovery.RemovalNamespaceObservation{}, fmt.Errorf("existing-parent removal namespace has no parent provenance")
		}
		parentPath := parent.PhysicalRoot()
		if filepath.Clean(filepath.Dir(filepath.Clean(destination.hostPath))) != filepath.Clean(parentPath) {
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceChanged,
				"destination parent no longer matches captured existing parent",
			)
		}
		info, err := os.Lstat(parentPath)
		switch {
		case err == nil:
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return newRemovalNamespaceObservation(
					recovery.RemovalNamespaceChanged,
					"captured parent is no longer a directory",
				)
			}
			if err := requireExactRootProvenance(ctx, parentPath, parent); err != nil {
				return newRemovalNamespaceObservation(
					recovery.RemovalNamespaceChanged,
					"captured parent authority no longer matches",
				)
			}
			return newRemovalNamespaceObservation(recovery.RemovalNamespaceMatched, "")
		case os.IsNotExist(err):
			if err := validateRetainedRemovalAncestor(ctx, parentPath, ancestor, namespace.MissingSuffix()); err != nil {
				return newRemovalNamespaceObservation(
					recovery.RemovalNamespaceChanged,
					"retained ancestor authority no longer matches",
				)
			}
			return newRemovalNamespaceObservation(recovery.RemovalNamespaceMatched, "")
		default:
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceUnavailable,
				"captured parent authority could not be observed",
			)
		}
	case recovery.RemovalNamespaceInitiallyAbsentParent:
		ancestor, present := namespace.RetainedAncestorProvenance()
		if !present {
			return recovery.RemovalNamespaceObservation{}, fmt.Errorf("initially-absent removal namespace has no retained ancestor provenance")
		}
		wantParent := filepath.Join(ancestor.PhysicalRoot(), filepath.FromSlash(namespace.MissingSuffix()))
		parent := filepath.Dir(filepath.Clean(destination.hostPath))
		if filepath.Clean(wantParent) != parent {
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceChanged,
				"destination parent no longer matches captured missing suffix",
			)
		}
		if err := validateRetainedRemovalAncestor(ctx, parent, ancestor, namespace.MissingSuffix()); err != nil {
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceChanged,
				"retained ancestor authority no longer matches",
			)
		}
		info, err := os.Lstat(parent)
		if os.IsNotExist(err) {
			return newRemovalNamespaceObservation(recovery.RemovalNamespaceMatched, "")
		}
		if err != nil {
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceUnavailable,
				"initially absent parent authority could not be observed",
			)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceChanged,
				"initially absent parent is not a directory",
			)
		}
		root, err := rootedpath.CaptureRootNoFollow(parent)
		if err != nil {
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceChanged,
				"initially absent parent cannot be captured without following links",
			)
		}
		defer root.Close()
		current, err := root.Authority()
		if err != nil {
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceUnavailable,
				"initially absent parent authority could not be read",
			)
		}
		persisted, err := rootedpath.NewAuthorityProvenance(
			ancestor.PhysicalRoot(), ancestor.ObjectFingerprint(), ancestor.MountFingerprint(),
		)
		if err != nil {
			return recovery.RemovalNamespaceObservation{}, err
		}
		if err := persisted.MatchDescendant(current); err != nil {
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceChanged,
				"initially absent parent authority no longer matches retained ancestor",
			)
		}
		return newRemovalNamespaceObservation(recovery.RemovalNamespaceMatched, "")
	default:
		return recovery.RemovalNamespaceObservation{}, fmt.Errorf("unsupported removal namespace variant %q", namespace.Variant())
	}
}

func newRemovalNamespaceObservation(
	status recovery.RemovalNamespaceObservationStatus,
	detail string,
) (recovery.RemovalNamespaceObservation, error) {
	return recovery.NewRemovalNamespaceObservation(status, detail)
}

type removalCleanupCandidate struct {
	intent      recovery.RemovalIntent
	destination mutationDestination
	residuePath string
	cleanupPath string
	obligation  recovery.RemovalCleanupObligation
}

type removalSlotObservation struct {
	entry    recovery.RemovalResidueEntryObservation
	identity mutationfs.EntryIdentity
}

// cleanupRemovalResidues performs a complete read-only retirement preflight
// before executing any removal-slot action. This prevents one blocked intent from
// being discovered after another intent has already been mutated.
func (authority *mutationAuthority) cleanupRemovalResidues(
	ctx context.Context,
	plan recovery.Plan,
) ([]recovery.RemovalCleanupObligation, error) {
	intents := plan.RemovalIntents()
	candidates := make([]removalCleanupCandidate, 0, len(intents))
	for index, intent := range intents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		destination, err := authority.resolveBoundDestination(intent.Scope(), intent.Destination())
		if err != nil {
			obligation, obligationErr := unavailableRemovalObligation(intent, "destination authority could not be resolved")
			if obligationErr != nil {
				return nil, obligationErr
			}
			return nil, newRemovalCleanupError(obligation, err)
		}
		namespace, err := observeRemovalNamespace(ctx, destination, intent.Namespace())
		if err != nil {
			return nil, fmt.Errorf("observe removal intent[%d] namespace contract: %w", index, err)
		}
		residuePath, cleanupPath, err := removalNamespacePaths(intent.Namespace())
		if err != nil {
			return nil, fmt.Errorf("derive removal intent[%d] namespace paths: %w", index, err)
		}
		residue, err := unavailableRemovalSlotObservation("residue was not observed")
		if err != nil {
			return nil, err
		}
		cleanup, err := unavailableRemovalSlotObservation("cleanup stage was not observed")
		if err != nil {
			return nil, err
		}
		if namespace.Status() == recovery.RemovalNamespaceMatched {
			residue, err = authority.observeRemovalSlot(ctx, destination, residuePath, "residue")
			if err != nil {
				return nil, err
			}
			cleanup, err = authority.observeRemovalSlot(ctx, destination, cleanupPath, "cleanup stage")
			if err != nil {
				return nil, err
			}
		}
		obligation, err := intent.AssessCleanup(
			namespace,
			recovery.NewRemovalResidueObservation(residue.entry, cleanup.entry),
		)
		if err != nil {
			return nil, fmt.Errorf("assess removal intent[%d] cleanup: %w", index, err)
		}
		candidates = append(candidates, removalCleanupCandidate{
			intent: intent, destination: destination,
			residuePath: residuePath, cleanupPath: cleanupPath,
			obligation: obligation,
		})
	}

	for _, candidate := range candidates {
		if candidate.obligation.Readiness() != recovery.RemovalCleanupReady {
			return nil, newRemovalCleanupError(candidate.obligation, nil)
		}
	}

	discharged := make([]recovery.RemovalCleanupObligation, 0, len(candidates))
	for index, candidate := range candidates {
		obligation, err := authority.executeRemovalCleanupCandidate(ctx, candidate)
		if err != nil {
			return nil, fmt.Errorf("reconcile removal intent[%d]: %w", index, err)
		}
		discharged = append(discharged, obligation)
	}
	if !plan.RetirementReady(discharged) {
		return nil, fmt.Errorf("complete removal cleanup authority was not discharged")
	}
	return discharged, nil
}

func unavailableRemovalObligation(
	intent recovery.RemovalIntent,
	detail string,
) (recovery.RemovalCleanupObligation, error) {
	namespace, err := recovery.NewRemovalNamespaceObservation(
		recovery.RemovalNamespaceUnavailable,
		detail,
	)
	if err != nil {
		return recovery.RemovalCleanupObligation{}, err
	}
	entries, err := unavailableRemovalSlots()
	if err != nil {
		return recovery.RemovalCleanupObligation{}, err
	}
	return intent.AssessCleanup(
		namespace,
		entries,
	)
}

func unavailableRemovalEntryObservation(detail string) (recovery.RemovalResidueEntryObservation, error) {
	return recovery.NewRemovalResidueEntryObservation(
		recovery.RemovalResidueEntryUnavailable,
		"", "", nil, "", detail,
	)
}

func unavailableRemovalSlotObservation(detail string) (removalSlotObservation, error) {
	observation, err := unavailableRemovalEntryObservation(detail)
	if err != nil {
		return removalSlotObservation{}, err
	}
	return removalSlotObservation{entry: observation}, nil
}

func unavailableRemovalSlots() (recovery.RemovalResidueObservation, error) {
	residue, err := unavailableRemovalEntryObservation("residue was not observed")
	if err != nil {
		return recovery.RemovalResidueObservation{}, err
	}
	cleanup, err := unavailableRemovalEntryObservation("cleanup stage was not observed")
	if err != nil {
		return recovery.RemovalResidueObservation{}, err
	}
	return recovery.NewRemovalResidueObservation(residue, cleanup), nil
}

func (authority *mutationAuthority) observeRemovalSlot(
	ctx context.Context,
	destination mutationDestination,
	path string,
	role string,
) (removalSlotObservation, error) {
	capability, err := authority.acquireRemovalSlot(destination, path)
	if err != nil {
		return unavailableRemovalSlotObservation(role + " authority could not be bound")
	}
	entry, identity, observeErr := journal.ObserveRootedRemovalEntry(ctx, authority.filesystem, capability)
	closeErr := capability.Close()
	if observeErr != nil || closeErr != nil {
		return unavailableRemovalSlotObservation(role + " could not be observed")
	}
	return removalSlotObservation{entry: entry, identity: identity}, nil
}

func newRemovalCleanupError(
	obligation recovery.RemovalCleanupObligation,
	cause error,
) error {
	return newRemovalCleanupErrorWithOutcome(obligation, cause, mutationfs.CommitOutcome{}, false)
}

func newRemovalCleanupErrorWithOutcome(
	obligation recovery.RemovalCleanupObligation,
	cause error,
	outcome mutationfs.CommitOutcome,
	known bool,
) error {
	readiness := obligation.Readiness()
	if readiness == recovery.RemovalCleanupReady || readiness == recovery.RemovalCleanupPending {
		readiness = recovery.RemovalCleanupRetry
	}
	detail := obligation.Detail()
	if detail == "" {
		detail = "cleanup obligation could not be discharged"
	}
	return &removalCleanupError{
		destination:  obligation.Destination(),
		readiness:    readiness,
		reason:       obligation.Reason(),
		detail:       detail,
		outcome:      outcome,
		outcomeKnown: known,
		cause:        cause,
	}
}

func (authority *mutationAuthority) executeRemovalCleanupCandidate(
	ctx context.Context,
	candidate removalCleanupCandidate,
) (recovery.RemovalCleanupObligation, error) {
	namespace, err := observeRemovalNamespace(ctx, candidate.destination, candidate.intent.Namespace())
	if err != nil {
		return recovery.RemovalCleanupObligation{}, err
	}
	if namespace.Status() != recovery.RemovalNamespaceMatched {
		entries, entriesErr := unavailableRemovalSlots()
		if entriesErr != nil {
			return recovery.RemovalCleanupObligation{}, entriesErr
		}
		obligation, obligationErr := candidate.intent.AssessCleanup(
			namespace,
			entries,
		)
		if obligationErr != nil {
			return recovery.RemovalCleanupObligation{}, obligationErr
		}
		return recovery.RemovalCleanupObligation{}, newRemovalCleanupError(obligation, nil)
	}
	residue, err := authority.observeRemovalSlot(ctx, candidate.destination, candidate.residuePath, "residue")
	if err != nil {
		return recovery.RemovalCleanupObligation{}, err
	}
	cleanup, err := authority.observeRemovalSlot(ctx, candidate.destination, candidate.cleanupPath, "cleanup stage")
	if err != nil {
		return recovery.RemovalCleanupObligation{}, err
	}
	obligation, err := candidate.intent.AssessCleanup(
		namespace,
		recovery.NewRemovalResidueObservation(residue.entry, cleanup.entry),
	)
	if err != nil {
		return recovery.RemovalCleanupObligation{}, err
	}
	switch obligation.Readiness() {
	case recovery.RemovalCleanupBlocked, recovery.RemovalCleanupRetry:
		return recovery.RemovalCleanupObligation{}, newRemovalCleanupError(obligation, nil)
	case recovery.RemovalCleanupReady:
	default:
		return recovery.RemovalCleanupObligation{}, fmt.Errorf("removal cleanup candidate is not actionable")
	}

	switch obligation.Action() {
	case recovery.RemovalCleanupActionConfirmAbsence:
		if err := authority.confirmRemovalSlotsAbsent(ctx, candidate, obligation); err != nil {
			return recovery.RemovalCleanupObligation{}, err
		}
	case recovery.RemovalCleanupActionPromoteResidue:
		if residue.identity == nil {
			return recovery.RemovalCleanupObligation{}, newRemovalCleanupError(obligation, nil)
		}
		capability, acquireErr := authority.acquireRemovalSlot(candidate.destination, candidate.residuePath)
		if acquireErr != nil {
			return recovery.RemovalCleanupObligation{}, newRemovalCleanupError(obligation, acquireErr)
		}
		outcome, movedIdentity, renameErr := authority.filesystem.PromoteRootedRemovalResidue(
			ctx,
			capability,
			residue.identity,
			candidate.intent.Namespace().Names(),
		)
		if renameErr != nil {
			return recovery.RemovalCleanupObligation{}, newRemovalCleanupErrorWithOutcome(obligation, renameErr, outcome, true)
		}
		if err := authority.cleanupRemovalProgress(ctx, candidate, obligation, movedIdentity); err != nil {
			return recovery.RemovalCleanupObligation{}, err
		}
	case recovery.RemovalCleanupActionCleanupProgress:
		if cleanup.identity == nil {
			return recovery.RemovalCleanupObligation{}, newRemovalCleanupError(obligation, nil)
		}
		capability, acquireErr := authority.acquireRemovalSlot(candidate.destination, candidate.cleanupPath)
		if acquireErr != nil {
			return recovery.RemovalCleanupObligation{}, newRemovalCleanupError(obligation, acquireErr)
		}
		outcome, cleanupErr := authority.filesystem.CleanupRootedRemovalStage(
			ctx,
			capability,
			cleanup.identity,
			candidate.intent.Namespace().Names(),
		)
		if cleanupErr != nil {
			return recovery.RemovalCleanupObligation{}, newRemovalCleanupErrorWithOutcome(obligation, cleanupErr, outcome, true)
		}
		if err := authority.confirmRemovalSlotsAbsent(ctx, candidate, obligation); err != nil {
			return recovery.RemovalCleanupObligation{}, err
		}
	default:
		return recovery.RemovalCleanupObligation{}, fmt.Errorf("removal cleanup action is unavailable")
	}
	return obligation.Discharge()
}

func (authority *mutationAuthority) cleanupRemovalProgress(
	ctx context.Context,
	candidate removalCleanupCandidate,
	obligation recovery.RemovalCleanupObligation,
	identity mutationfs.EntryIdentity,
) error {
	capability, err := authority.acquireRemovalSlot(candidate.destination, candidate.cleanupPath)
	if err != nil {
		return newRemovalCleanupError(obligation, err)
	}
	outcome, err := authority.filesystem.CleanupRootedRemovalStage(
		ctx,
		capability,
		identity,
		candidate.intent.Namespace().Names(),
	)
	if err != nil {
		return newRemovalCleanupErrorWithOutcome(obligation, err, outcome, true)
	}
	return authority.confirmRemovalSlotsAbsent(ctx, candidate, obligation)
}

func (authority *mutationAuthority) confirmRemovalSlotsAbsent(
	ctx context.Context,
	candidate removalCleanupCandidate,
	obligation recovery.RemovalCleanupObligation,
) error {
	for _, path := range []string{candidate.residuePath, candidate.cleanupPath} {
		capability, err := authority.acquireRemovalSlot(candidate.destination, path)
		if err != nil {
			return newRemovalCleanupError(obligation, err)
		}
		outcome, err := authority.filesystem.ConfirmRootedEntryAbsent(ctx, capability)
		if err != nil {
			return newRemovalCleanupErrorWithOutcome(obligation, err, outcome, true)
		}
	}
	return nil
}

func (authority *mutationAuthority) acquireRemovalSlot(
	destination mutationDestination,
	slotPath string,
) (rootedpath.CommitCapability, error) {
	if authority == nil || destination.root == nil {
		return nil, fmt.Errorf("removal slot root authority is unavailable")
	}
	rootAuthority, err := destination.root.Authority()
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(rootAuthority.PhysicalRoot(), filepath.Clean(slotPath))
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("removal slot path %q escaped destination root", slotPath)
	}
	rootRelative, err := rootedpath.NewRelativeDestination(filepath.ToSlash(relative))
	if err != nil {
		return nil, err
	}
	bound, err := rootAuthority.Bind(rootRelative)
	if err != nil {
		return nil, err
	}
	return destination.root.Acquire(bound)
}

func removalNamespacePaths(namespace recovery.RemovalNamespaceAuthority) (string, string, error) {
	var parent string
	switch namespace.Variant() {
	case recovery.RemovalNamespaceExistingParent:
		provenance, present := namespace.ParentProvenance()
		if !present {
			return "", "", fmt.Errorf("existing-parent namespace lacks parent provenance")
		}
		parent = provenance.PhysicalRoot()
	case recovery.RemovalNamespaceInitiallyAbsentParent:
		provenance, present := namespace.RetainedAncestorProvenance()
		if !present {
			return "", "", fmt.Errorf("initially-absent namespace lacks retained ancestor provenance")
		}
		parent = filepath.Join(provenance.PhysicalRoot(), filepath.FromSlash(namespace.MissingSuffix()))
	default:
		return "", "", fmt.Errorf("unsupported removal namespace variant %q", namespace.Variant())
	}
	return filepath.Join(parent, namespace.Names().Residue()),
		filepath.Join(parent, namespace.Names().Cleanup()), nil
}

func requireExactRootProvenance(
	ctx context.Context,
	path string,
	expected recovery.ManifestRootProvenance,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("removal namespace %q is not a directory", path)
	}
	root, err := rootedpath.CaptureRootNoFollow(path)
	if err != nil {
		return err
	}
	defer root.Close()
	current, err := root.Authority()
	if err != nil {
		return err
	}
	provenance, err := rootedpath.NewAuthorityProvenance(
		expected.PhysicalRoot(), expected.ObjectFingerprint(), expected.MountFingerprint(),
	)
	if err != nil {
		return err
	}
	return provenance.Match(current)
}

func validateRetainedRemovalAncestor(
	ctx context.Context,
	parentPath string,
	ancestor recovery.ManifestRootProvenance,
	suffix string,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	wantParent := filepath.Join(ancestor.PhysicalRoot(), filepath.FromSlash(suffix))
	if filepath.Clean(wantParent) != filepath.Clean(parentPath) {
		return fmt.Errorf("parent path does not match retained ancestor relation")
	}
	return requireExactRootProvenance(ctx, ancestor.PhysicalRoot(), ancestor)
}
