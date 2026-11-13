package fileset

import (
	"context"
	"crypto/rand"
	"fmt"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

// InspectFileSetRecovery5 previews only the exact independently qualified intent
// and raw revision supplied by the caller. Reading evidence grants no authority;
// callers requalify that selection under complete leases before recovery.
func InspectFileSetRecovery5(ctx context.Context, evidenceRoot string, expected PublicationIntent, revision string, budget PhysicalWorkBudget) (RecoveryDisposition, error) {
	plan, err := loadQualifiedPublication(ctx, evidenceRoot, expected, revision, budget)
	if err != nil {
		return "", err
	}
	defer plan.rootWitness.Close()
	if err := plan.admitWork(budget, publicationRecoveryWork); err != nil {
		return "", err
	}
	return plan.inspect(ctx, revision)
}

// RecoverFileSet5 finalizes all-After evidence or restores a Before/After mixture.
// The caller independently qualifies expected and holds all target/evidence
// leases through completion. It never materializes or rolls forward After bytes.
func RecoverFileSet5(ctx context.Context, evidenceRoot string, expected PublicationIntent, revision string, budget PhysicalWorkBudget) error {
	plan, err := loadQualifiedPublication(ctx, evidenceRoot, expected, revision, budget)
	if err != nil {
		return err
	}
	defer plan.rootWitness.Close()
	if err := plan.admitWork(budget, publicationRecoveryWork); err != nil {
		return err
	}
	// The cancellable read-only prefix must finish before compensation starts.
	disposition, err := plan.inspect(ctx, revision)
	if err != nil {
		return err
	}
	return plan.finish(context.WithoutCancel(ctx), disposition, revision)
}

func loadQualifiedPublication(ctx context.Context, root string, expected PublicationIntent, revision string, budget PhysicalWorkBudget) (*publicationPlan, error) {
	if ctx == nil {
		return nil, fmt.Errorf("publication recovery context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := expected.Validate(); err != nil {
		return nil, err
	}
	if !isPublicationHash(revision) {
		return nil, fmt.Errorf("%w: exact marker revision is required", ErrRecoveryIntentMismatch)
	}
	view, err := ReadPublicationIntentEvidence5(ctx, root, mutationfs.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return nil, err
	}
	if view.markerRevision != revision || !samePublicationIntent(view.intent, expected) {
		return nil, fmt.Errorf("%w: publication selection or revision changed", ErrRecoveryIntentMismatch)
	}
	plan, err := bindPublicationPlan(ctx, view.evidenceRoot, expected, view.headerSize, view.intentSize, budget)
	if err != nil {
		return nil, err
	}
	plan.activeIdentity = view.directoryIdentity
	return plan, nil
}

func samePublicationIntent(left, right PublicationIntent) bool {
	return left.Equal(right)
}

func (plan *publicationPlan) inspect(ctx context.Context, revision string) (RecoveryDisposition, error) {
	// This read is included in admitWork; nil avoids debiting the caller twice.
	view, err := readPublicationIntentEvidence(ctx, plan.root, mutationfs.MaximumPhysicalPathDepth, nil)
	if err != nil {
		return "", err
	}
	if view.markerRevision != revision || !samePublicationIntent(view.intent, plan.intent) || !samePublicationDirectory(view.directoryIdentity, plan.activeIdentity) {
		return "", fmt.Errorf("%w: publication evidence changed", ErrRecoveryIntentMismatch)
	}
	for index := range plan.intent.targets {
		if _, err := plan.readBackup(ctx, index); err != nil {
			return "", err
		}
	}
	classification, err := classifyPublicationTargets(ctx, plan.intent)
	if err != nil {
		return "", err
	}
	if classification.cleanAfter {
		return RecoveryFinalize, nil
	}
	if !classification.recoverable {
		return "", fmt.Errorf("publication contains a target outside its Before/After evidence")
	}
	return RecoveryRollback, nil
}

func (plan *publicationPlan) settle(ctx context.Context, revision string) (RecoveryDisposition, error) {
	disposition, err := plan.inspect(ctx, revision)
	if err != nil {
		return "", err
	}
	return disposition, plan.finish(ctx, disposition, revision)
}

func (plan *publicationPlan) finish(ctx context.Context, disposition RecoveryDisposition, revision string) error {
	if disposition != RecoveryRollback && disposition != RecoveryFinalize {
		return fmt.Errorf("unknown publication recovery disposition")
	}
	if disposition == RecoveryRollback {
		for index := range plan.intent.targets {
			if err := plan.restoreTarget(ctx, index); err != nil {
				return err
			}
		}
	}
	for _, target := range plan.intent.targets {
		terminal := target.after
		if disposition == RecoveryRollback {
			terminal = target.before
		}
		if err := confirmPublicationTarget(ctx, target.path, terminal); err != nil {
			return fmt.Errorf("confirm terminal publication target %q: %w", target.path, err)
		}
	}
	return plan.cleanupEvidence(ctx, revision)
}

func confirmPublicationTarget(ctx context.Context, path string, expected PublicationImage) error {
	current, _, identity, err := readPublicationTarget(ctx, path, expected.size)
	if err != nil {
		return err
	}
	if !current.Equal(expected) {
		return fmt.Errorf("terminal publication image changed")
	}
	if expected.exists {
		return storagecommit.ConfirmFilePersistence(ctx, path, identity)
	}
	capability, err := acquirePublicationTarget(path)
	if err != nil {
		return err
	}
	_, err = storagecommit.ConfirmRootedEntryAbsentWithOutcome(ctx, capability)
	return err
}

func (plan *publicationPlan) restoreTarget(ctx context.Context, index int) error {
	target := plan.intent.targets[index]
	current, _, identity, err := readPublicationTarget(ctx, target.path, max(target.before.size, target.after.size))
	if err != nil {
		return err
	}
	if current.Equal(target.before) {
		return nil
	}
	if !current.Equal(target.after) {
		return fmt.Errorf("publication target %q changed before restoration", target.path)
	}
	if target.before.exists {
		content, err := plan.readBackup(ctx, index)
		if err != nil {
			return err
		}
		return commitPublicationImage(ctx, target.path, current, identity, content, target.before.mode)
	}
	capability, err := acquirePublicationTarget(target.path)
	if err != nil {
		return err
	}
	return cleanupPublicationEntry(ctx, capability, identity, publicationFileLimits(target.after.size))
}

func acquirePublicationTarget(path string) (rootedpath.CommitCapability, error) {
	d, err := fileSetAbsolutePathWork(path, mutationfs.MaximumPhysicalPathDepth)
	if err != nil {
		return nil, err
	}
	traversal := &prepaidPublicationTraversal{remaining: d}
	witness, destination, err := rootedpath.CaptureDestinationBounded(path, mutationfs.MaximumPhysicalPathDepth, traversal)
	if err != nil {
		return nil, err
	}
	defer witness.Close()
	lexical, err := destination.LexicalPath()
	if err != nil || lexical != path {
		return nil, fmt.Errorf("%w: publication namespace changed", ErrRecoveryIntentMismatch)
	}
	return witness.Acquire(destination)
}

func (plan *publicationPlan) cleanupEvidence(ctx context.Context, revision string) error {
	view, err := readPublicationIntentEvidence(ctx, plan.root, mutationfs.MaximumPhysicalPathDepth, nil)
	if err != nil {
		return err
	}
	if view.markerRevision != revision || !samePublicationIntent(view.intent, plan.intent) || !samePublicationDirectory(view.directoryIdentity, plan.activeIdentity) {
		return fmt.Errorf("%w: evidence changed before retirement", ErrRecoveryIntentMismatch)
	}
	identity := view.directoryIdentity
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	name := fmt.Sprintf("%s%x", publicationRetiredPrefix, nonce)
	if err := plan.renameEvidence(ctx, transactionDirName, name, identity); err != nil {
		if err := plan.resolveRetirement(ctx, name, identity, revision, err); err != nil {
			return err
		}
	}
	// Retirement is durable before optional collection. Never sweep the active
	// name or turn a post-terminal cleanup failure into a publication failure.
	collectRetiredPublication(ctx, plan.root, name, identity)
	return nil
}

func cleanupPublicationEntry(ctx context.Context, capability rootedpath.CommitCapability, identity storagecommit.EntryIdentity, limits mutationfs.TreeTraversalLimits) error {
	request, err := storagecommit.NewRootedEntryCleanup(capability, identity, limits)
	if err != nil {
		_ = capability.Close()
		return err
	}
	_, err = storagecommit.CommitRootedEntryCleanup(ctx, request)
	return err
}

type prepaidPublicationTraversal struct{ remaining int }

func (budget *prepaidPublicationTraversal) AdmitPathComponents(count int) error {
	if count < 0 || count > budget.remaining {
		return fmt.Errorf("prepaid publication traversal exhausted")
	}
	budget.remaining -= count
	return nil
}
