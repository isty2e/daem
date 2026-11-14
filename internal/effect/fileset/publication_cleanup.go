package fileset

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

const (
	publicationRetiredPrefix           = ".daem-retired-"
	publicationCleanupEffectsSupported = runtime.GOOS == "darwin" || runtime.GOOS == "linux"
)

type publicationGarbage struct {
	name     string
	snapshot mutationfs.DirectorySnapshot
}

// PublicationCleanup selects inactive artifacts only. The caller supplies the
// dedicated FileSet5 root and holds its exclusive root/authority leases during
// Execute. Neither inspection nor execution acquires target authority.
type PublicationCleanup struct {
	root          string
	witness       *rootedpath.CapturedRoot
	destination   rootedpath.Destination
	identity      storagecommit.EntryIdentity
	selected      []publicationGarbage
	deferred      []string
	executionWork fileSetPhysicalWork
	executeOnce   sync.Once
}

func InspectPublicationCleanup(ctx context.Context, root string, budget PhysicalWorkBudget) (*PublicationCleanup, error) {
	if ctx == nil || budget == nil {
		return nil, fmt.Errorf("publication cleanup requires context and physical work budget")
	}
	canonical, err := canonicalStateDirBounded(root, mutationfs.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return nil, err
	}
	if err := ObserveClearPublicationFenceAt(ctx, canonical, mutationfs.MaximumPhysicalPathDepth, budget); err != nil {
		return nil, err
	}
	plan := &PublicationCleanup{root: canonical}
	witness, destination, err := rootedpath.CaptureDestinationBounded(canonical, mutationfs.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return nil, err
	}
	plan.witness, plan.destination = witness, destination
	snapshot, more, err := plan.rootBatch(ctx, maximumStateDirFenceEntries, budget)
	if errors.Is(err, os.ErrNotExist) {
		return plan, nil
	}
	if err != nil {
		plan.deferred = append(plan.deferred, err.Error())
		if ctx.Err() != nil {
			plan.Close()
			return nil, ctx.Err()
		}
		return plan, nil
	}
	if more {
		plan.deferred = append(plan.deferred, "additional metadata directory entries were not inspected")
	}
	if !publicationCleanupEffectsSupported {
		for _, entry := range snapshot.Entries() {
			if publicationArtifactName(entry.Name()) {
				plan.deferred = append(plan.deferred, "inactive publication cleanup is unsupported on this platform")
				break
			}
		}
		return plan, nil
	}
	identity, ok := snapshot.RootIdentity().(storagecommit.EntryIdentity)
	if !ok || !snapshot.RootOwnedByInvoker() || snapshot.RootMode() != 0o700 {
		plan.deferred = append(plan.deferred, "publication cleanup root ownership or private mode is unproven")
		return plan, nil
	}
	plan.identity = identity
	if err := plan.reserveRoot(budget); err != nil {
		plan.deferred = append(plan.deferred, err.Error())
		return plan, nil
	}
	for _, entry := range snapshot.Entries() {
		if !publicationArtifactName(entry.Name()) {
			continue
		}
		if entry.Kind() != mutationfs.EntryKindDirectory || !entry.OwnedByInvoker() || entry.Mode() != 0o700 {
			plan.deferred = append(plan.deferred, "preserved unqualified artifact: "+entry.Name())
			continue
		}
		tree, err := plan.snapshot(ctx, entry.Name(), budget)
		if err != nil || !publicationGarbageShape(tree) {
			if err == nil {
				err = fmt.Errorf("unrecognized private publication shape")
			}
			plan.deferred = append(plan.deferred, entry.Name()+": "+err.Error())
			if ctx.Err() != nil {
				plan.Close()
				return nil, ctx.Err()
			}
			continue
		}
		work, err := plan.candidateWork(entry.Name(), tree)
		if err == nil {
			err = plan.reserve(budget, work)
		}
		if err != nil {
			plan.deferred = append(plan.deferred, "remaining inactive work deferred: "+err.Error())
			break
		}
		plan.selected = append(plan.selected, publicationGarbage{name: entry.Name(), snapshot: tree})
	}
	return plan, nil
}

func (plan *PublicationCleanup) Paths() []string {
	paths := make([]string, len(plan.selected))
	for index, entry := range plan.selected {
		paths[index] = filepath.Join(plan.root, entry.name)
	}
	return paths
}

func (plan *PublicationCleanup) Deferred() []string { return slices.Clone(plan.deferred) }

func (plan *PublicationCleanup) Root() string { return plan.root }

func (plan *PublicationCleanup) Close() {
	if plan.witness != nil {
		_ = plan.witness.Close()
	}
}

func (plan *PublicationCleanup) Execute(ctx context.Context) error {
	if ctx == nil || plan.witness == nil {
		return fmt.Errorf("publication cleanup is not initialized")
	}
	started := false
	plan.executeOnce.Do(func() { started = true })
	if !started {
		return fmt.Errorf("publication cleanup has already been consumed")
	}
	if len(plan.selected) == 0 {
		return ctx.Err()
	}
	budget := &publicationCleanupCursor{remaining: plan.executionWork}
	if err := ObserveClearPublicationFenceAt(ctx, plan.root, mutationfs.MaximumPhysicalPathDepth, budget); err != nil {
		return err
	}
	root, _, err := plan.rootBatch(ctx, 0, budget)
	if err != nil {
		return err
	}
	identity, ok := root.RootIdentity().(storagecommit.EntryIdentity)
	if !ok || !identity.SameObject(plan.identity) || !root.RootOwnedByInvoker() || root.RootMode() != 0o700 {
		return fmt.Errorf("publication cleanup root changed")
	}
	// Revalidate the complete disclosed selection before deleting its first file.
	for _, selected := range plan.selected {
		fresh, err := plan.snapshotUpTo(ctx, selected.name, max(1, len(selected.snapshot.Entries())), budget)
		if err != nil {
			return err
		}
		if !fresh.Equal(selected.snapshot) || !publicationGarbageShape(fresh) {
			return fmt.Errorf("publication cleanup candidate changed: %s", selected.name)
		}
	}
	for _, selected := range plan.selected {
		for _, file := range selected.snapshot.Entries() {
			if err := plan.remove(ctx, filepath.Join(selected.name, file.Name()), file.Identity(), file.Size(), budget); err != nil {
				return err
			}
		}
		// An unexpected inserted child is never recursively collected.
		if err := plan.remove(ctx, selected.name, selected.snapshot.RootIdentity(), 0, budget); err != nil {
			return err
		}
	}
	return nil
}

func (plan *PublicationCleanup) bind(relative string) (rootedpath.Destination, error) {
	path := plan.destination.Relative().Path()
	if relative != "" {
		path += "/" + filepath.ToSlash(relative)
	}
	value, err := rootedpath.NewRelativeDestination(path)
	if err != nil {
		return rootedpath.Destination{}, err
	}
	return plan.destination.Root().Bind(value)
}

func (plan *PublicationCleanup) snapshot(ctx context.Context, relative string, budget PhysicalWorkBudget) (mutationfs.DirectorySnapshot, error) {
	return plan.snapshotUpTo(ctx, relative, maximumStateDirFenceEntries, budget)
}

func (plan *PublicationCleanup) snapshotUpTo(ctx context.Context, relative string, maximumEntries int, budget PhysicalWorkBudget) (mutationfs.DirectorySnapshot, error) {
	destination, err := plan.bind(relative)
	if err != nil {
		return mutationfs.DirectorySnapshot{}, err
	}
	work, err := cleanupSnapshotWork(destination, maximumEntries)
	if err != nil {
		return mutationfs.DirectorySnapshot{}, err
	}
	if err := budget.AdmitPhysicalWork(work.pathComponents, work.entries, work.bytes); err != nil {
		return mutationfs.DirectorySnapshot{}, err
	}
	capability, err := plan.witness.Acquire(destination)
	if err != nil {
		return mutationfs.DirectorySnapshot{}, err
	}
	defer capability.Close()
	return storagecommit.SnapshotRootedDirectoryEntries(ctx, capability, maximumEntries)
}

func (plan *PublicationCleanup) rootBatch(ctx context.Context, maximumEntries int, budget PhysicalWorkBudget) (mutationfs.DirectorySnapshot, bool, error) {
	destination, err := plan.bind("")
	if err != nil {
		return mutationfs.DirectorySnapshot{}, false, err
	}
	work, err := cleanupSnapshotWork(destination, maximumEntries)
	if err != nil {
		return mutationfs.DirectorySnapshot{}, false, err
	}
	if err := budget.AdmitPhysicalWork(work.pathComponents, work.entries, work.bytes); err != nil {
		return mutationfs.DirectorySnapshot{}, false, err
	}
	capability, err := plan.witness.Acquire(destination)
	if err != nil {
		return mutationfs.DirectorySnapshot{}, false, err
	}
	defer capability.Close()
	return storagecommit.SnapshotRootedDirectoryBatch(ctx, capability, maximumEntries)
}

func (plan *PublicationCleanup) remove(ctx context.Context, relative string, expected mutationfs.EntryIdentity, size int64, budget PhysicalWorkBudget) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	destination, err := plan.bind(relative)
	if err != nil {
		return err
	}
	identity, ok := expected.(storagecommit.EntryIdentity)
	if !ok {
		return fmt.Errorf("publication cleanup identity is unproven")
	}
	limits := publicationFileLimits(size)
	if expected.Kind() == mutationfs.EntryKindDirectory {
		limits, err = mutationfs.NewTreeTraversalLimits(0, 0, 0)
		if err != nil {
			return err
		}
	}
	work, err := cleanupRemovalWork(destination, expected.Kind(), limits)
	if err != nil {
		return err
	}
	if err := budget.AdmitPhysicalWork(work.pathComponents, work.entries, work.bytes); err != nil {
		return err
	}
	capability, err := plan.witness.Acquire(destination)
	if err != nil {
		return err
	}
	if expected.Kind() == mutationfs.EntryKindDirectory {
		fresh, err := storagecommit.CaptureRootedEntryIdentity(ctx, capability)
		if err != nil {
			_ = capability.Close()
			return err
		}
		if !fresh.SameObject(identity) {
			_ = capability.Close()
			return fmt.Errorf("publication cleanup directory identity changed: %s", relative)
		}
		beforeProtection, beforeErr := identity.ProtectionFingerprint()
		afterProtection, afterErr := fresh.ProtectionFingerprint()
		if beforeErr != nil || afterErr != nil || beforeProtection != afterProtection {
			_ = capability.Close()
			return fmt.Errorf("publication cleanup directory protection changed: %s", relative)
		}
		request, err := storagecommit.NewRootedEmptyDirectoryCleanup(capability, fresh)
		if err != nil {
			_ = capability.Close()
			return err
		}
		_, err = storagecommit.CommitRootedEntryCleanup(ctx, request)
		return err
	}
	return cleanupPublicationEntry(ctx, capability, identity, limits)
}

func publicationArtifactName(name string) bool {
	for _, prefix := range []string{fileSetTemporaryPrefix, publicationRetiredPrefix} {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		nonce := strings.TrimPrefix(name, prefix)
		return len(nonce) == 32 && strings.IndexFunc(nonce, func(r rune) bool {
			return r < '0' || r > '9' && r < 'a' || r > 'f'
		}) == -1
	}
	return false
}

func publicationGarbageShape(snapshot mutationfs.DirectorySnapshot) bool {
	if !snapshot.RootOwnedByInvoker() || snapshot.RootMode() != 0o700 {
		return false
	}
	for _, entry := range snapshot.Entries() {
		if entry.Kind() != mutationfs.EntryKindFile || !entry.OwnedByInvoker() || entry.Mode() != transactionEvidenceMode {
			return false
		}
		if entry.Name() == transactionMarkerFile || entry.Name() == publicationIntentFile {
			continue
		}
		number := strings.TrimSuffix(strings.TrimPrefix(entry.Name(), "target-"), ".before")
		index, err := strconv.Atoi(number)
		if err != nil || index < 0 || entry.Name() != publicationBackupName(index) {
			return false
		}
	}
	return true
}

func collectRetiredPublication(ctx context.Context, root, name string, expected storagecommit.EntryIdentity) {
	if !publicationCleanupEffectsSupported {
		return
	}
	budget := &fenceObservationBudget{}
	witness, destination, err := rootedpath.CaptureDestinationBounded(root, mutationfs.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return
	}
	plan := &PublicationCleanup{root: root, witness: witness, destination: destination}
	defer plan.Close()
	rootSnapshot, _, err := plan.rootBatch(ctx, 0, budget)
	if err != nil {
		return
	}
	identity, ok := rootSnapshot.RootIdentity().(storagecommit.EntryIdentity)
	if !ok || rootSnapshot.RootMode() != 0o700 || !rootSnapshot.RootOwnedByInvoker() {
		return
	}
	plan.identity = identity
	tree, err := plan.snapshot(ctx, name, budget)
	if err != nil || !publicationGarbageShape(tree) {
		return
	}
	retired, ok := tree.RootIdentity().(storagecommit.EntryIdentity)
	if !ok || !retired.SameObject(expected) {
		return
	}
	plan.selected = []publicationGarbage{{name: name, snapshot: tree}}
	if err := plan.reserveRoot(budget); err != nil {
		return
	}
	work, err := plan.candidateWork(name, tree)
	if err != nil {
		return
	}
	if err := plan.reserve(budget, work); err != nil {
		return
	}
	_ = plan.Execute(ctx)
}
