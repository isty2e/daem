package journal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

// inventoryOptions supplies the only observation capabilities used to
// classify a recovery root.
type inventoryOptions struct {
	Filesystem mutationfs.Reader
	StateCodec durable.SnapshotCodec
}

type recoveryRootInventory struct {
	decision retirement.Decision
	active   *activeJournalEvidence
	root     mutationfs.DirectorySnapshot
	control  *mutationfs.DirectorySnapshot
}

type journalDirectoryEvidence struct {
	identity     retirement.Identity
	journal      recoveryJournal
	operationDir string
}

type activeJournalEvidence struct {
	journalDirectoryEvidence
	physicalAuthority ActiveJournalAuthority
}

const (
	maximumRecoveryRootEntries        = 4_096
	maximumRecoveryJournalEntries     = 100_000
	maximumRetirementControlEntries   = 64
	maximumRecoveryTreeEntries        = 100_000
	maximumRecoveryTreeDepth          = 64
	maximumRecoveryTreeBytes          = 4 << 30
	maximumRetirementControlTreeDepth = 0
	maximumRetirementControlTreeBytes = 1 << 20
)

func recoveryTreeTraversalLimits() mutationfs.TreeTraversalLimits {
	limits, err := mutationfs.NewTreeTraversalLimits(
		maximumRecoveryTreeEntries,
		maximumRecoveryTreeDepth,
		maximumRecoveryTreeBytes,
	)
	if err != nil {
		panic(err)
	}
	return limits
}

func retirementControlTraversalLimits() mutationfs.TreeTraversalLimits {
	limits, err := mutationfs.NewTreeTraversalLimits(
		maximumRetirementControlEntries,
		maximumRetirementControlTreeDepth,
		maximumRetirementControlTreeBytes,
	)
	if err != nil {
		panic(err)
	}
	return limits
}

func loadRecoveryRootInventory(
	ctx context.Context,
	recoveryRoot string,
	options inventoryOptions,
) (recoveryRootInventory, error) {
	if ctx == nil {
		return recoveryRootInventory{}, fmt.Errorf("recovery inventory context is required")
	}
	if options.Filesystem == nil {
		return recoveryRootInventory{}, fmt.Errorf("recovery inventory filesystem is required")
	}
	if strings.TrimSpace(recoveryRoot) == "" {
		return recoveryRootInventory{}, fmt.Errorf("recovery directory is required")
	}
	physicalRoot, err := mutation.CanonicalDirectoryEntryPath(recoveryRoot)
	if err != nil {
		return recoveryRootInventory{}, fmt.Errorf("canonicalize recovery directory: %w", err)
	}

	root, err := options.Filesystem.SnapshotDirectory(
		ctx,
		physicalRoot,
		maximumRecoveryRootEntries,
	)
	if errors.Is(err, os.ErrNotExist) {
		return emptyRecoveryRootInventory(), nil
	}
	if err != nil {
		return recoveryRootInventory{}, fmt.Errorf("snapshot recovery directory: %w", err)
	}

	var (
		activeEntries  []mutationfs.DirectoryEntrySnapshot
		controlEntries []mutationfs.DirectoryEntrySnapshot
		residueEntries []mutationfs.DirectoryEntrySnapshot
		garbageEntries []mutationfs.DirectoryEntrySnapshot
		blockers       []retirement.Blocker
	)
	for _, entry := range root.Entries() {
		name := retirement.InspectName(entry.Name())
		if blocker, blocked := retirement.BlockerForName(name); blocked {
			blockers = append(blockers, blocker)
			continue
		}
		switch name.Kind() {
		case retirement.NameControl:
			controlEntries = append(controlEntries, entry)
		case retirement.NameResidue:
			residueEntries = append(residueEntries, entry)
		case retirement.NameGC:
			garbageEntries = append(garbageEntries, entry)
		case retirement.NameUnrelated:
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			if err := retirement.ValidateOperationID(entry.Name()); err != nil {
				blockers = append(blockers, mustInventoryBlocker(
					entry.Name(),
					fmt.Sprintf("foreign visible recovery entry %q: %v", entry.Name(), err),
				))
				continue
			}
			activeEntries = append(activeEntries, entry)
		default:
			blockers = append(blockers, mustInventoryBlocker(
				entry.Name(),
				fmt.Sprintf("unsupported recovery entry %q", entry.Name()),
			))
		}
	}
	observeNested := len(blockers) == 0
	controls := make([]retirement.Control, 0, len(controlEntries))
	controlSnapshots := make([]mutationfs.DirectorySnapshot, 0, len(controlEntries))
	for _, entry := range controlEntries {
		if !observeNested {
			break
		}
		control, snapshot, loadErr := loadRetirementControl(
			ctx,
			physicalRoot,
			entry,
			options.Filesystem,
		)
		if loadErr != nil {
			if inventoryObservationInterrupted(ctx, loadErr) {
				return recoveryRootInventory{}, loadErr
			}
			blockers = append(blockers, mustInventoryBlocker(entry.Name(), loadErr.Error()))
			observeNested = false
			break
		}
		controls = append(controls, control)
		controlSnapshots = append(controlSnapshots, snapshot)
		if len(controls) > 1 {
			observeNested = false
			break
		}
	}

	activeIdentities := make([]retirement.Identity, 0, len(activeEntries))
	var active *activeJournalEvidence
	for _, entry := range activeEntries {
		if !observeNested {
			break
		}
		loaded, loadErr := loadActiveJournalEvidence(
			ctx,
			physicalRoot,
			entry,
			options,
		)
		if loadErr != nil {
			if inventoryObservationInterrupted(ctx, loadErr) {
				return recoveryRootInventory{}, loadErr
			}
			blockers = append(blockers, mustInventoryBlocker(entry.Name(), loadErr.Error()))
			observeNested = false
			break
		}
		physicalAuthority, authorityErr := newActiveJournalAuthority(entry.Identity())
		if authorityErr != nil {
			blockers = append(
				blockers,
				mustInventoryBlocker(entry.Name(), authorityErr.Error()),
			)
			observeNested = false
			break
		}
		activeIdentities = append(activeIdentities, loaded.identity)
		if active == nil {
			candidate := activeJournalEvidence{
				journalDirectoryEvidence: loaded,
				physicalAuthority:        physicalAuthority,
			}
			active = &candidate
		}
		if len(activeIdentities) > 1 {
			observeNested = false
			break
		}
	}

	residues := make([]retirement.Residue, 0, len(residueEntries))
	for _, entry := range residueEntries {
		evidence, evidenceErr := retirementEvidence(entry)
		if evidenceErr != nil {
			blockers = append(blockers, mustInventoryBlocker(entry.Name(), evidenceErr.Error()))
			continue
		}
		partial, partialErr := retirement.ValidatePartialResidue(evidence)
		if partialErr != nil {
			blockers = append(blockers, mustInventoryBlocker(entry.Name(), partialErr.Error()))
			continue
		}

		if observeNested &&
			len(controls) == 1 &&
			controls[0].Record().Phase() == retirement.PhasePrepared &&
			retirement.InspectName(entry.Name()).BelongsTo(controls[0].Record().Identity()) {
			loaded, loadErr := loadResidueJournalEvidence(
				ctx,
				physicalRoot,
				entry,
				options,
			)
			if loadErr != nil {
				if inventoryObservationInterrupted(ctx, loadErr) {
					return recoveryRootInventory{}, loadErr
				}
				blockers = append(blockers, mustInventoryBlocker(entry.Name(), loadErr.Error()))
				continue
			}
			correlated, correlateErr := retirement.ValidateResidue(evidence, loaded.identity)
			if correlateErr != nil {
				blockers = append(blockers, mustInventoryBlocker(entry.Name(), correlateErr.Error()))
				continue
			}
			residues = append(residues, correlated)
			continue
		}
		residues = append(residues, partial)
	}

	garbage := make([]retirement.Garbage, 0, len(garbageEntries))
	for _, entry := range garbageEntries {
		evidence, evidenceErr := retirementEvidence(entry)
		if evidenceErr != nil {
			blockers = append(blockers, mustInventoryBlocker(entry.Name(), evidenceErr.Error()))
			continue
		}
		artifact, validateErr := retirement.ValidateGarbage(evidence)
		if validateErr != nil {
			blockers = append(blockers, mustInventoryBlocker(entry.Name(), validateErr.Error()))
			continue
		}
		garbage = append(garbage, artifact)
	}

	currentRoot, err := options.Filesystem.SnapshotDirectory(
		ctx,
		physicalRoot,
		maximumRecoveryRootEntries,
	)
	if err != nil {
		return recoveryRootInventory{}, fmt.Errorf("revalidate recovery directory: %w", err)
	}
	if !root.Equal(currentRoot) {
		return recoveryRootInventory{}, fmt.Errorf("recovery directory changed while inventorying")
	}

	decision := retirement.Classify(retirement.NewLayoutEvidence(
		activeIdentities,
		controls,
		residues,
		garbage,
		blockers,
	))
	switch decision.State() {
	case retirement.StateActive, retirement.StatePrepared:
		if active == nil || len(activeIdentities) != 1 {
			return recoveryRootInventory{}, fmt.Errorf(
				"active recovery classification has no unique loaded journal",
			)
		}
	default:
		active = nil
	}
	var control *mutationfs.DirectorySnapshot
	if len(controlSnapshots) == 1 {
		snapshot := controlSnapshots[0]
		control = &snapshot
	}
	return recoveryRootInventory{
		decision: decision,
		active:   active,
		root:     root,
		control:  control,
	}, nil
}

func emptyRecoveryRootInventory() recoveryRootInventory {
	return recoveryRootInventory{
		decision: retirement.Classify(retirement.NewLayoutEvidence(
			nil,
			nil,
			nil,
			nil,
			nil,
		)),
	}
}

func loadRetirementControl(
	ctx context.Context,
	recoveryRoot string,
	entry mutationfs.DirectoryEntrySnapshot,
	filesystem mutationfs.Reader,
) (retirement.Control, mutationfs.DirectorySnapshot, error) {
	directoryEvidence, err := retirementEvidence(entry)
	if err != nil {
		return retirement.Control{}, mutationfs.DirectorySnapshot{}, err
	}
	if entry.Kind() != mutationfs.EntryKindDirectory ||
		!entry.OwnedByInvoker() ||
		entry.Mode() != retirement.DirectoryMode {
		_, validationErr := retirement.ValidateControl(retirement.ControlEvidence{
			Directory: directoryEvidence,
		})
		return retirement.Control{}, mutationfs.DirectorySnapshot{}, validationErr
	}

	controlPath := filepath.Join(recoveryRoot, entry.Name())
	before, err := filesystem.SnapshotDirectory(
		ctx,
		controlPath,
		maximumRetirementControlEntries,
	)
	if err != nil {
		return retirement.Control{}, mutationfs.DirectorySnapshot{}, fmt.Errorf(
			"snapshot retirement control %q: %w",
			entry.Name(),
			err,
		)
	}
	if before.RootIdentity() == nil ||
		!before.RootIdentity().Equal(entry.Identity()) ||
		before.RootMode() != entry.Mode() ||
		before.RootOwnedByInvoker() != entry.OwnedByInvoker() {
		return retirement.Control{}, mutationfs.DirectorySnapshot{}, fmt.Errorf(
			"retirement control %q changed before inspection",
			entry.Name(),
		)
	}

	children := make([]retirement.EntryEvidence, 0, len(before.Entries()))
	var recordContent []byte
	for _, child := range before.Entries() {
		childEvidence, evidenceErr := retirementEvidence(child)
		if evidenceErr != nil {
			return retirement.Control{}, mutationfs.DirectorySnapshot{}, evidenceErr
		}
		children = append(children, childEvidence)
		if child.Name() != retirement.RecordFileName ||
			child.Kind() != mutationfs.EntryKindFile ||
			!child.OwnedByInvoker() ||
			child.Mode() != retirement.RecordMode ||
			child.Size() > retirement.MaximumRecordBytes {
			continue
		}
		snapshot, readErr := filesystem.ReadRegularFileSnapshotUpTo(
			ctx,
			filepath.Join(controlPath, retirement.RecordFileName),
			retirement.MaximumRecordBytes,
		)
		if readErr != nil {
			return retirement.Control{}, mutationfs.DirectorySnapshot{}, fmt.Errorf(
				"read retirement control %q: %w",
				entry.Name(),
				readErr,
			)
		}
		if snapshot.Identity() == nil || !snapshot.Identity().Equal(child.Identity()) {
			return retirement.Control{}, mutationfs.DirectorySnapshot{}, fmt.Errorf(
				"retirement record in %q changed while reading",
				entry.Name(),
			)
		}
		recordContent = snapshot.Content()
	}

	after, err := filesystem.SnapshotDirectory(
		ctx,
		controlPath,
		maximumRetirementControlEntries,
	)
	if err != nil {
		return retirement.Control{}, mutationfs.DirectorySnapshot{}, fmt.Errorf(
			"revalidate retirement control %q: %w",
			entry.Name(),
			err,
		)
	}
	if !before.Equal(after) {
		return retirement.Control{}, mutationfs.DirectorySnapshot{}, fmt.Errorf(
			"retirement control %q changed while inspecting",
			entry.Name(),
		)
	}
	control, err := retirement.ValidateControl(retirement.ControlEvidence{
		Directory:     directoryEvidence,
		Children:      children,
		RecordContent: recordContent,
	})
	if err != nil {
		return retirement.Control{}, mutationfs.DirectorySnapshot{}, err
	}
	return control, before, nil
}

func loadActiveJournalEvidence(
	ctx context.Context,
	recoveryRoot string,
	entry mutationfs.DirectoryEntrySnapshot,
	options inventoryOptions,
) (journalDirectoryEvidence, error) {
	loaded, err := loadJournalDirectoryEvidence(
		ctx,
		filepath.Join(recoveryRoot, entry.Name()),
		entry,
		options,
	)
	if err != nil {
		return journalDirectoryEvidence{}, err
	}
	if loaded.journal.OperationID != entry.Name() {
		return journalDirectoryEvidence{}, fmt.Errorf(
			"recovery journal operation_id %q does not match directory %q",
			loaded.journal.OperationID,
			entry.Name(),
		)
	}
	return loaded, nil
}

func loadResidueJournalEvidence(
	ctx context.Context,
	recoveryRoot string,
	entry mutationfs.DirectoryEntrySnapshot,
	options inventoryOptions,
) (journalDirectoryEvidence, error) {
	return loadJournalDirectoryEvidence(
		ctx,
		filepath.Join(recoveryRoot, entry.Name()),
		entry,
		options,
	)
}

func loadJournalDirectoryEvidence(
	ctx context.Context,
	directoryPath string,
	entry mutationfs.DirectoryEntrySnapshot,
	options inventoryOptions,
) (journalDirectoryEvidence, error) {
	if err := validateJournalDirectoryEntry(entry); err != nil {
		return journalDirectoryEvidence{}, err
	}
	before, err := options.Filesystem.SnapshotDirectory(
		ctx,
		directoryPath,
		maximumRecoveryJournalEntries,
	)
	if err != nil {
		return journalDirectoryEvidence{}, fmt.Errorf(
			"snapshot recovery journal entry %q: %w",
			entry.Name(),
			err,
		)
	}
	if before.RootIdentity() == nil ||
		!before.RootIdentity().Equal(entry.Identity()) ||
		before.RootMode() != entry.Mode() ||
		before.RootOwnedByInvoker() != entry.OwnedByInvoker() {
		return journalDirectoryEvidence{}, fmt.Errorf(
			"recovery journal entry %q changed before inspection",
			entry.Name(),
		)
	}

	var journalEntry *mutationfs.DirectoryEntrySnapshot
	for _, child := range before.Entries() {
		if child.Name() == recoveryJournalFileName {
			candidate := child
			journalEntry = &candidate
			break
		}
	}
	if journalEntry == nil {
		return journalDirectoryEvidence{}, fmt.Errorf(
			"recovery journal entry %q has no %s",
			entry.Name(),
			recoveryJournalFileName,
		)
	}
	if journalEntry.Kind() != mutationfs.EntryKindFile ||
		!journalEntry.OwnedByInvoker() ||
		journalEntry.Mode() != recoveryJournalMode ||
		journalEntry.Size() > maximumRecoveryJournalBytes {
		return journalDirectoryEvidence{}, fmt.Errorf(
			"recovery journal %q has invalid file metadata",
			filepath.Join(entry.Name(), recoveryJournalFileName),
		)
	}

	journalPath := filepath.Join(directoryPath, recoveryJournalFileName)
	snapshot, err := options.Filesystem.ReadRegularFileSnapshotUpTo(
		ctx,
		journalPath,
		maximumRecoveryJournalBytes,
	)
	if err != nil {
		return journalDirectoryEvidence{}, fmt.Errorf("read recovery journal: %w", err)
	}
	if snapshot.Identity() == nil || !snapshot.Identity().Equal(journalEntry.Identity()) {
		return journalDirectoryEvidence{}, fmt.Errorf(
			"recovery journal %q changed while reading",
			entry.Name(),
		)
	}
	journal, err := decodeRecoveryJournalSnapshot(snapshot, journalPath, options.StateCodec)
	if err != nil {
		return journalDirectoryEvidence{}, err
	}
	fingerprint, err := recoveryJournalAuthorityFingerprint(journal, options.StateCodec)
	if err != nil {
		return journalDirectoryEvidence{}, err
	}
	identity, err := retirement.NewIdentity(journal.OperationID, fingerprint)
	if err != nil {
		return journalDirectoryEvidence{}, fmt.Errorf("derive recovery journal retirement identity: %w", err)
	}
	after, err := options.Filesystem.SnapshotDirectory(
		ctx,
		directoryPath,
		maximumRecoveryJournalEntries,
	)
	if err != nil {
		return journalDirectoryEvidence{}, fmt.Errorf(
			"revalidate recovery journal entry %q: %w",
			entry.Name(),
			err,
		)
	}
	if !before.Equal(after) {
		return journalDirectoryEvidence{}, fmt.Errorf(
			"recovery journal entry %q changed while inspecting",
			entry.Name(),
		)
	}
	return journalDirectoryEvidence{
		identity:     identity,
		journal:      journal,
		operationDir: directoryPath,
	}, nil
}

func validateJournalDirectoryEntry(
	entry mutationfs.DirectoryEntrySnapshot,
) error {
	if entry.Kind() != mutationfs.EntryKindDirectory {
		return fmt.Errorf(
			"recovery journal entry %q must be a no-follow directory",
			entry.Name(),
		)
	}
	if !entry.OwnedByInvoker() {
		return fmt.Errorf(
			"recovery journal entry %q is not owned by the invoking user",
			entry.Name(),
		)
	}
	if entry.Mode() != retirement.DirectoryMode {
		return fmt.Errorf(
			"recovery journal entry %q permissions are %04o, want %04o",
			entry.Name(),
			entry.Mode(),
			retirement.DirectoryMode,
		)
	}
	return nil
}

func retirementEvidence(
	entry mutationfs.DirectoryEntrySnapshot,
) (retirement.EntryEvidence, error) {
	var kind retirement.EntryKind
	switch entry.Kind() {
	case mutationfs.EntryKindFile:
		kind = retirement.EntryRegular
	case mutationfs.EntryKindDirectory:
		kind = retirement.EntryDirectory
	case mutationfs.EntryKindSymlink:
		kind = retirement.EntrySymlink
	case mutationfs.EntryKindSpecial:
		kind = retirement.EntrySpecial
	default:
		return retirement.EntryEvidence{}, fmt.Errorf(
			"recovery entry %q has invalid structural kind",
			entry.Name(),
		)
	}
	return retirement.NewEntryEvidence(
		entry.Name(),
		kind,
		entry.Mode(),
		entry.OwnedByInvoker(),
		entry.Size(),
	)
}

func mustInventoryBlocker(name string, detail string) retirement.Blocker {
	blocker, err := retirement.NewBlocker(
		name,
		fmt.Sprintf("recovery entry %q: %s", name, detail),
	)
	if err != nil {
		panic(err)
	}
	return blocker
}

func inventoryObservationInterrupted(ctx context.Context, err error) bool {
	return ctx.Err() != nil ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, errRecoveryJournalStateCodecRequired)
}
