package journal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"strconv"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

type retirementTreeEvidence struct {
	identity          mutationfs.EntryIdentity
	work              recovery.ArtifactWork
	fingerprint       string
	stableFingerprint string
	fileSizes         map[string]int64
	fileFingerprints  map[string]string
}

func (evidence retirementTreeEvidence) valid() bool {
	return evidence.identity != nil &&
		evidence.identity.Kind() == mutationfs.EntryKindDirectory &&
		evidence.fingerprint != ""
}

func (evidence retirementTreeEvidence) sameTree(other retirementTreeEvidence) bool {
	return evidence.valid() && other.valid() &&
		evidence.work.Equal(other.work) &&
		evidence.fingerprint == other.fingerprint
}

func (evidence retirementTreeEvidence) sameTreeExceptMutableFile(
	other retirementTreeEvidence,
) bool {
	return evidence.valid() && other.valid() &&
		evidence.stableFingerprint != "" &&
		evidence.stableFingerprint == other.stableFingerprint
}

func (evidence retirementTreeEvidence) fileFingerprint(path string) string {
	if evidence.fileFingerprints == nil {
		return ""
	}
	return evidence.fileFingerprints[path]
}

type retirementTreeSnapshotSink struct {
	hasher           hash.Hash
	stableHasher     hash.Hash
	mutableFile      string
	entries          int
	bytes            int64
	fileSizes        map[string]int64
	fileFingerprints map[string]string
	failure          error
}

func newRetirementTreeSnapshotSink() *retirementTreeSnapshotSink {
	return newRetirementTreeSnapshotSinkWithMutableFile("")
}

func newRetirementTreeSnapshotSinkWithMutableFile(
	mutableFile string,
) *retirementTreeSnapshotSink {
	return &retirementTreeSnapshotSink{
		hasher:           sha256.New(),
		stableHasher:     sha256.New(),
		mutableFile:      mutableFile,
		fileSizes:        make(map[string]int64),
		fileFingerprints: make(map[string]string),
	}
}

func (sink *retirementTreeSnapshotSink) VisitRoot(mode fs.FileMode) error {
	return sink.writeStableRecord("root", strconv.FormatUint(uint64(mode.Perm()), 8))
}

func (sink *retirementTreeSnapshotSink) VisitDirectory(
	path mutationfs.TreeRelativePath,
	mode fs.FileMode,
) error {
	sink.entries++
	return sink.writeStableRecord(
		"directory",
		path.Path(),
		strconv.FormatUint(uint64(mode.Perm()), 8),
	)
}

func (sink *retirementTreeSnapshotSink) VisitRegularFile(
	path mutationfs.TreeRelativePath,
	mode fs.FileMode,
	size int64,
	content io.Reader,
) error {
	sink.entries++
	sink.bytes += size
	sink.fileSizes[path.Path()] = size
	fields := []string{
		"file",
		path.Path(),
		strconv.FormatUint(uint64(mode.Perm()), 8),
		strconv.FormatInt(size, 10),
	}
	if err := sink.writeRecord(sink.hasher, fields...); err != nil {
		return err
	}
	fileHasher := sha256.New()
	writers := []io.Writer{sink.hasher, fileHasher}
	if path.Path() == sink.mutableFile {
		if err := sink.writeRecord(
			sink.stableHasher,
			"mutable-file",
			path.Path(),
			strconv.FormatUint(uint64(mode.Perm()), 8),
		); err != nil {
			return err
		}
	} else {
		if err := sink.writeRecord(sink.stableHasher, fields...); err != nil {
			return err
		}
		writers = append(writers, sink.stableHasher)
	}
	_, err := io.Copy(io.MultiWriter(writers...), content)
	if err == nil {
		sink.fileFingerprints[path.Path()] = hex.EncodeToString(fileHasher.Sum(nil))
	}
	return err
}

func (sink *retirementTreeSnapshotSink) writeStableRecord(fields ...string) error {
	if err := sink.writeRecord(sink.hasher, fields...); err != nil {
		return err
	}
	return sink.writeRecord(sink.stableHasher, fields...)
}

func (sink *retirementTreeSnapshotSink) writeRecord(
	destination hash.Hash,
	fields ...string,
) error {
	if sink == nil || destination == nil {
		return fmt.Errorf("journal retirement tree sink is uninitialized")
	}
	for _, field := range fields {
		if err := binary.Write(destination, binary.BigEndian, uint64(len(field))); err != nil {
			sink.failure = errors.Join(sink.failure, err)
			return err
		}
		if _, err := io.WriteString(destination, field); err != nil {
			sink.failure = errors.Join(sink.failure, err)
			return err
		}
	}
	return nil
}

func (sink *retirementTreeSnapshotSink) evidence(
	identity mutationfs.EntryIdentity,
) (retirementTreeEvidence, error) {
	if sink == nil || sink.failure != nil {
		return retirementTreeEvidence{}, sink.failure
	}
	work, err := recovery.NewArtifactWork(sink.entries, sink.bytes)
	if err != nil {
		return retirementTreeEvidence{}, err
	}
	return retirementTreeEvidence{
		identity:          identity,
		work:              work,
		fingerprint:       hex.EncodeToString(sink.hasher.Sum(nil)),
		stableFingerprint: hex.EncodeToString(sink.stableHasher.Sum(nil)),
		fileSizes:         sink.fileSizes,
		fileFingerprints:  sink.fileFingerprints,
	}, nil
}

func requireJournalFingerprint(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	expected string,
	stateCodec durable.SnapshotCodec,
	label string,
) error {
	if authority == nil {
		return fmt.Errorf("%s record authority is required", label)
	}
	if expected == "" {
		return fmt.Errorf("%s fingerprint is required", label)
	}
	if stateCodec == nil {
		return fmt.Errorf("%s state codec is required", label)
	}
	capability, err := authority.Acquire()
	if err != nil {
		return err
	}
	content, mode, identity, readErr := filesystem.ReadRootedRegularFileUpTo(
		ctx,
		capability,
		maximumRecoveryJournalBytes,
	)
	if readErr != nil {
		return errors.Join(readErr, capability.Close())
	}
	snapshot, err := mutationfs.NewRegularFileSnapshot(content, mode, identity)
	if err != nil {
		return errors.Join(err, capability.Close())
	}
	journal, err := decodeRecoveryJournalSnapshot(
		snapshot,
		recoveryJournalFileName,
		stateCodec,
	)
	if err != nil {
		return errors.Join(err, capability.Close())
	}
	fingerprint, err := recoveryJournalAuthorityFingerprint(journal, stateCodec)
	if err != nil {
		return errors.Join(err, capability.Close())
	}
	if fingerprint != expected {
		return errors.Join(
			fmt.Errorf("%s changed before visibility transition", label),
			capability.Close(),
		)
	}
	return capability.Close()
}

type retirementControlSnapshotSink struct {
	expected      retirement.Record
	controlName   string
	tree          *retirementTreeSnapshotSink
	rootSeen      bool
	rootMode      fs.FileMode
	children      []retirement.EntryEvidence
	recordContent []byte
	failure       error
}

func (sink *retirementControlSnapshotSink) VisitRoot(mode fs.FileMode) error {
	if err := sink.tree.VisitRoot(mode); err != nil {
		return err
	}
	sink.rootSeen = true
	sink.rootMode = mode.Perm()
	return nil
}

func (sink *retirementControlSnapshotSink) VisitDirectory(
	path mutationfs.TreeRelativePath,
	mode fs.FileMode,
) error {
	if err := sink.tree.VisitDirectory(path, mode); err != nil {
		return err
	}
	return fmt.Errorf(
		"journal retirement control contains unexpected directory %q with mode %04o",
		path.Path(),
		mode.Perm(),
	)
}

func (sink *retirementControlSnapshotSink) VisitRegularFile(
	path mutationfs.TreeRelativePath,
	mode fs.FileMode,
	size int64,
	content io.Reader,
) error {
	var payload bytes.Buffer
	teed := io.TeeReader(content, &payload)
	if err := sink.tree.VisitRegularFile(path, mode, size, teed); err != nil {
		return err
	}
	if size > retirement.MaximumRecordBytes {
		return fmt.Errorf(
			"journal retirement control file %q exceeds %d bytes",
			path.Path(),
			retirement.MaximumRecordBytes,
		)
	}
	evidence, err := retirement.NewEntryEvidence(
		path.Path(),
		retirement.EntryRegular,
		mode.Perm(),
		true,
		size,
	)
	if err != nil {
		sink.failure = errors.Join(sink.failure, err)
	}
	sink.children = append(sink.children, evidence)

	if path.Path() == retirement.RecordFileName {
		sink.recordContent = payload.Bytes()
	}
	return nil
}

func (sink *retirementControlSnapshotSink) validate() error {
	if sink.failure != nil {
		return sink.failure
	}
	if !sink.rootSeen {
		return fmt.Errorf("journal retirement control root is missing")
	}
	directory, err := retirement.NewEntryEvidence(
		sink.controlName,
		retirement.EntryDirectory,
		sink.rootMode,
		true,
		0,
	)
	if err != nil {
		return err
	}
	control, err := retirement.ValidateControl(retirement.ControlEvidence{
		Directory:     directory,
		Children:      sink.children,
		RecordContent: sink.recordContent,
	})
	if err != nil {
		return err
	}
	if !control.Record().Equal(sink.expected) {
		return fmt.Errorf("journal retirement record changed")
	}
	return nil
}

func observeRetirementControl(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	expected retirement.Record,
	limits mutationfs.TreeTraversalLimits,
) (retirementTreeEvidence, error) {
	if authority == nil {
		return retirementTreeEvidence{}, fmt.Errorf("journal retirement control authority is required")
	}
	capability, err := authority.Acquire()
	if err != nil {
		return retirementTreeEvidence{}, err
	}
	sink := &retirementControlSnapshotSink{
		expected:    expected,
		controlName: expected.Identity().ControlName(),
		tree:        newRetirementTreeSnapshotSink(),
	}
	identity, snapshotErr := filesystem.SnapshotRootedDirectory(
		ctx,
		capability,
		limits,
		sink,
	)
	validationErr := sink.validate()
	evidence, evidenceErr := sink.tree.evidence(identity)
	closeErr := capability.Close()
	if snapshotErr != nil || validationErr != nil || evidenceErr != nil || closeErr != nil {
		return retirementTreeEvidence{}, errors.Join(
			snapshotErr,
			validationErr,
			evidenceErr,
			closeErr,
		)
	}
	return evidence, nil
}

func requireRetirementControl(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	expected retirement.Record,
	limits mutationfs.TreeTraversalLimits,
) error {
	_, err := observeRetirementControl(ctx, filesystem, authority, expected, limits)
	return err
}

func readRetirementRecord(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	expected retirement.Record,
) (
	rootedpath.CommitCapability,
	mutationfs.EntryIdentity,
	error,
) {
	if authority == nil {
		return nil, nil, fmt.Errorf("journal retirement record authority is required")
	}
	capability, err := authority.Acquire()
	if err != nil {
		return nil, nil, err
	}
	content, mode, identity, err := filesystem.ReadRootedRegularFileUpTo(
		ctx,
		capability,
		retirement.MaximumRecordBytes,
	)
	if err != nil {
		return nil, nil, errors.Join(err, capability.Close())
	}
	if mode != retirement.RecordMode {
		return nil, nil, errors.Join(
			fmt.Errorf(
				"journal retirement record mode is %04o, want %04o",
				mode.Perm(),
				retirement.RecordMode,
			),
			capability.Close(),
		)
	}
	observed, err := retirement.Decode(content)
	if err != nil {
		return nil, nil, errors.Join(err, capability.Close())
	}
	if !observed.Equal(expected) {
		return nil, nil, errors.Join(
			fmt.Errorf("journal retirement record changed"),
			capability.Close(),
		)
	}
	return capability, identity, nil
}

func observeRetirementPreparation(
	ctx context.Context,
	execution retirementExecution,
	bindings retirementBindings,
	budget *recovery.PhysicalWorkBudget,
	filesystem mutationfs.RootedStore,
) (retirementExecutionEvidence, error) {
	evidence := retirementExecutionEvidence{}
	var err error
	if execution.start == retirementStartActive {
		evidence.active, err = observeActiveRetirementDirectory(
			ctx,
			filesystem,
			bindings.active,
			budget,
			maximumRecoveryTreeEntries,
			maximumRecoveryTreeDepth,
			maximumRecoveryTreeBytes,
		)
		if err != nil {
			return retirementExecutionEvidence{}, fmt.Errorf("observe active recovery journal: %w", err)
		}
		if !execution.activeAuthority.matches(evidence.active.identity) {
			return retirementExecutionEvidence{}, fmt.Errorf("active recovery journal identity changed before retirement preparation")
		}
		journalBytes := evidence.active.fileSizes[recoveryJournalFileName]
		if journalBytes <= 0 || journalBytes > maximumRecoveryJournalBytes {
			return retirementExecutionEvidence{}, fmt.Errorf("active recovery journal record has invalid size")
		}
		if !journalFingerprintMatchesEvidence(execution.journalFingerprint, evidence.active) {
			return retirementExecutionEvidence{}, fmt.Errorf("active recovery journal record differs from its retirement plan")
		}
		evidence.activeLimits, err = exactRetirementTreeLimits(
			evidence.active.work,
			maximumRecoveryTreeDepth,
		)
		if err != nil {
			return retirementExecutionEvidence{}, err
		}
		evidence.activeEnvelopeLimits, err = activeRetirementEnvelopeLimits(evidence.active)
		if err != nil {
			return retirementExecutionEvidence{}, err
		}
	}

	evidence.control, evidence.controlPresent, err = observeOptionalRetirementControl(
		ctx,
		filesystem,
		bindings.control,
		execution.record,
		budget,
	)
	if err != nil {
		return retirementExecutionEvidence{}, err
	}
	if execution.start != retirementStartActive && !evidence.controlPresent {
		return retirementExecutionEvidence{}, fmt.Errorf("journal cleanup control disappeared before preparation")
	}

	wantResidue := execution.start == retirementStartPrepared ||
		execution.start == retirementStartFinalizingWithResidue
	evidence.residue, evidence.residuePresent, err = observeOptionalRetirementDirectory(
		ctx,
		filesystem,
		bindings.residue,
		budget,
	)
	if err != nil {
		return retirementExecutionEvidence{}, err
	}
	if evidence.residuePresent != wantResidue {
		if evidence.residuePresent {
			return retirementExecutionEvidence{}, fmt.Errorf("journal retirement residue appeared after cleanup planning")
		}
		return retirementExecutionEvidence{}, fmt.Errorf("journal retirement residue disappeared after cleanup planning")
	}
	if execution.start == retirementStartActive && evidence.residuePresent {
		return retirementExecutionEvidence{}, fmt.Errorf("journal retirement residue exists before active retirement")
	}
	if evidence.residuePresent {
		evidence.residueLimits, err = exactRetirementTreeLimits(
			evidence.residue.work,
			maximumRecoveryTreeDepth,
		)
		if err != nil {
			return retirementExecutionEvidence{}, err
		}
	}
	if err := requireRetirementEntryAbsent(ctx, filesystem, bindings.garbage, "journal retirement GC residue"); err != nil {
		return retirementExecutionEvidence{}, err
	}

	currentWork := evidence.control.work
	currentRecordBytes := int64(0)
	if evidence.controlPresent {
		currentRecordBytes = evidence.control.fileSizes[retirement.RecordFileName]
	} else {
		encoded, encodeErr := retirement.Encode(execution.record)
		if encodeErr != nil {
			return retirementExecutionEvidence{}, encodeErr
		}
		currentWork, err = recovery.NewArtifactWork(1, int64(len(encoded)))
		if err != nil {
			return retirementExecutionEvidence{}, err
		}
		currentRecordBytes = int64(len(encoded))
	}
	evidence.controlCurrentWork = currentWork
	evidence.controlCurrentLimits, err = exactRetirementTreeLimits(
		currentWork,
		maximumRetirementControlTreeDepth,
	)
	if err != nil {
		return retirementExecutionEvidence{}, err
	}
	finalizing, err := execution.record.Finalizing()
	if err != nil {
		return retirementExecutionEvidence{}, err
	}
	finalContent, err := retirement.Encode(finalizing)
	if err != nil {
		return retirementExecutionEvidence{}, err
	}
	finalWork, err := recovery.NewArtifactWork(
		currentWork.Entries(),
		currentWork.Bytes()-currentRecordBytes+int64(len(finalContent)),
	)
	if err != nil {
		return retirementExecutionEvidence{}, err
	}
	evidence.controlFinalWork = finalWork
	evidence.controlFinalLimits, err = exactRetirementTreeLimits(
		finalWork,
		maximumRetirementControlTreeDepth,
	)
	if err != nil {
		return retirementExecutionEvidence{}, err
	}
	return evidence, nil
}

func observeOptionalRetirementControl(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	expected retirement.Record,
	budget *recovery.PhysicalWorkBudget,
) (retirementTreeEvidence, bool, error) {
	present, err := retirementEntryPresent(ctx, filesystem, authority)
	if err != nil || !present {
		return retirementTreeEvidence{}, present, err
	}
	limits, err := retirementPreparationLimits(
		budget,
		maximumRetirementControlEntries,
		maximumRetirementControlTreeDepth,
		maximumRetirementControlTreeBytes,
	)
	if err != nil {
		return retirementTreeEvidence{}, false, err
	}
	evidence, err := observeRetirementControl(ctx, filesystem, authority, expected, limits)
	if err != nil {
		return retirementTreeEvidence{}, false, err
	}
	if err := budget.AdmitRetirementDirectoryObservation(evidence.work); err != nil {
		return retirementTreeEvidence{}, false, err
	}
	return evidence, true, nil
}

func observeOptionalRetirementDirectory(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	budget *recovery.PhysicalWorkBudget,
) (retirementTreeEvidence, bool, error) {
	present, err := retirementEntryPresent(ctx, filesystem, authority)
	if err != nil || !present {
		return retirementTreeEvidence{}, present, err
	}
	evidence, err := observeRetirementDirectory(
		ctx,
		filesystem,
		authority,
		budget,
		maximumRecoveryTreeEntries,
		maximumRecoveryTreeDepth,
		maximumRecoveryTreeBytes,
	)
	if err != nil {
		return retirementTreeEvidence{}, false, err
	}
	return evidence, true, nil
}

func observeRetirementDirectory(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	budget *recovery.PhysicalWorkBudget,
	maximumEntries int,
	maximumDepth int,
	maximumBytes int64,
) (retirementTreeEvidence, error) {
	limits, err := retirementPreparationLimits(
		budget,
		maximumEntries,
		maximumDepth,
		maximumBytes,
	)
	if err != nil {
		return retirementTreeEvidence{}, err
	}
	evidence, err := observeRetirementDirectoryWithLimits(
		ctx,
		filesystem,
		authority,
		limits,
		newRetirementTreeSnapshotSink(),
	)
	if err != nil {
		return retirementTreeEvidence{}, err
	}
	if err := budget.AdmitRetirementDirectoryObservation(evidence.work); err != nil {
		return retirementTreeEvidence{}, err
	}
	return evidence, nil
}

func observeActiveRetirementDirectory(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	budget *recovery.PhysicalWorkBudget,
	maximumEntries int,
	maximumDepth int,
	maximumBytes int64,
) (retirementTreeEvidence, error) {
	limits, err := retirementPreparationLimits(
		budget,
		maximumEntries,
		maximumDepth,
		maximumBytes,
	)
	if err != nil {
		return retirementTreeEvidence{}, err
	}
	evidence, err := observeRetirementDirectoryWithLimits(
		ctx,
		filesystem,
		authority,
		limits,
		newRetirementTreeSnapshotSinkWithMutableFile(recoveryJournalFileName),
	)
	if err != nil {
		return retirementTreeEvidence{}, err
	}
	if err := budget.AdmitRetirementDirectoryObservation(evidence.work); err != nil {
		return retirementTreeEvidence{}, err
	}
	return evidence, nil
}

func observeRetirementDirectoryWithLimits(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	limits mutationfs.TreeTraversalLimits,
	sink *retirementTreeSnapshotSink,
) (retirementTreeEvidence, error) {
	capability, err := authority.Acquire()
	if err != nil {
		return retirementTreeEvidence{}, err
	}
	identity, snapshotErr := filesystem.SnapshotRootedDirectory(ctx, capability, limits, sink)
	evidence, evidenceErr := sink.evidence(identity)
	closeErr := capability.Close()
	if snapshotErr != nil || evidenceErr != nil || closeErr != nil {
		return retirementTreeEvidence{}, errors.Join(snapshotErr, evidenceErr, closeErr)
	}
	return evidence, nil
}

func retirementPreparationLimits(
	budget *recovery.PhysicalWorkBudget,
	maximumEntries int,
	maximumDepth int,
	maximumBytes int64,
) (mutationfs.TreeTraversalLimits, error) {
	if budget == nil || budget.RemainingEntries() < 1 {
		return mutationfs.TreeTraversalLimits{}, fmt.Errorf("journal retirement observation has no entry capacity")
	}
	return mutationfs.NewTreeTraversalLimits(
		min(maximumEntries, budget.RemainingEntries()-1),
		maximumDepth,
		min(maximumBytes, budget.RemainingBytes()),
	)
}

func exactRetirementTreeLimits(
	work recovery.ArtifactWork,
	maximumDepth int,
) (mutationfs.TreeTraversalLimits, error) {
	return mutationfs.NewTreeTraversalLimits(
		work.Entries(),
		maximumDepth,
		work.Bytes(),
	)
}

func activeRetirementEnvelopeLimits(
	evidence retirementTreeEvidence,
) (mutationfs.TreeTraversalLimits, error) {
	journalBytes := evidence.fileSizes[recoveryJournalFileName]
	if journalBytes <= 0 || journalBytes > maximumRecoveryJournalBytes {
		return mutationfs.TreeTraversalLimits{}, fmt.Errorf("active recovery journal record has invalid size")
	}
	maximumBytes := evidence.work.Bytes() - journalBytes + maximumRecoveryJournalBytes
	work, err := recovery.NewArtifactWork(evidence.work.Entries(), maximumBytes)
	if err != nil {
		return mutationfs.TreeTraversalLimits{}, err
	}
	return exactRetirementTreeLimits(work, maximumRecoveryTreeDepth)
}

func journalFingerprintMatchesEvidence(
	fingerprint string,
	evidence retirementTreeEvidence,
) bool {
	return fingerprint != "" &&
		fingerprint == "sha256:"+evidence.fileFingerprint(recoveryJournalFileName)
}

func retirementEntryPresent(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
) (bool, error) {
	capability, err := authority.Acquire()
	if err != nil {
		return false, err
	}
	_, observeErr := filesystem.CaptureRootedEntryIdentity(ctx, capability)
	closeErr := capability.Close()
	if errors.Is(observeErr, fs.ErrNotExist) {
		return false, closeErr
	}
	if observeErr != nil || closeErr != nil {
		return false, errors.Join(observeErr, closeErr)
	}
	return true, nil
}
