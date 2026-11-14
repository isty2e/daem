package publication

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/effect/journal/namespace/namespacetest"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/workmeasure"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeMixedJournalCombinesSelectedWholeAndDirectoryBackups(t *testing.T) {
	fixture := mixedStageFixture(t)
	native := nativeJournalFixture(t)
	native.claims.owner = fixture.whole.input.Owner()
	selected := native.after.ManagedAggregates()[0]
	original := aggregate.ExistingDocument([]byte(`{"model":true,"foreign":0}`))
	edits := selectedCaptureEdits(t, native, original, []aggregate.ProjectionContract{selected.Contribution().Contract()}, []durable.ManagedAggregateState{selected})
	capture := selectedCaptureFixture(t, native, original, edits, nil, fixture.whole.roots)
	group, comparison, err := prepareSelectedDocumentGroup(t.Context(), capture.namespace, capture.input, capture.subjects)
	if err != nil {
		t.Fatal(err)
	}
	fixture.groups = append(fixture.groups, group)
	fixture.expected = append(fixture.expected, capture.namespace)
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
	mutation, err := journalrecord.NewManagedPathRecordMutation(state.Subject(), state.ConsumerTargets(), state.Scope(), state.Destination(), hash, hash, state.ContentKind(), 0, &state)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := observe.NewManagedPathEvidence(state.Subject(), state.Destination(), true, hash, 0)
	if err != nil {
		t.Fatal(err)
	}
	fixture.capture.ManagedPathMutations = append(fixture.capture.ManagedPathMutations, mutation)
	fixture.capture.ManagedPathEvidence = append(fixture.capture.ManagedPathEvidence, evidence)
	combined := fixture.whole
	for side, value := range []durable.Snapshot{fixture.journal.Before(), fixture.journal.After()} {
		aggregates := native.before.ManagedAggregates()
		if side == 1 {
			aggregates = native.after.ManagedAggregates()
		}
		snapshot, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: append(value.ManagedPaths(), state), ManagedAggregates: append(value.ManagedAggregates(), aggregates...)})
		if err != nil {
			t.Fatal(err)
		}
		if side == 0 {
			combined.envelope.before = snapshot
		} else {
			combined.envelope.after = snapshot
		}
	}
	fixture.journal = combined.recordForGroups(t, fixture.groups)
	final, capability := imageStageDestination(t)
	defer capability.Close()
	var fingerprint string
	prepared, err := journalTestFilesystem().PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
		var err error
		fingerprint, err = stageMixedDocumentJournal(fixture.journal, t.Context(), fixture.paths, fixture.capture, []journaldocument.Comparison{fixture.comparison, comparison}, writer, fixture.stageOptions(imageBudget(t)))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Abort(context.Background())
	if err := prepared.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(final, journalrecord.FileName))
	if err != nil || fingerprint == "" || journalrecord.Fingerprint(content) != fingerprint {
		t.Fatalf("missing mixed record: %v", err)
	}
	decoded := readStagedNativeRecord(t, filepath.Join(final, journalrecord.FileName), fixture.whole.envelope.codec)
	entries, err := decoded.RecordedEntries()
	if err != nil || len(entries) != 2 {
		t.Fatalf("lost ordinary mixed work: %v", err)
	}
	documents, err := decoded.Documents()
	if err != nil || len(documents) != 2 {
		t.Fatalf("lost native mixed work: %v", err)
	}
	directories := 0
	for _, entry := range entries {
		reference := entry.Backup()
		if reference.Kind() != recovery.PathKindDirectory {
			continue
		}
		directories++
		backup := filepath.Join(final, filepath.FromSlash(reference.Path()))
		actual, kind, err := access.HashPath(t.Context(), backup)
		if err != nil || actual != hash || kind != artifact.ArtifactKindDirectory {
			t.Fatalf("directory backup lost identity: %v", err)
		}
		info, err := os.Stat(filepath.Join(backup, "nested", "run"))
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("nested executable lost permissions: %v", err)
		}
	}
	if directories != 1 {
		t.Fatal("mixed record did not retain the directory backup")
	}
	artifacts, err := rootedpath.CaptureRoot(final)
	if err != nil {
		t.Fatal(err)
	}
	defer artifacts.Close()
	read := fixture.whole.envelope.read
	read.Artifacts, read.Budget = artifacts, imageBudget(t)
	loaded, err := journalqualification.LoadDocuments(decoded, t.Context(), read)
	if err != nil || loaded.Len() != 2 || !requireLoadedNativeDocument(t, loaded, 1).Images().Original().Document().Equal(comparison.BeforeDocument()) || !requireLoadedNativeDocument(t, loaded, 1).Images().Expected().Document().Equal(comparison.ExpectedDocument()) {
		t.Fatalf("multiple mixed groups cannot reconstruct: %v", err)
	}
}

func TestMixedStageFixtureKeepsNativeBaselineIndependent(t *testing.T) {
	fixture := mixedStageFixture(t)
	ordinary, present := fixture.capture.ManagedPathMutations[0].PreviousState()
	if !present {
		t.Fatal("record fixture lost its ordinary baseline")
	}
	for _, side := range []struct {
		name     string
		baseline durable.Snapshot
		combined durable.Snapshot
	}{
		{"before", fixture.whole.envelope.before, fixture.journal.Before()},
		{"after", fixture.whole.envelope.after, fixture.journal.After()},
	} {
		paths := append(side.baseline.ManagedPaths(), ordinary)
		rebuilt, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: paths, ManagedAggregates: side.baseline.ManagedAggregates()})
		if err != nil || !rebuilt.Equal(side.combined) {
			t.Fatalf("%s native baseline cannot be independently extended with the selected ordinary row: %v", side.name, err)
		}
	}
}

func TestNativeMixedJournalStagesActualOrdinaryAndWholeProducers(t *testing.T) {
	fixture := mixedStageFixture(t)
	destination, capability := imageStageDestination(t)
	defer capability.Close()
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	var fingerprint string
	var order []string
	budget := imageBudget(t)
	initialBytes := budget.RemainingBytes()
	prepared, err := journalTestFilesystem().PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
		var err error
		fingerprint, err = stageMixedDocumentJournal(fixture.journal, t.Context(), fixture.paths, fixture.capture, []journaldocument.Comparison{fixture.comparison}, mixedRecordingWriter{RootedTreeWriter: writer, order: &order}, fixture.stageOptions(budget))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Abort(context.Background())
	if entries, err := os.ReadDir(temporary); err != nil || len(entries) != 0 {
		t.Fatalf("ordinary backup source survived successful staging: %v", err)
	}
	if len(order) != 4 || order[0] != "files/000001" || order[len(order)-1] != journalrecord.FileName {
		t.Fatalf("expected ordinary backups, native images, JSON last: %v", order)
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private stage published an operation: %v", err)
	}
	if err := prepared.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(destination, journalrecord.FileName))
	if err != nil || fingerprint == "" || journalrecord.Fingerprint(content) != fingerprint {
		t.Fatalf("mixed fingerprint does not identify the final record: %v", err)
	}
	decoded := readStagedNativeRecord(t, filepath.Join(destination, journalrecord.FileName), fixture.whole.envelope.codec)
	entries, err := decoded.RecordedEntries()
	if err != nil || len(entries) != 1 || !decoded.Before().Equal(fixture.journal.Before()) || !decoded.After().Equal(fixture.journal.After()) {
		t.Fatalf("mixed record lost ordinary metadata or complete Snapshots: %v", err)
	}
	backup := filepath.Join(destination, filepath.FromSlash(entries[0].Backup().Path()))
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
	read.Artifacts, read.Budget = root, imageBudget(t)
	loaded, err := journalqualification.LoadDocuments(decoded, t.Context(), read)
	if err != nil || loaded.Len() != 1 || !requireLoadedNativeDocument(t, loaded, 0).Images().Original().Document().Equal(fixture.comparison.BeforeDocument()) || !requireLoadedNativeDocument(t, loaded, 0).Images().Expected().Document().Equal(fixture.comparison.ExpectedDocument()) {
		t.Fatalf("mixed document images cannot reconstruct: %v", err)
	}
	if initialBytes-budget.RemainingBytes() < 4*int64(len(fixture.ordinary))+int64(len(content))+fixture.comparison.BeforeDocument().ContentSize()+fixture.comparison.ExpectedDocument().ContentSize() {
		t.Fatal("backup observation, seal, revalidation or copy work escaped the shared budget")
	}
	fixture.whole.requireUnchangedHost(t)
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
			}
			entries, err := source.record.RecordedEntries()
			if err != nil {
				t.Fatal(err)
			}
			if change == "duplicate reference" {
				entries = append(entries, entries[0])
			}
			if err := validateOrdinaryCaptureBackups(t.Context(), directory, source.view, entries, fixture.capture.Filesystem, budget); err == nil {
				t.Fatal("ordinary source accepted artifacts not owned by its entry metadata")
			}
		})
	}
}

func TestNativeMixedJournalAllowsEmptyOrdinaryBackupSource(t *testing.T) {
	fixture := mixedStageFixture(t)
	fixture.capture.ManagedPathMutations, fixture.capture.ManagedPathEvidence = nil, nil
	destination, capability := imageStageDestination(t)
	var fingerprint string
	prepared, err := journalTestFilesystem().PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
		var err error
		fingerprint, err = stageMixedDocumentJournal(fixture.journal, t.Context(), fixture.paths, fixture.capture, []journaldocument.Comparison{fixture.comparison}, writer, fixture.stageOptions(imageBudget(t)))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Abort(context.Background())
	if err := prepared.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(destination, journalrecord.FileName))
	if err != nil || fingerprint == "" || journalrecord.Fingerprint(content) != fingerprint {
		t.Fatalf("empty ordinary source lost native work: %v", err)
	}
	decoded := readStagedNativeRecord(t, filepath.Join(destination, journalrecord.FileName), fixture.whole.envelope.codec)
	entries, err := decoded.RecordedEntries()
	if err != nil || len(entries) != 0 || !decoded.Before().Equal(fixture.journal.Before()) || !decoded.After().Equal(fixture.journal.After()) {
		t.Fatalf("empty ordinary source changed full evidence: %v", err)
	}
}

func TestNativeMixedJournalRefusesMisjoinedFactsBeforeWriter(t *testing.T) {
	for _, change := range []string{"operation", "time", "provenance", "before state", "after state", "source identity", "missing backup", "changed backup", "extra backup", "invalid native", "combined bytes", "combined entries"} {
		t.Run(change, func(t *testing.T) {
			fixture := mixedStageFixture(t)
			budget := imageBudget(t)
			source, directory := fixture.source(t, budget)
			journal := fixture.journal
			comparisons := []journaldocument.Comparison{fixture.comparison}
			switch change {
			case "operation", "time", "provenance", "before state", "after state":
				basis, err := fixture.journal.OrdinaryCaptureBasis()
				if err != nil {
					t.Fatal(err)
				}
				authority, err := namespacetest.LegacyRoot(t, fixture.whole.roots, output.RootProject).Root.Authority()
				if err != nil {
					t.Fatal(err)
				}
				manifest, err := authority.Provenance()
				if err != nil {
					t.Fatal(err)
				}
				operationID, created, before, after := basis.OperationID(), basis.CreatedAt(), basis.Before(), basis.After()
				groups := []journalrecord.SealedDocumentGroup(nil)
				control, err := journalrecord.NewNativeCapture(operationID, created, fixture.whole.input.Owner(), manifest, before, after, groups)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := control.WithOrdinaryEvidence(source.record); err != nil {
					t.Fatal("frozen header-only control did not admit matching ordinary evidence", err)
				}
				switch change {
				case "operation":
					operationID = "other"
				case "time":
					created = created.Add(time.Second)
				case "provenance":
					// Isolate the header join from contradictory project-alias facts.
					other := nativeNamespaceRoot(t, t.TempDir())
					otherAuthority, err := other.Root.Authority()
					if err != nil {
						t.Fatal(err)
					}
					otherProvenance, err := otherAuthority.Provenance()
					if err != nil {
						t.Fatal(err)
					}
					if manifest.ObjectFingerprint() == otherProvenance.ObjectFingerprint() {
						t.Fatal("foreign root fixture reused the captured object fingerprint")
					}
					manifest, err = rootedpath.NewAuthorityProvenance(manifest.PhysicalRoot(), otherProvenance.ObjectFingerprint(), manifest.MountFingerprint())
					if err != nil {
						t.Fatal(err)
					}
				case "before state":
					before = durable.EmptySnapshot()
				case "after state":
					after = durable.EmptySnapshot()
				}
				journal, err = journalrecord.NewNativeCapture(operationID, created, fixture.whole.input.Owner(), manifest, before, after, groups)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := journal.WithOrdinaryEvidence(source.record); err == nil {
					t.Fatal("foreign capture header passed the ordinary evidence join")
				}
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
				merged, err := fixture.journal.WithOrdinaryEvidence(source.record)
				if err != nil {
					t.Fatal(err)
				}
				prepared, err := merged.PrepareDocuments(t.Context(), comparisons, fixture.whole.envelope.read.Limits, fixture.whole.envelope.read.Codecs)
				if err != nil {
					t.Fatal(err)
				}
				content, err := prepared.Encode(t.Context(), fixture.whole.envelope.codec)
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
			fingerprint, err := stageWithOrdinarySource(journal, t.Context(), source, comparisons, writer, fixture.stageOptions(budget))
			if err == nil || fingerprint != "" || writer.directories != 0 || writer.writes != 0 {
				t.Fatalf("invalid mixed source caused final-tree writes or exposed evidence: %v", err)
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
				fingerprint, err = stageMixedDocumentJournal(fixture.journal, ctx, fixture.paths, fixture.capture, []journaldocument.Comparison{fixture.comparison}, wrapped, fixture.stageOptions(budget))
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
		fingerprint, err = stageWithOrdinarySource(fixture.journal, t.Context(), source, []journaldocument.Comparison{fixture.comparison}, wrapped, fixture.stageOptions(budget))
		return err
	})
	if err == nil || fingerprint != "" {
		t.Fatalf("verified copy admitted changed source: %v", err)
	}
	if entries, err := os.ReadDir(filepath.Dir(destination)); err != nil || len(entries) != 0 {
		t.Fatalf("changed-source tree survived: %v", err)
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
		fingerprint, err = stageMixedDocumentJournal(fixture.journal, t.Context(), fixture.paths, fixture.capture, []journaldocument.Comparison{fixture.comparison}, wrapped, fixture.stageOptions(imageBudget(t)))
		return err
	})
	if err == nil || fingerprint != "" {
		t.Fatalf("failed source cleanup exposed successful evidence: %v", err)
	}
	if entries, err := os.ReadDir(filepath.Dir(destination)); err != nil || len(entries) != 0 {
		t.Fatalf("cleanup failure retained final staged tree: %v", err)
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
			fingerprint, err := stageMixedDocumentJournal(fixture.journal, t.Context(), fixture.paths, fixture.capture, []journaldocument.Comparison{fixture.comparison}, writer, fixture.stageOptions(budget))
			if err == nil || fingerprint != "" || writer.writes != 0 || writer.directories != 0 {
				t.Fatalf("exhausted source budget escaped: %v", err)
			}
			if entries, err := os.ReadDir(temporary); err != nil || len(entries) != 0 {
				t.Fatalf("failed source capture survived: %v", err)
			}
		})
	}
}

type mixedRecordingWriter struct {
	mutationfs.RootedTreeWriter
	order *[]string
}

type mixedNoWriteWriter struct{ imageStageWriter }

type mixedMutationWriter struct {
	mutationfs.RootedTreeWriter
	mutate func()
}

func (writer mixedRecordingWriter) WriteFile(relative mutationfs.TreeRelativePath, mode fs.FileMode, content io.Reader) error {
	*writer.order = append(*writer.order, relative.Path())
	return writer.RootedTreeWriter.WriteFile(relative, mode, content)
}

func (writer *mixedNoWriteWriter) SetRootMode(fs.FileMode) error {
	writer.directories++
	return errors.New("unexpected root mode write")
}

func (writer *mixedMutationWriter) SetRootMode(mode fs.FileMode) error {
	writer.mutate()
	return writer.RootedTreeWriter.SetRootMode(mode)
}
