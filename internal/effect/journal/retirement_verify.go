package journal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

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
	rootSeen      bool
	rootMode      fs.FileMode
	children      []retirement.EntryEvidence
	recordContent []byte
	failure       error
}

func (sink *retirementControlSnapshotSink) VisitRoot(mode fs.FileMode) error {
	sink.rootSeen = true
	sink.rootMode = mode.Perm()
	return nil
}

func (sink *retirementControlSnapshotSink) VisitDirectory(
	path mutationfs.TreeRelativePath,
	mode fs.FileMode,
) error {
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
		limited := io.LimitReader(content, retirement.MaximumRecordBytes+1)
		value, readErr := io.ReadAll(limited)
		if readErr != nil {
			return readErr
		}
		sink.recordContent = value
	}
	if _, err := io.Copy(io.Discard, content); err != nil {
		return err
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

func requireRetirementControl(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	expected retirement.Record,
) error {
	if authority == nil {
		return fmt.Errorf("journal retirement control authority is required")
	}
	capability, err := authority.Acquire()
	if err != nil {
		return err
	}
	sink := &retirementControlSnapshotSink{
		expected:    expected,
		controlName: expected.Identity().ControlName(),
	}
	_, snapshotErr := filesystem.SnapshotRootedDirectory(
		ctx,
		capability,
		retirementControlTraversalLimits(),
		sink,
	)
	return errors.Join(snapshotErr, sink.validate(), capability.Close())
}

func requireRetirementResidueTree(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
) error {
	if authority == nil {
		return fmt.Errorf("journal retirement residue authority is required")
	}
	capability, err := authority.Acquire()
	if err != nil {
		return err
	}
	_, validateErr := filesystem.ValidateRootedDirectoryTree(
		ctx,
		capability,
		recoveryTreeTraversalLimits(),
	)
	return errors.Join(validateErr, capability.Close())
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
