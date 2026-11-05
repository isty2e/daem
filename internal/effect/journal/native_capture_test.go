package journal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

type nativeCaptureTestInput struct {
	paths         Paths
	batch         NativeDocumentObservationBatch
	after         durable.Snapshot
	edits         []compose.ProjectionEdit
	options       NativeCaptureOptions
	host          string
	registry      string
	original      aggregate.Document
	stateBytes    []byte
	registryBytes []byte
}

func nativeCaptureInput(t *testing.T, create bool, scope target.Scope) nativeCaptureTestInput {
	t.Helper()
	fixture := nativeJournalFixture(t)
	rootPath := t.TempDir()
	rootPath, err := filepath.EvalSymlinks(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	paths := Paths{ManifestRoot: rootPath, StatefilePath: filepath.Join(rootPath, "state.json"), RecoveryDir: filepath.Join(rootPath, "recovery")}
	manifest := filepath.Join(rootPath, "daem.toml")
	requireWholeGroupWrite(t, manifest, []byte("version=1\ntargets=['codex']\n"), 0o600)
	nativePath := "config.toml"
	if scope == target.ScopeGlobal {
		nativePath = "extension-settings/preferences.toml"
	}
	placement, err := profile.ResolveHostConfigPlacement(profile.HostConfigPlacementInput{Target: target.TargetCodex, Scope: scope, Path: nativePath, Ownership: hostconfig.OwnershipKeys})
	if err != nil {
		t.Fatal(err)
	}
	destination := placement.Destination()
	alias, err := aggregate.NewDocumentAddress(target.TargetCodex, scope, destination)
	if err != nil {
		t.Fatal(err)
	}
	before := membershipNativeRow(t, alias, "native-toml-v1", "model")
	before = selectionNativeRow(t, before.Contribution().Contract(), nativeRecordCanonical(t, "true"))
	after := selectionNativeRow(t, before.Contribution().Contract(), nativeRecordCanonical(t, `"fast"`))
	fixture.before, fixture.after = selectionState(t, before), selectionState(t, after)
	original := aggregate.ExistingDocument([]byte("model=true\n# user comment\nunmanaged=1\n"))
	if create {
		fixture.before, original = durable.EmptySnapshot(), aggregate.AbsentDocument()
	}
	fixture.read.codecs, err = aggregatecodec.ForOperation(t.Context(), []aggregate.CodecContractID{after.Contribution().CodecContractID()}, fixture.read.limits, fixture.read.limits)
	if err != nil {
		t.Fatal(err)
	}
	fixture.codec, err = statefile.NewNativeCodec(fixture.read.codecs.Contributions())
	if err != nil {
		t.Fatal(err)
	}
	stateBytes, err := fixture.codec.Encode(fixture.before)
	if err != nil {
		t.Fatal(err)
	}
	requireWholeGroupWrite(t, paths.StatefilePath, stateBytes, 0o600)
	host := filepath.Join(rootPath, ".codex", "config.toml")
	roots := journalnamespace.Roots{Manifest: nativeNamespaceRoot(t, rootPath)}
	if scope == target.ScopeGlobal {
		roots.CodexHome = nativeNamespaceRoot(t, filepath.Join(rootPath, "codex-home"))
		host = filepath.Join(roots.CodexHome.SelectedPath, filepath.FromSlash(destination.RelativePath()))
	}
	if original.Exists() {
		requireWholeGroupWrite(t, host, original.Content(), 0o644)
	}
	budget := physicalwork.NewCaptureBudget(nil)
	actualBefore, err := statefile.LoadOptionalWithOptions(t.Context(), paths.StatefilePath, statefile.ReadOptions{Codec: fixture.codec, PhysicalPathBudget: budget})
	if err != nil {
		t.Fatal(err)
	}
	stateAuthority, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(paths.StatefilePath, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(stateAuthority.Exact(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	var bindings []ownership.DocumentBinding
	var claims []ownership.Claim
	if original.Exists() {
		namespace, err := journalnamespace.Observe(t.Context(), []aggregate.DocumentAddress{alias}, roots, journalTestFilesystem(), budget)
		if err != nil {
			t.Fatal(err)
		}
		exact, _ := namespace.Observation().Exact()
		binding, err := ownership.NewDocumentBinding(owner, destination, exact)
		if err != nil {
			t.Fatal(err)
		}
		bindings = append(bindings, binding)
		if scope == target.ScopeGlobal {
			address, err := ownership.NewManagedAddress(exact, before.Contribution().Contract().Address().Footprint())
			if err != nil {
				t.Fatal(err)
			}
			claim, err := ownership.NewActiveClaim(address, owner)
			if err != nil {
				t.Fatal(err)
			}
			claims = append(claims, claim)
		}
	}
	registry, err := ownership.NewRegistry(claims, bindings...)
	if err != nil {
		t.Fatal(err)
	}
	registryBytes, err := ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(rootPath, "ownership.json")
	requireWholeGroupWrite(t, registryPath, registryBytes, 0o600)
	reader, err := ownershipstore.NewRecoveryReader(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := ObserveNativeDocuments(t.Context(), journalobservation.Input{Owner: owner, Before: actualBefore, Aliases: []aggregate.DocumentAddress{alias}, Roots: roots, Filesystem: journalTestFilesystem(), Registry: reader, Limits: fixture.read.limits, Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := rootedpath.BindSelectedEntryAuthorityBounded(roots.Manifest.Root, rootPath, filepath.Join(paths.RecoveryDir, fixture.journal.OperationID()), physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = entry.Close() })
	return nativeCaptureTestInput{
		paths: paths, batch: batch, after: fixture.after,
		edits:   selectedCaptureEdits(t, fixture, original, []aggregate.ProjectionContract{after.Contribution().Contract()}, []durable.ManagedAggregateState{after}),
		options: NativeCaptureOptions{CaptureOptions: CaptureOptions{Filesystem: journalTestFilesystem(), StateCodec: fixture.codec, Codecs: fixture.read.codecs, ManifestRoot: roots.Manifest.Root, OperationAuthority: entry}},
		host:    host, registry: registryPath, original: original, stateBytes: stateBytes, registryBytes: registryBytes,
	}
}

func (input nativeCaptureTestInput) capture(ctx context.Context) (CapturedNativeJournal, error) {
	return CaptureNativeJournalWithOptions(ctx, input.paths, "native-record-apply", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), input.batch, input.after, input.edits, input.options)
}

func (input nativeCaptureTestInput) requireUnchanged(t *testing.T) {
	t.Helper()
	content, err := os.ReadFile(input.host)
	if input.original.Exists() {
		if err != nil || !bytes.Equal(content, input.original.Content()) {
			t.Fatalf("capture changed host content: %q %v", content, err)
		}
		info, err := os.Stat(input.host)
		if err != nil || info.Mode().Perm() != 0o644 {
			t.Fatalf("capture changed host mode: %v", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("capture created a host document: %v", err)
	}
	for path, expected := range map[string][]byte{input.paths.StatefilePath: input.stateBytes, input.registry: input.registryBytes} {
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("capture changed durable state %q: %v", path, err)
		}
	}
}

func TestCaptureNativeJournalPublishesAndReloadsActualSelectedWork(t *testing.T) {
	for _, create := range []bool{true, false} {
		t.Run(fmt.Sprint("create=", create), func(t *testing.T) {
			input := nativeCaptureInput(t, create, target.ScopeProject)
			captured, err := input.capture(t.Context())
			if err != nil || captured.Validate() != nil || len(captured.Documents()) != 1 {
				t.Fatalf("capture did not publish/reload a complete native journal: %v", err)
			}
			if !captured.BeforeState().Equal(input.batch.Evidence().BeforeState()) || !captured.AfterState().Equal(input.after) || !captured.Owner().Equal(input.batch.owner) {
				t.Fatal("capture lost complete operation facts")
			}
			snapshot, err := input.options.Filesystem.ReadRegularFileSnapshotUpTo(t.Context(), captured.CaptureResult().JournalPath, journalrecord.MaximumBytes)
			if err != nil {
				t.Fatal(err)
			}
			content := snapshot.Content()
			if journalrecord.Fingerprint(content) != captured.CaptureResult().RecordFingerprint {
				t.Fatal("capture fingerprint was not actual published bytes")
			}
			if _, err := journalrecord.DecodeNativeSnapshot(t.Context(), snapshot, captured.CaptureResult().JournalPath, input.options.StateCodec); err != nil {
				t.Fatal(err)
			}
			if _, err := journalrecord.DecodeOrdinarySnapshot(t.Context(), snapshot, captured.CaptureResult().JournalPath, input.options.StateCodec); err == nil {
				t.Fatal("default journal15 reader admitted native16")
			}
			document := captured.Documents()[0]
			original, mode := document.Original()
			expected, expectedMode := document.Expected()
			if !original.Equal(input.original) || !expected.Exists() || expectedMode == nil || (create && mode != nil) || (!create && (mode == nil || mode.FileMode() != 0o644 || expectedMode.FileMode() != 0o644)) {
				t.Fatal("capture lost actual images or host mode policy")
			}
			bindings := document.BindingIntents()
			if len(bindings) != 1 || len(document.ProvisionalAcquires()) != 0 {
				t.Fatal("project capture lost binding or invented exclusive claims")
			}
			if _, exact := document.Claims(); exact {
				t.Fatal("project capture invented an exclusive claim")
			}
			if subjects, err := document.SelectedSubjects(t.Context()); err != nil || len(subjects) != 1 {
				t.Fatal("capture lost actual selected subjects", err)
			}
			if err := captured.ValidateActive(t.Context(), input.options.Filesystem, input.options.OperationAuthority, input.batch.budget); err != nil {
				t.Fatal("published authority could not be revalidated", err)
			}
			if err := captured.ValidateActive(t.Context(), input.options.Filesystem, input.options.OperationAuthority, physicalwork.NewCaptureBudget(nil)); err == nil {
				t.Fatal("active validation accepted a replacement allocation")
			}
			capability, err := document.Namespace().AcquireReferent(t.Context(), input.batch.budget)
			if err != nil {
				t.Fatal("artifact-root close invalidated borrowed host namespace", err)
			}
			if err := capability.Close(); err != nil {
				t.Fatal(err)
			}
			input.requireUnchanged(t)
			if err := os.WriteFile(captured.CaptureResult().JournalPath, append(content, ' '), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := captured.ValidateActive(t.Context(), input.options.Filesystem, input.options.OperationAuthority, input.batch.budget); err == nil {
				t.Fatal("changed published record revision retained usable active evidence")
			}
		})
	}
}

type nativeCaptureFaultFilesystem struct {
	mutationfs.Store
	fault         string
	directory     string
	published     bool
	cancel        context.CancelFunc
	err           error
	afterCommit   func()
	prepareCalls  int
	ordinaryPath  string
	ordinaryReads int
}

func (filesystem *nativeCaptureFaultFilesystem) PrepareRootedTree(ctx context.Context, capability rootedpath.CommitCapability, build func(mutationfs.RootedTreeWriter) error) (mutationfs.PreparedRootedTree, error) {
	filesystem.prepareCalls++
	prepared, err := filesystem.Store.PrepareRootedTree(ctx, capability, func(writer mutationfs.RootedTreeWriter) error {
		return build(nativeCaptureFaultWriter{RootedTreeWriter: writer, filesystem: filesystem})
	})
	if err != nil {
		return nil, err
	}
	return nativeCaptureFaultTree{PreparedRootedTree: prepared, filesystem: filesystem}, nil
}

func (filesystem *nativeCaptureFaultFilesystem) ReadRootedRegularFileUpTo(ctx context.Context, capability rootedpath.CommitCapability, maximum int64) ([]byte, fs.FileMode, mutationfs.EntryIdentity, error) {
	path, err := capability.Destination().LexicalPath()
	if err != nil {
		return nil, 0, nil, err
	}
	if path == filesystem.ordinaryPath {
		filesystem.ordinaryReads++
	}
	if filesystem.published && filesystem.fault == "reload" && filepath.Base(path) == "expected" {
		return nil, 0, nil, filesystem.err
	}
	return filesystem.Store.ReadRootedRegularFileUpTo(ctx, capability, maximum)
}

func (filesystem *nativeCaptureFaultFilesystem) CaptureRootedEntryIdentity(ctx context.Context, capability rootedpath.CommitCapability) (mutationfs.EntryIdentity, error) {
	identity, err := filesystem.Store.CaptureRootedEntryIdentity(ctx, capability)
	path, pathErr := capability.Destination().LexicalPath()
	if err == nil && pathErr == nil && filesystem.published && filesystem.fault == "late-cancel" && path == filesystem.directory {
		filesystem.cancel()
	}
	return identity, errors.Join(err, pathErr)
}

type nativeCaptureFaultWriter struct {
	mutationfs.RootedTreeWriter
	filesystem *nativeCaptureFaultFilesystem
}

func (writer nativeCaptureFaultWriter) WriteFile(path mutationfs.TreeRelativePath, mode fs.FileMode, content io.Reader) error {
	if err := writer.RootedTreeWriter.WriteFile(path, mode, content); err != nil {
		return err
	}
	if writer.filesystem.fault == "stage" && path.Path() == journalrecord.FileName {
		return writer.filesystem.err
	}
	return nil
}

type nativeCaptureFaultTree struct {
	mutationfs.PreparedRootedTree
	filesystem *nativeCaptureFaultFilesystem
}

func (tree nativeCaptureFaultTree) CommitWithOutcome(ctx context.Context) (mutationfs.CommitOutcome, error) {
	if tree.filesystem.fault == "commit-cancel" {
		tree.filesystem.cancel()
	}
	outcome, err := tree.PreparedRootedTree.CommitWithOutcome(ctx)
	if outcome.State() == mutationfs.CommitOutcomeComplete {
		tree.filesystem.published = true
		if tree.filesystem.afterCommit != nil {
			tree.filesystem.afterCommit()
		}
	}
	if err == nil && tree.filesystem.fault == "indeterminate" {
		outcome, err = mutationfs.NewCommitOutcome(mutationfs.CommitOutcomeIndeterminate, nil)
		return outcome, errors.Join(err, tree.filesystem.err)
	}
	if err == nil && tree.filesystem.fault == "committed-error" {
		return outcome, tree.filesystem.err
	}
	return outcome, err
}

func TestCaptureNativeJournalFailureNeverReturnsPublishedEvidence(t *testing.T) {
	for _, fault := range []string{"stage", "commit-cancel", "committed-error", "indeterminate", "reload", "image-change", "reload-budget", "late-cancel"} {
		t.Run(fault, func(t *testing.T) {
			input := nativeCaptureInput(t, false, target.ScopeProject)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			directory := filepath.Join(input.paths.RecoveryDir, "native-record-apply")
			filesystem := &nativeCaptureFaultFilesystem{Store: input.options.Filesystem, fault: fault, directory: directory, cancel: cancel, err: errors.New("capture injected failure")}
			if fault == "image-change" {
				filesystem.afterCommit = func() {
					if err := os.WriteFile(filepath.Join(directory, "document-000000", "expected"), []byte("corrupted artifact"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			} else if fault == "reload-budget" {
				filesystem.afterCommit = func() {
					if err := input.batch.budget.AdmitTree(input.batch.budget.RemainingTreeWork()); err != nil {
						t.Fatal(err)
					}
				}
			}
			input.options.Filesystem = filesystem
			captured, err := input.capture(ctx)
			if err == nil || captured.Validate() == nil || captured.CaptureResult() != (CaptureResult{}) || len(captured.Documents()) != 0 {
				t.Fatalf("failed capture returned usable evidence: %+v %v", captured.CaptureResult(), err)
			}
			var publication *NativeJournalPublicationError
			if fault == "stage" {
				if errors.As(err, &publication) {
					t.Fatal("staging failure claimed a publication attempt")
				}
			} else {
				if !errors.As(err, &publication) || publication.Directory() != directory {
					t.Fatalf("lost publication outcome: %v", err)
				}
				want := mutationfs.CommitOutcomeComplete
				if fault == "commit-cancel" {
					want = mutationfs.CommitOutcomeUncommitted
				} else if fault == "indeterminate" {
					want = mutationfs.CommitOutcomeIndeterminate
				}
				if publication.Outcome().State() != want {
					t.Fatalf("publication outcome=%q want=%q: %v", publication.Outcome().State(), want, err)
				}
			}
			if filesystem.published {
				if _, statErr := os.Stat(filepath.Join(directory, journalrecord.FileName)); statErr != nil {
					t.Fatalf("published evidence was erased after a late failure: %v", statErr)
				}
			} else {
				if _, statErr := os.Stat(directory); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("failed private stage published an operation: %v", statErr)
				}
				if entries, readErr := os.ReadDir(input.paths.RecoveryDir); readErr == nil && len(entries) != 0 || readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
					t.Fatalf("private staging residue was not cleaned: %v %v", entries, readErr)
				}
			}
			input.requireUnchanged(t)
		})
	}
}

func TestCaptureNativeJournalRetainsActualGlobalClaimsAndProvisionalMetadata(t *testing.T) {
	for _, create := range []bool{true, false} {
		t.Run(fmt.Sprint("create=", create), func(t *testing.T) {
			input := nativeCaptureInput(t, create, target.ScopeGlobal)
			captured, err := input.capture(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			document := captured.Documents()[0]
			claims, exact := document.Claims()
			pending := document.ProvisionalAcquires()
			bindings := document.BindingIntents()
			if len(bindings) != 1 {
				t.Fatal("global group lost its binding transition")
			}
			if create {
				if _, known := document.Namespace().Observation().Exact(); known {
					if !exact || len(pending) != 0 || len(claims.Before()) != 0 || len(claims.After()) != 1 {
						t.Fatal("actual exact absent namespace was replaced with provisional metadata")
					}
					t.Log("filesystem supplied exact absent-path authority; provisional publication branch is not exercised on this host")
				} else {
					if exact || len(pending) != 1 || !pending[0].Intent().Owner().Equal(captured.Owner()) || pending[0].Alias() != document.Namespace().Addresses()[0] {
						t.Fatal("new global namespace lost alias-correlated provisional ownership")
					}
					if _, known := bindings[0].KnownAfter(); known {
						t.Fatal("unpromoted binding was replaced with invented exact metadata")
					}
				}
			} else if !exact || len(pending) != 0 || len(claims.Before()) != 1 || len(claims.After()) != 1 {
				t.Fatal("existing global namespace lost its actual complete claim fragment")
			}
			input.requireUnchanged(t)
		})
	}
}

func TestCaptureNativeJournalJoinsOrdinaryWorkWithoutInventingHistory(t *testing.T) {
	for _, work := range []bool{false, true} {
		t.Run(fmt.Sprint("ordinary-work=", work), func(t *testing.T) {
			input := nativeCaptureInput(t, false, target.ScopeProject)
			ordinary := []byte("ordinary executable\n")
			hash := artifact.HashFileContentWithExecutable(ordinary, true)
			id, err := entity.New(entity.KindInstructions, "ordinary")
			if err != nil {
				t.Fatal(err)
			}
			subject, err := topologyprojection.Subject(id, "instructions.project.agents")
			if err != nil {
				t.Fatal(err)
			}
			destination, err := output.Parse("AGENTS.md")
			if err != nil {
				t.Fatal(err)
			}
			row, err := durable.NewManagedPathState(subject, []target.Target{target.TargetCodex}, target.ScopeProject, destination, hash, realization.PathProjectionFile, realization.PathPermissionsExact, 0o700)
			if err != nil {
				t.Fatal(err)
			}
			ordinaryPath := filepath.Join(input.paths.ManifestRoot, "AGENTS.md")
			requireWholeGroupWrite(t, ordinaryPath, ordinary, 0o700)
			mutation, err := journalrecord.NewManagedPathRecordMutation(row.Subject(), row.ConsumerTargets(), row.Scope(), row.Destination(), hash, hash, row.ContentKind(), 0o700, &row)
			if err != nil {
				t.Fatal(err)
			}
			evidence, err := observe.NewManagedPathEvidence(row.Subject(), row.Destination(), true, hash, 0o700)
			if err != nil {
				t.Fatal(err)
			}
			before, err := input.batch.Evidence().BeforeState().WithManagedPaths([]durable.ManagedPathState{row})
			if err != nil {
				t.Fatal(err)
			}
			input.after, err = input.after.WithManagedPaths([]durable.ManagedPathState{row})
			if err != nil {
				t.Fatal(err)
			}
			input.stateBytes, err = input.options.StateCodec.Encode(before)
			if err != nil {
				t.Fatal(err)
			}
			requireWholeGroupWrite(t, input.paths.StatefilePath, input.stateBytes, 0o600)
			before, err = statefile.LoadOptionalWithOptions(t.Context(), input.paths.StatefilePath, statefile.ReadOptions{Codec: input.options.StateCodec, PhysicalPathBudget: input.batch.budget})
			if err != nil {
				t.Fatal(err)
			}
			reader, err := ownershipstore.NewRecoveryReader(input.registry)
			if err != nil {
				t.Fatal(err)
			}
			input.batch, err = ObserveNativeDocuments(t.Context(), journalobservation.Input{
				Owner: input.batch.owner, Before: before, Aliases: input.batch.Evidence().Namespaces()[0].Addresses(), Roots: input.batch.roots,
				Filesystem: input.options.Filesystem, Registry: reader, Limits: input.batch.Evidence().DocumentLimits(), Budget: input.batch.budget,
			})
			if err != nil {
				t.Fatal(err)
			}
			input.options.ManagedPathEvidence = []observe.ManagedPathEvidence{evidence}
			if work {
				input.options.ManagedPathMutations = []journalrecord.ManagedPathMutation{mutation}
				input.options.Resolver = func(destination output.Destination) (string, error) {
					return filepath.Join(input.paths.ManifestRoot, filepath.FromSlash(destination.RelativePath())), nil
				}
			}
			filesystem := &nativeCaptureFaultFilesystem{Store: input.options.Filesystem, ordinaryPath: ordinaryPath}
			input.options.Filesystem = filesystem
			scratch := t.TempDir()
			t.Setenv("TMPDIR", scratch)
			captured, err := input.capture(t.Context())
			if work {
				if err != nil || captured.Validate() != nil || !captured.BeforeState().Equal(before) || !captured.AfterState().Equal(input.after) {
					t.Fatalf("mixed capture lost its complete state or qualification: %v", err)
				}
				entries, err := captured.journal.RecordedEntries()
				if err != nil {
					t.Fatal(err)
				}
				documents, err := captured.journal.Documents()
				if err != nil {
					t.Fatal(err)
				}
				if filesystem.prepareCalls != 1 || filesystem.ordinaryReads != 1 || len(entries) != 1 || len(documents) != 1 {
					t.Fatalf("mixed capture did not join its actual ordinary and native actors: prepared=%d reads=%d", filesystem.prepareCalls, filesystem.ordinaryReads)
				}
				backup := filepath.Join(captured.CaptureResult().Directory, filepath.FromSlash(entries[0].Backup().Path()))
				content, readErr := os.ReadFile(backup)
				info, statErr := os.Stat(backup)
				if readErr != nil || statErr != nil || !bytes.Equal(content, ordinary) || info.Mode().Perm() != 0o700 {
					t.Fatalf("published ordinary backup lost its bytes or permissions: %v %v", readErr, statErr)
				}
			} else {
				if err != nil || !captured.BeforeState().Equal(before) || !captured.AfterState().Equal(input.after) {
					t.Fatalf("native capture lost unchanged ordinary history: %v", err)
				}
				if len(captured.BeforeState().ManagedPaths()) != 1 || len(captured.AfterState().ManagedPaths()) != 1 || filesystem.ordinaryReads != 0 {
					t.Fatal("ordinary census was dropped or ordinary host was reread")
				}
				if _, err := os.Stat(filepath.Join(captured.CaptureResult().Directory, "files")); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("unchanged ordinary history invented backup work", err)
				}
			}
			if entries, err := os.ReadDir(scratch); err != nil || len(entries) != 0 {
				t.Fatalf("ordinary backup source was materialized: %v %v", entries, err)
			}
			actual, err := os.ReadFile(ordinaryPath)
			if err != nil || !bytes.Equal(actual, ordinary) {
				t.Fatal("mixed capture changed ordinary host output", err)
			}
			input.requireUnchanged(t)
		})
	}
}

func TestCaptureNativeJournalInventoryFamilyAdmission(t *testing.T) {
	for _, existing := range []string{"finalized15", "active15", "malformed15", "malformed16", "unknown"} {
		t.Run(existing, func(t *testing.T) {
			input := nativeCaptureInput(t, true, target.ScopeProject)
			if existing == "finalized15" || existing == "active15" {
				identity, legacy := captureInventoryJournal(t, input.paths.RecoveryDir, "previous-ordinary-operation")
				if existing == "finalized15" {
					if err := os.Rename(legacy.Directory, filepath.Join(input.paths.RecoveryDir, identity.GCName())); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				version := journalrecord.OrdinaryVersion
				if existing == "malformed16" {
					version = journalrecord.NativeVersion
				} else if existing == "unknown" {
					version = journalrecord.NativeVersion + 1
				}
				path := filepath.Join(input.paths.RecoveryDir, "previous-operation", journalrecord.FileName)
				requireWholeGroupWrite(t, path, []byte(fmt.Sprintf(`{"version":%d,"operation_id":"previous-operation","invalid":true}`, version)), 0o600)
			}
			filesystem := &nativeCaptureFaultFilesystem{Store: input.options.Filesystem}
			input.options.Filesystem = filesystem
			captured, err := input.capture(t.Context())
			if existing == "finalized15" {
				if err != nil || captured.Validate() != nil || len(captured.Documents()) != 1 {
					t.Fatalf("inert legacy residue prevented native publication/reload: %v", err)
				}
				if err := ensureNoActive(t.Context(), input.paths.RecoveryDir, inventoryOptions{Filesystem: filesystem, StateCodec: input.options.StateCodec, PhysicalPathBudget: input.batch.budget, recordDecoder: decodeNativeCaptureInventoryJournal}); !errors.Is(err, ErrInterruptedApply) {
					t.Fatalf("published native16 was not recognized as active: %v", err)
				}
			} else {
				if err == nil || captured.Validate() == nil || filesystem.prepareCalls != 0 {
					t.Fatalf("blocked/unsupported inventory staged or returned capture: %v", err)
				}
				if existing == "active15" && !errors.Is(err, ErrInterruptedApply) {
					t.Fatalf("active15 was not classified by its strict decoder: %v", err)
				}
				if _, err := os.Stat(filepath.Join(input.paths.RecoveryDir, "native-record-apply")); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("blocked inventory published a new native operation", err)
				}
			}
			input.requireUnchanged(t)
		})
	}
}

func TestCaptureNativeJournalRefusesHostReferentInsidePublicationTree(t *testing.T) {
	input := nativeCaptureInput(t, true, target.ScopeGlobal)
	roots := input.batch.roots
	roots.CodexHome = nativeNamespaceRoot(t, input.paths.RecoveryDir)
	placement, err := profile.ResolveHostConfigPlacement(profile.HostConfigPlacementInput{
		Target: target.TargetCodex, Scope: target.ScopeGlobal, Path: "native-record-apply/document-000000/expected", Ownership: hostconfig.OwnershipKeys, Syntax: hostconfig.SyntaxTOML,
	})
	if err != nil {
		t.Fatal(err)
	}
	alias, err := aggregate.NewDocumentAddress(target.TargetCodex, target.ScopeGlobal, placement.Destination())
	if err != nil {
		t.Fatal(err)
	}
	row := membershipNativeRow(t, alias, "native-toml-v1", "model")
	row = selectionNativeRow(t, row.Contribution().Contract(), nativeRecordCanonical(t, `"fast"`))
	input.after = selectionState(t, row)
	input.edits = selectedCaptureEdits(t, nativeRecordFixture{read: nativeDocumentReadOptions{codecs: input.options.Codecs}}, aggregate.AbsentDocument(), []aggregate.ProjectionContract{row.Contribution().Contract()}, []durable.ManagedAggregateState{row})
	input.host = filepath.Join(input.paths.RecoveryDir, "native-record-apply", "document-000000", "expected")
	reader, err := ownershipstore.NewRecoveryReader(input.registry)
	if err != nil {
		t.Fatal(err)
	}
	input.batch, err = ObserveNativeDocuments(t.Context(), journalobservation.Input{
		Owner: input.batch.owner, Before: input.batch.Evidence().BeforeState(), Aliases: []aggregate.DocumentAddress{alias}, Roots: roots,
		Filesystem: input.options.Filesystem, Registry: reader, Limits: input.batch.Evidence().DocumentLimits(), Budget: input.batch.budget,
	})
	if err != nil {
		t.Fatal("actual selected global referent could not be observed", err)
	}
	filesystem := &nativeCaptureFaultFilesystem{Store: input.options.Filesystem}
	input.options.Filesystem = filesystem
	captured, err := input.capture(t.Context())
	if err == nil || captured.Validate() == nil || filesystem.prepareCalls != 0 {
		t.Fatalf("publication could establish a selected host referent: %v", err)
	}
	if _, err := os.Stat(filepath.Join(input.paths.RecoveryDir, "native-record-apply")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("overlapping publication created host/journal content", err)
	}
	input.requireUnchanged(t)
}

func TestCaptureNativeJournalRefusesUnjoinedAndExhaustedWork(t *testing.T) {
	for _, fault := range []string{"missing-authority", "omitted-edit", "wrong-after", "original-change", "budget", "changed-order", "canceled"} {
		t.Run(fault, func(t *testing.T) {
			input := nativeCaptureInput(t, fault != "wrong-after" && fault != "original-change", target.ScopeProject)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch fault {
			case "missing-authority":
				input.options.OperationAuthority = nil
			case "omitted-edit":
				input.edits = nil
			case "wrong-after":
				input.after = input.batch.Evidence().BeforeState()
			case "original-change":
				input.original = aggregate.ExistingDocument([]byte("model=false\nunmanaged=1\n"))
				requireWholeGroupWrite(t, input.host, input.original.Content(), 0o644)
			case "budget":
				if err := input.batch.budget.AdmitTree(input.batch.budget.RemainingTreeWork()); err != nil {
					t.Fatal(err)
				}
			case "changed-order":
				_, order := piEvidenceFixture(t, target.ScopeProject)
				input.options.OrderObservations = append(input.options.OrderObservations, order)
			case "canceled":
				cancel()
			}
			captured, err := input.capture(ctx)
			if err == nil || captured.Validate() == nil || len(captured.Documents()) != 0 {
				t.Fatalf("unsupported/exhausted capture returned evidence: %v", err)
			}
			if _, err := os.Stat(filepath.Join(input.paths.RecoveryDir, "native-record-apply")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid work published a journal: %v", err)
			}
			input.requireUnchanged(t)
		})
	}
}

func TestCaptureNativeJournalLastSelectedRemovalRetainsOriginalAndBindingRelease(t *testing.T) {
	input := nativeCaptureInput(t, false, target.ScopeProject)
	var err error
	input.after, err = input.batch.Evidence().BeforeState().WithManagedAggregates(nil)
	if err != nil {
		t.Fatal(err)
	}
	contract := input.batch.Evidence().BeforeState().ManagedAggregates()[0].Contribution().Contract()
	input.edits = selectedCaptureEdits(t, nativeRecordFixture{read: nativeDocumentReadOptions{codecs: input.options.Codecs}}, input.original, []aggregate.ProjectionContract{contract}, nil)
	captured, err := input.capture(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	document := captured.Documents()[0]
	expected, mode := document.Expected()
	if !expected.Exists() || mode == nil || mode.FileMode() != 0o644 || len(captured.AfterState().ManagedAggregates()) != 0 {
		t.Fatal("last selected removal invented a whole-file delete or retained subject state")
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	codec, _ := input.options.Codecs.Lookup(contract.CodecContractID())
	state, failure := codec.Read(expected, selection)
	if failure != nil || len(state.States()) != 1 || state.States()[0].Present() {
		t.Fatal("last selected removal image retained the selected key", failure)
	}
	bindings := document.BindingIntents()
	if len(bindings) != 1 {
		t.Fatal("last selected removal lost its binding retirement")
	}
	after, known := bindings[0].KnownAfter()
	if _, present := after.Get(); !known || present {
		t.Fatal("last selected removal did not retain known binding release metadata")
	}
	input.requireUnchanged(t)
}
