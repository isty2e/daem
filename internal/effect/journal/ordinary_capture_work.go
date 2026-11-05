package journal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

// Legacy capture keeps its per-artifact policy. Native capture supplies the
// containing allocation, charging reads and backup writes as separate passes.
type ordinaryCaptureWork struct {
	budget               *physicalwork.Budget
	backupParentAdmitted bool
}

func (work *ordinaryCaptureWork) admit(entries int, bytes int64) error {
	if work.budget == nil {
		return nil
	}
	amount, err := workmeasure.NewArtifactWork(entries, bytes)
	if err != nil {
		return err
	}
	return work.budget.AdmitTree(amount)
}

func (work *ordinaryCaptureWork) admitBackupEntry(bytes int64) error {
	entries := 1
	if !work.backupParentAdmitted {
		entries++
	}
	if err := work.admit(entries, bytes); err != nil {
		return err
	}
	work.backupParentAdmitted = true
	return nil
}

func (work *ordinaryCaptureWork) writeFile(path string, content []byte, mode fs.FileMode) error {
	if err := work.admitBackupEntry(int64(len(content))); err != nil {
		return err
	}
	return writeRecoveryBackupFile(path, content, mode)
}

func (work *ordinaryCaptureWork) rootedResolver(resolver RootedCapabilityResolver) RootedCapabilityResolver {
	if work.budget == nil || resolver == nil {
		return resolver
	}
	return func(destination output.Destination, _ rootedpath.PhysicalTraversalBudget) (rootedpath.CommitCapability, bool, error) {
		return resolver(destination, work.budget)
	}
}

func (work *ordinaryCaptureWork) globalBindings(actions []journalrecord.CaptureMutation, resolver func(output.Destination) (string, error), rooted RootedCapabilityResolver) (recoveryGlobalPathBindings, error) {
	if work.budget == nil {
		return captureRecoveryGlobalPathBindings(actions, resolver, rooted)
	}
	if err := journalrecord.ValidateCaptureMutations(actions); err != nil {
		return nil, err
	}
	bindings := make(recoveryGlobalPathBindings)
	for _, action := range actions {
		if action.Scope() != target.ScopeGlobal {
			continue
		}
		if _, present := bindings[action.Destination()]; present {
			continue
		}
		if rooted == nil {
			return nil, fmt.Errorf("native ordinary capture requires retained global authority")
		}
		binding, err := observeRecoveryGlobalPathBindingForRecovery(action.Destination(), resolver, rooted, work.budget)
		if err != nil {
			return nil, err
		}
		bindings[action.Destination()] = binding
	}
	return bindings, nil
}

type ordinaryCaptureReader struct {
	mutationfs.Reader
	work *ordinaryCaptureWork
}

func (reader ordinaryCaptureReader) readLimit(maximum int64) (int64, error) {
	if reader.work.budget.RemainingBytes() <= 0 {
		return 0, fmt.Errorf("ordinary capture exhausted the remaining byte allocation")
	}
	return min(maximum, reader.work.budget.RemainingBytes()), nil
}

func (reader ordinaryCaptureReader) chargeRead(content []byte, maximum int64, readErr error) error {
	charged := int64(len(content))
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		charged = maximum
	}
	return reader.work.admit(0, charged)
}

func (reader ordinaryCaptureReader) ReadRootedRegularFile(ctx context.Context, capability rootedpath.CommitCapability) ([]byte, fs.FileMode, mutationfs.EntryIdentity, error) {
	return reader.ReadRootedRegularFileUpTo(ctx, capability, recovery.MaximumRecoveryBackupFileBytes)
}

func (reader ordinaryCaptureReader) ReadRootedRegularFileUpTo(ctx context.Context, capability rootedpath.CommitCapability, maximum int64) ([]byte, fs.FileMode, mutationfs.EntryIdentity, error) {
	limit, err := reader.readLimit(maximum)
	if err != nil {
		return nil, 0, nil, err
	}
	content, mode, identity, readErr := reader.Reader.ReadRootedRegularFileUpTo(ctx, capability, limit)
	if err := errors.Join(readErr, reader.chargeRead(content, limit, readErr)); err != nil {
		return nil, 0, nil, err
	}
	return content, mode, identity, nil
}

func (reader ordinaryCaptureReader) ReadRegularFileSnapshotUpTo(ctx context.Context, path string, maximum int64) (mutationfs.RegularFileSnapshot, error) {
	limit, err := reader.readLimit(maximum)
	if err != nil {
		return mutationfs.RegularFileSnapshot{}, err
	}
	snapshot, readErr := reader.Reader.ReadRegularFileSnapshotUpTo(ctx, path, limit)
	if err := errors.Join(readErr, reader.chargeRead(snapshot.Content(), limit, readErr)); err != nil {
		return mutationfs.RegularFileSnapshot{}, err
	}
	return snapshot, nil
}

func (reader ordinaryCaptureReader) SnapshotRootedDirectory(ctx context.Context, capability rootedpath.CommitCapability, limits mutationfs.TreeTraversalLimits, sink mutationfs.RootedTreeSnapshotSink) (mutationfs.EntryIdentity, error) {
	// Each descendant is read and copied. Keep room for the backup root, its
	// optional files/ parent and one overflow directory-entry observation.
	auxiliary := 2
	if !reader.work.backupParentAdmitted {
		auxiliary++
	}
	remaining := reader.work.budget.RemainingEntries() - auxiliary
	if remaining < 0 {
		return nil, fmt.Errorf("ordinary directory capture exhausted its entry allocation")
	}
	bounded, err := mutationfs.NewTreeTraversalLimits(min(limits.MaximumEntries(), remaining/2), limits.MaximumDepth(), min(limits.MaximumBytes(), reader.work.budget.RemainingBytes()/2))
	if err != nil {
		return nil, err
	}
	measured := &ordinaryCaptureReadSink{RootedTreeSnapshotSink: sink, work: reader.work}
	identity, readErr := reader.Reader.SnapshotRootedDirectory(ctx, capability, bounded, measured)
	if readErr != nil {
		unobservedEntries := max(0, bounded.MaximumEntries()+1-measured.entries)
		unobservedBytes := max(int64(0), bounded.MaximumBytes()-measured.bytes)
		readErr = errors.Join(readErr, reader.work.admit(unobservedEntries, unobservedBytes))
	}
	return identity, readErr
}

type ordinaryCaptureReadSink struct {
	mutationfs.RootedTreeSnapshotSink
	work    *ordinaryCaptureWork
	entries int
	bytes   int64
}

func (sink *ordinaryCaptureReadSink) VisitDirectory(path mutationfs.TreeRelativePath, mode fs.FileMode) error {
	if err := sink.work.admit(1, 0); err != nil {
		return err
	}
	sink.entries++
	return sink.RootedTreeSnapshotSink.VisitDirectory(path, mode)
}

func (sink *ordinaryCaptureReadSink) VisitRegularFile(path mutationfs.TreeRelativePath, mode fs.FileMode, size int64, content io.Reader) error {
	if err := sink.work.admit(1, size); err != nil {
		return err
	}
	sink.entries++
	sink.bytes += size
	return sink.RootedTreeSnapshotSink.VisitRegularFile(path, mode, size, content)
}

func (work *ordinaryCaptureWork) removalIntents(ctx context.Context, demands recovery.RemovalDemandSet, resolver func(output.Destination) (string, error)) ([]recovery.RemovalIntent, error) {
	if work.budget == nil {
		return captureRemovalIntents(ctx, demands, resolver)
	}
	return captureRemovalIntentsWithBudget(ctx, demands, resolver, work.budget)
}
