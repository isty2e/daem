package journal

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/target"
)

type mixedJournalFixture struct {
	whole      wholeGroupTestFixture
	journal    nativeJournalSnapshots
	comparison journaldocument.Comparison
	paths      Paths
	capture    CaptureOptions
	ordinary   []byte
}

func (fixture mixedJournalFixture) stageOptions(budget *physicalwork.Budget) nativeMixedStageOptions {
	return nativeMixedStageOptions{nativeDocumentStageOptions: documentStageOptions(fixture.whole.envelope, budget), roots: fixture.whole.namespace.roots}
}

func mixedStageFixture(t *testing.T) mixedJournalFixture {
	t.Helper()
	whole := wholeGroupFixture(t, target.TargetPi, target.ScopeProject, "replace", []byte("native before\x00"), []byte("native after\xff"))
	group, comparison, err := prepareWholeDocumentGroup(t.Context(), whole.namespace, whole.input)
	if err != nil {
		t.Fatal(err)
	}
	root := whole.namespace.roots.Manifest
	authority, err := root.Root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := authority.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	journal := nativeJournalSnapshots{record: whole.envelope.record, owner: whole.input.owner, before: whole.envelope.before, after: whole.envelope.after}
	journal.record.ManifestRootProvenance, journal.record.DocumentGroups = persistedNativeRoot(provenance), []recoveryDocumentGroup{group}
	ordinary := []byte("ordinary executable\n")
	hash := artifact.HashFileContentWithExecutable(ordinary, true)
	entry := recoveryEntryFor("ordinary", "AGENTS.md", string(hash), string(hash), "unused")
	state := resourceStateWithPermissions(entry, string(hash), realization.PathPermissionsExact, 0o700)
	requireWholeGroupWrite(t, filepath.Join(root.SelectedPath, "AGENTS.md"), ordinary, 0o700)
	mutation, err := NewManagedPathRecordMutation(state.Subject(), state.ConsumerTargets(), state.Scope(), state.Destination(), hash, hash, state.ContentKind(), 0o700, &state)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := observe.NewManagedPathEvidence(state.Subject(), state.Destination(), true, hash, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	appendState := func(value durable.Snapshot) durable.Snapshot {
		result, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: append(value.ManagedPaths(), state), ManagedAggregates: value.ManagedAggregates()})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	journal.before, journal.after = appendState(journal.before), appendState(journal.after)
	paths := Paths{ManifestRoot: root.SelectedPath, StatefilePath: filepath.Join(root.SelectedPath, "state.json"), RecoveryDir: filepath.Join(root.SelectedPath, "recovery")}
	capture := CaptureOptions{ManagedPathMutations: []ManagedPathMutation{mutation}, ManagedPathEvidence: []observe.ManagedPathEvidence{evidence}, ManifestRoot: root.Root, Filesystem: journalTestFilesystem(), StateCodec: whole.envelope.codec, Codecs: whole.envelope.read.codecs, Resolver: func(destination output.Destination) (string, error) {
		return filepath.Join(root.SelectedPath, filepath.FromSlash(destination.RelativePath())), nil
	}}
	return mixedJournalFixture{whole: whole, journal: journal, comparison: comparison, paths: paths, capture: capture, ordinary: ordinary}
}

func (fixture mixedJournalFixture) source(t *testing.T, budget *physicalwork.Budget) (ordinaryCaptureSource, string) {
	t.Helper()
	directory := t.TempDir()
	createdAt, err := time.Parse(time.RFC3339, fixture.journal.record.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	source, err := fixture.journal.captureOrdinarySource(t.Context(), fixture.paths, directory, createdAt, fixture.capture, budget)
	if err != nil {
		t.Fatal(err)
	}
	return source, directory
}

func TestNativeMixedJournalStagesActualOrdinaryAndWholeProducers(t *testing.T) {
	fixture := mixedStageFixture(t)
	destination, capability := imageStageDestination(t)
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	var fingerprint string
	var order []string
	budget := imageBudget(t)
	initialBytes := budget.RemainingBytes()
	prepared, err := journalTestFilesystem().PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
		var err error
		fingerprint, err = fixture.journal.stageMixedDocumentJournal(t.Context(), fixture.paths, fixture.capture, []journaldocument.Comparison{fixture.comparison}, mixedRecordingWriter{RootedTreeWriter: writer, order: &order}, fixture.stageOptions(budget))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Abort(context.Background())
	if entries, err := os.ReadDir(temporary); err != nil || len(entries) != 0 {
		t.Fatalf("ordinary backup source survived successful staging: %v", err)
	}
	if len(order) != 4 || order[0] != "files/000001" || order[len(order)-1] != recoveryJournalFileName {
		t.Fatalf("expected ordinary backups, native images, JSON last: %v", order)
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private stage published an operation: %v", err)
	}
	if err := prepared.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(destination, recoveryJournalFileName))
	if err != nil || fingerprint == "" || recoveryJournalRecordFingerprint(content) != fingerprint {
		t.Fatalf("mixed fingerprint does not identify the final record: %v", err)
	}
	decoded, err := decodeNativeJournalRecord(t.Context(), content, fixture.whole.envelope.codec)
	if err != nil || len(decoded.record.Entries) != 1 || !decoded.before.Equal(fixture.journal.before) || !decoded.after.Equal(fixture.journal.after) {
		t.Fatalf("mixed record lost ordinary metadata or complete Snapshots: %v", err)
	}
	backup := filepath.Join(destination, filepath.FromSlash(decoded.record.Entries[0].Before.BackupPath))
	data, err := os.ReadFile(backup)
	if err != nil || string(data) != string(fixture.ordinary) {
		t.Fatalf("ordinary backup changed: %v", err)
	}
	info, err := os.Stat(backup)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("ordinary executable metadata was collapsed into image0600: %v", err)
	}
	root, err := rootedpath.CaptureRoot(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	read := fixture.whole.envelope.read
	read.artifacts, read.budget = root, imageBudget(t)
	loaded, err := decoded.loadDocuments(t.Context(), read)
	if err != nil || len(loaded.groups) != 1 || !loaded.images[0].original.document.Equal(fixture.comparison.BeforeDocument()) || !loaded.images[0].expected.document.Equal(fixture.comparison.ExpectedDocument()) {
		t.Fatalf("mixed document images cannot reconstruct: %v", err)
	}
	if initialBytes-budget.RemainingBytes() < 4*int64(len(fixture.ordinary))+int64(len(content))+fixture.comparison.BeforeDocument().ContentSize()+fixture.comparison.ExpectedDocument().ContentSize() {
		t.Fatal("backup observation, seal, revalidation or copy work escaped the shared budget")
	}
	fixture.whole.requireUnchangedHost(t)
}

type mixedRecordingWriter struct {
	mutationfs.RootedTreeWriter
	order *[]string
}

func (writer mixedRecordingWriter) WriteFile(relative mutationfs.TreeRelativePath, mode fs.FileMode, content io.Reader) error {
	*writer.order = append(*writer.order, relative.Path())
	return writer.RootedTreeWriter.WriteFile(relative, mode, content)
}

func TestNativeMixedJournalRefusesMisjoinedFactsBeforeWriter(t *testing.T) {
	for _, change := range []string{"operation", "time", "provenance", "before state", "after state", "source identity", "missing backup", "changed backup", "extra backup", "invalid native", "combined bytes", "combined entries"} {
		t.Run(change, func(t *testing.T) {
			fixture := mixedStageFixture(t)
			budget := imageBudget(t)
			source, directory := fixture.source(t, budget)
			comparisons := []journaldocument.Comparison{fixture.comparison}
			switch change {
			case "operation":
				source.record.OperationID = "other"
			case "time":
				created, err := time.Parse(time.RFC3339, source.record.CreatedAt)
				if err != nil {
					t.Fatal(err)
				}
				source.record.CreatedAt = created.Add(time.Second).Format(time.RFC3339)
			case "provenance":
				source.record.ManifestRootProvenance.ObjectFingerprint += "other"
			case "before state":
				source.record.StatefileBefore = durable.EmptySnapshot()
			case "after state":
				source.record.StatefileAfter = durable.EmptySnapshot()
			case "source identity":
				source.identity = artifact.ExactIdentity{}
			case "missing backup":
				if err := os.Remove(filepath.Join(directory, "files", "000001")); err != nil {
					t.Fatal(err)
				}
			case "changed backup":
				requireWholeGroupWrite(t, filepath.Join(directory, "files", "000001"), []byte("substituted"), 0o700)
			case "extra backup":
				requireWholeGroupWrite(t, filepath.Join(directory, "files", "other"), []byte("extra"), 0o600)
			case "invalid native":
				comparisons[0] = journaldocument.Comparison{}
			case "combined bytes":
				merged := fixture.journal
				merged.record.Entries = source.record.Entries
				_, content, err := merged.prepareDocumentJournal(t.Context(), comparisons, documentStageOptions(fixture.whole.envelope, budget), workmeasure.ArtifactWork{})
				if err != nil {
					t.Fatal(err)
				}
				nativeBytes := int64(len(content)) + fixture.comparison.BeforeDocument().ContentSize() + fixture.comparison.ExpectedDocument().ContentSize()
				work, _ := workmeasure.NewArtifactWork(0, budget.RemainingBytes()-nativeBytes-2*source.work.Bytes()+1)
				if err := budget.AdmitTree(work); err != nil {
					t.Fatal(err)
				}
			case "combined entries":
				work, _ := workmeasure.NewArtifactWork(budget.RemainingEntries()-4-2*(source.work.Entries()+1)+1, 0)
				if err := budget.AdmitTree(work); err != nil {
					t.Fatal(err)
				}
			}
			writer := &mixedNoWriteWriter{}
			fingerprint, err := fixture.journal.stageWithOrdinarySource(t.Context(), source, comparisons, writer, fixture.stageOptions(budget))
			if err == nil || fingerprint != "" || writer.directories != 0 || writer.writes != 0 {
				t.Fatalf("invalid mixed source caused final-tree writes or exposed evidence: %v", err)
			}
		})
	}
}

type mixedNoWriteWriter struct{ imageStageWriter }

func (writer *mixedNoWriteWriter) SetRootMode(fs.FileMode) error {
	writer.directories++
	return errors.New("unexpected root mode write")
}

func TestOrdinaryCaptureSourceRejectsInventoryAndContentMisjoins(t *testing.T) {
	for _, change := range []string{"missing", "extra", "wrong kind", "substitution", "unreferenced root", "duplicate reference"} {
		t.Run(change, func(t *testing.T) {
			fixture := mixedStageFixture(t)
			budget := imageBudget(t)
			source, directory := fixture.source(t, budget)
			backup := filepath.Join(directory, "files", "000001")
			switch change {
			case "missing":
				if err := os.Remove(backup); err != nil {
					t.Fatal(err)
				}
			case "extra":
				requireWholeGroupWrite(t, filepath.Join(directory, "files", "other"), nil, 0o600)
			case "wrong kind":
				if err := os.Remove(backup); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(backup, 0o700); err != nil {
					t.Fatal(err)
				}
			case "substitution":
				requireWholeGroupWrite(t, backup, []byte("substituted"), 0o700)
			case "unreferenced root":
				requireWholeGroupWrite(t, filepath.Join(directory, "journal.json"), nil, 0o600)
			case "duplicate reference":
				source.record.Entries = append(source.record.Entries, source.record.Entries[0])
			}
			if err := validateOrdinaryCaptureBackups(t.Context(), directory, source.view, source.record.Entries, fixture.capture.Filesystem, budget); err == nil {
				t.Fatal("ordinary source accepted artifacts not owned by its entry metadata")
			}
		})
	}
}

func TestNativeMixedJournalFailureCleansBothPrivateTrees(t *testing.T) {
	for _, failurePoint := range []string{"ordinary", "image", "json", "cancel"} {
		t.Run(failurePoint, func(t *testing.T) {
			fixture := mixedStageFixture(t)
			destination, capability := imageStageDestination(t)
			temporary := t.TempDir()
			t.Setenv("TMPDIR", temporary)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			failure := errors.New("injected mixed-stage failure")
			at := map[string]int{"ordinary": 1, "image": 2, "json": 4, "cancel": 4}[failurePoint]
			budget := imageBudget(t)
			before := budget.RemainingBytes()
			var fingerprint string
			_, err := journalTestFilesystem().PrepareRootedTree(ctx, capability, func(writer mutationfs.RootedTreeWriter) error {
				wrapped := &imageStageWriter{RootedTreeWriter: writer, afterWrite: func(count int) error {
					if count != at {
						return nil
					}
					if failurePoint == "cancel" {
						cancel()
						return nil
					}
					return failure
				}}
				var err error
				fingerprint, err = fixture.journal.stageMixedDocumentJournal(ctx, fixture.paths, fixture.capture, []journaldocument.Comparison{fixture.comparison}, wrapped, fixture.stageOptions(budget))
				return err
			})
			cause := failure
			if failurePoint == "cancel" {
				cause = context.Canceled
			}
			if !errors.Is(err, cause) || fingerprint != "" || before-budget.RemainingBytes() < 4*int64(len(fixture.ordinary)) {
				t.Fatalf("failed stage exposed evidence, lost cause or reset charges: %v", err)
			}
			if entries, err := os.ReadDir(temporary); err != nil || len(entries) != 0 {
				t.Fatalf("source cleanup failed: %v", err)
			}
			if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed stage published: %v", err)
			}
			if entries, err := os.ReadDir(filepath.Dir(destination)); err != nil || len(entries) != 0 {
				t.Fatalf("partial final tree survived: %v", err)
			}
		})
	}
}

func TestNativeMixedJournalCopyRejectsPostSealMutation(t *testing.T) {
	fixture := mixedStageFixture(t)
	budget := imageBudget(t)
	source, directory := fixture.source(t, budget)
	destination, capability := imageStageDestination(t)
	var fingerprint string
	_, err := journalTestFilesystem().PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
		wrapped := &mixedMutationWriter{RootedTreeWriter: writer, mutate: func() {
			requireWholeGroupWrite(t, filepath.Join(directory, "files", "000001"), []byte("changed after revalidation"), 0o700)
		}}
		var err error
		fingerprint, err = fixture.journal.stageWithOrdinarySource(t.Context(), source, []journaldocument.Comparison{fixture.comparison}, wrapped, fixture.stageOptions(budget))
		return err
	})
	if err == nil || fingerprint != "" {
		t.Fatalf("verified copy admitted changed source: %v", err)
	}
	if entries, err := os.ReadDir(filepath.Dir(destination)); err != nil || len(entries) != 0 {
		t.Fatalf("changed-source tree survived: %v", err)
	}
}

type mixedMutationWriter struct {
	mutationfs.RootedTreeWriter
	mutate func()
}

func (writer *mixedMutationWriter) SetRootMode(mode fs.FileMode) error {
	writer.mutate()
	return writer.RootedTreeWriter.SetRootMode(mode)
}

func TestNativeMixedJournalCombinesSelectedWholeAndDirectoryBackups(t *testing.T) {
	fixture := mixedStageFixture(t)
	native := nativeJournalFixture(t)
	native.claims.owner = fixture.whole.input.owner
	native.record.StateAuthority = fixture.journal.record.StateAuthority
	selected := native.after.ManagedAggregates()[0]
	original := aggregate.ExistingDocument([]byte(`{"model":true,"foreign":0}`))
	edits := selectedCaptureEdits(t, native, original, []aggregate.ProjectionContract{selected.Contribution().Contract()}, []durable.ManagedAggregateState{selected})
	capture := selectedCaptureFixture(t, native, original, edits, nil, fixture.whole.namespace.roots)
	group, comparison, err := prepareSelectedDocumentGroup(t.Context(), capture.namespace, capture.input, capture.subjects)
	if err != nil {
		t.Fatal(err)
	}
	fixture.journal.record.DocumentGroups = append(fixture.journal.record.DocumentGroups, group)
	root := fixture.paths.ManifestRoot
	directory := filepath.Join(root, ".agents", "skills", "directory")
	requireWholeGroupWrite(t, filepath.Join(directory, "nested", "run"), []byte("executable directory backup\n"), 0o700)
	hash, _, err := access.HashPath(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse(".agents/skills/directory")
	if err != nil {
		t.Fatal(err)
	}
	state, err := durable.NewManagedPathState(testManagedPathSubject(t, "directory"), []target.Target{target.TargetCodex}, target.ScopeProject, destination, hash, realization.PathProjectionDirectory, realization.PathPermissionsNone, 0)
	if err != nil {
		t.Fatal(err)
	}
	mutation, err := NewManagedPathRecordMutation(state.Subject(), state.ConsumerTargets(), state.Scope(), state.Destination(), hash, hash, state.ContentKind(), 0, &state)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := observe.NewManagedPathEvidence(state.Subject(), state.Destination(), true, hash, 0)
	if err != nil {
		t.Fatal(err)
	}
	fixture.capture.ManagedPathMutations = append(fixture.capture.ManagedPathMutations, mutation)
	fixture.capture.ManagedPathEvidence = append(fixture.capture.ManagedPathEvidence, evidence)
	for side, value := range []durable.Snapshot{fixture.journal.before, fixture.journal.after} {
		aggregates := native.before.ManagedAggregates()
		if side == 1 {
			aggregates = native.after.ManagedAggregates()
		}
		combined, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: append(value.ManagedPaths(), state), ManagedAggregates: append(value.ManagedAggregates(), aggregates...)})
		if err != nil {
			t.Fatal(err)
		}
		if side == 0 {
			fixture.journal.before = combined
		} else {
			fixture.journal.after = combined
		}
	}
	final, capability := imageStageDestination(t)
	var fingerprint string
	prepared, err := journalTestFilesystem().PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
		var err error
		fingerprint, err = fixture.journal.stageMixedDocumentJournal(t.Context(), fixture.paths, fixture.capture, []journaldocument.Comparison{fixture.comparison, comparison}, writer, fixture.stageOptions(imageBudget(t)))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Abort(context.Background())
	if err := prepared.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(final, recoveryJournalFileName))
	if err != nil || fingerprint == "" {
		t.Fatalf("missing mixed record: %v", err)
	}
	decoded, err := decodeNativeJournalRecord(t.Context(), content, fixture.whole.envelope.codec)
	if err != nil || len(decoded.record.Entries) != 2 || len(decoded.record.DocumentGroups) != 2 {
		t.Fatalf("lost mixed work: %v", err)
	}
	for _, entry := range decoded.record.Entries {
		if entry.Before.Kind != recovery.PathKindDirectory {
			continue
		}
		backup := filepath.Join(final, filepath.FromSlash(entry.Before.BackupPath))
		actual, kind, err := access.HashPath(t.Context(), backup)
		if err != nil || actual != hash || kind != artifact.ArtifactKindDirectory {
			t.Fatalf("directory backup lost identity: %v", err)
		}
		info, err := os.Stat(filepath.Join(backup, "nested", "run"))
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("nested executable lost permissions: %v", err)
		}
	}
	artifacts, err := rootedpath.CaptureRoot(final)
	if err != nil {
		t.Fatal(err)
	}
	defer artifacts.Close()
	read := fixture.whole.envelope.read
	read.artifacts, read.budget = artifacts, imageBudget(t)
	loaded, err := decoded.loadDocuments(t.Context(), read)
	if err != nil || len(loaded.groups) != 2 || !loaded.images[1].original.document.Equal(comparison.BeforeDocument()) || !loaded.images[1].expected.document.Equal(comparison.ExpectedDocument()) {
		t.Fatalf("multiple mixed groups cannot reconstruct: %v", err)
	}
}

func TestNativeMixedJournalCleanupFailureClearsSuccessfulFingerprint(t *testing.T) {
	fixture := mixedStageFixture(t)
	destination, capability := imageStageDestination(t)
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	var fingerprint string
	_, err := journalTestFilesystem().PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
		wrapped := &imageStageWriter{RootedTreeWriter: writer, afterWrite: func(count int) error {
			if count != 4 {
				return nil
			}
			entries, err := os.ReadDir(temporary)
			if err != nil || len(entries) != 1 {
				t.Fatalf("missing owned ordinary source: %v", err)
			}
			link := filepath.Join(temporary, entries[0].Name(), "unexpected-link")
			if err := os.Symlink("outside", link); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Remove(link) })
			return nil
		}}
		var err error
		fingerprint, err = fixture.journal.stageMixedDocumentJournal(t.Context(), fixture.paths, fixture.capture, []journaldocument.Comparison{fixture.comparison}, wrapped, fixture.stageOptions(imageBudget(t)))
		return err
	})
	if err == nil || fingerprint != "" {
		t.Fatalf("failed source cleanup exposed successful evidence: %v", err)
	}
	if entries, err := os.ReadDir(filepath.Dir(destination)); err != nil || len(entries) != 0 {
		t.Fatalf("cleanup failure retained final staged tree: %v", err)
	}
}

func TestNativeMixedJournalAllowsEmptyOrdinaryBackupSource(t *testing.T) {
	fixture := mixedStageFixture(t)
	fixture.capture.ManagedPathMutations, fixture.capture.ManagedPathEvidence = nil, nil
	destination, capability := imageStageDestination(t)
	var fingerprint string
	prepared, err := journalTestFilesystem().PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
		var err error
		fingerprint, err = fixture.journal.stageMixedDocumentJournal(t.Context(), fixture.paths, fixture.capture, []journaldocument.Comparison{fixture.comparison}, writer, fixture.stageOptions(imageBudget(t)))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Abort(context.Background())
	if err := prepared.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(destination, recoveryJournalFileName))
	if err != nil || fingerprint == "" {
		t.Fatalf("empty ordinary source lost native work: %v", err)
	}
	decoded, err := decodeNativeJournalRecord(t.Context(), content, fixture.whole.envelope.codec)
	if err != nil || len(decoded.record.Entries) != 0 || !decoded.before.Equal(fixture.journal.before) || !decoded.after.Equal(fixture.journal.after) {
		t.Fatalf("empty ordinary source changed full evidence: %v", err)
	}
}

func TestNativeMixedJournalExhaustedSourceBudgetCleansWithoutFinalWrites(t *testing.T) {
	for _, remaining := range []int{0, 1, 2} {
		t.Run(string(rune('0'+remaining)), func(t *testing.T) {
			fixture := mixedStageFixture(t)
			temporary := t.TempDir()
			t.Setenv("TMPDIR", temporary)
			budget := imageBudget(t)
			consume, err := workmeasure.NewArtifactWork(budget.RemainingEntries()-remaining, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err := budget.AdmitTree(consume); err != nil {
				t.Fatal(err)
			}
			writer := &mixedNoWriteWriter{}
			fingerprint, err := fixture.journal.stageMixedDocumentJournal(t.Context(), fixture.paths, fixture.capture, []journaldocument.Comparison{fixture.comparison}, writer, fixture.stageOptions(budget))
			if err == nil || fingerprint != "" || writer.writes != 0 || writer.directories != 0 {
				t.Fatalf("exhausted source budget escaped: %v", err)
			}
			if entries, err := os.ReadDir(temporary); err != nil || len(entries) != 0 {
				t.Fatalf("failed source capture survived: %v", err)
			}
		})
	}
}
