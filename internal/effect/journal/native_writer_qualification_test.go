package journal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/target"
)

func caseAliasedMixedFixture(t *testing.T) mixedJournalFixture {
	t.Helper()
	fixture := mixedStageFixture(t)
	root := fixture.whole.namespace.roots.Manifest
	actual := filepath.Join(root.SelectedPath, ".agents", "skills", "CaseOwner")
	variant := filepath.Join(root.SelectedPath, ".agents", "skills", "caseowner")
	physical := filepath.Join(actual, "host.md")
	requireWholeGroupWrite(t, physical, fixture.comparison.before.Content(), 0o644)
	before, err := os.Stat(actual)
	if err != nil {
		t.Fatal(err)
	}
	other, err := os.Stat(variant)
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("fixture filesystem distinguishes the case variants")
	}
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, other) {
		t.Skip("case variants are distinct actual directories")
	}
	address := fixture.whole.namespace.Addresses()[0]
	aliasPath := filepath.Join(root.SelectedPath, filepath.FromSlash(address.AggregateRoot().RelativePath()))
	if err := os.Remove(aliasPath); err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(filepath.Dir(aliasPath), physical)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(relative, aliasPath); err != nil {
		t.Fatal(err)
	}
	namespace, err := ObserveNativeDocumentNamespace(t.Context(), fixture.whole.namespace.Addresses(), fixture.whole.namespace.roots, journalTestFilesystem(), fixture.whole.input.budget)
	if err != nil {
		t.Fatal(err)
	}
	fixture.whole.namespace = namespace
	exact, known := namespace.Observation().Exact()
	if !known {
		t.Fatal("existing native referent requires exact evidence")
	}
	binding, err := ownership.NewDocumentBinding(fixture.whole.input.owner, address.AggregateRoot(), exact)
	if err != nil {
		t.Fatal(err)
	}
	present, err := ownership.PresentDocumentBinding(binding)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := ownership.NewExactDocumentBindingIntent(fixture.whole.input.owner, address.AggregateRoot(), present, present)
	if err != nil {
		t.Fatal(err)
	}
	fixture.whole.input.bindings = []ownership.DocumentBindingIntent{intent}
	group, comparison, err := prepareWholeDocumentGroup(t.Context(), namespace, fixture.whole.input)
	if err != nil {
		t.Fatal(err)
	}
	fixture.journal.record.DocumentGroups, fixture.comparison = []recoveryDocumentGroup{group}, comparison
	hash, _, err := access.HashPath(t.Context(), variant)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse(".agents/skills/caseowner")
	if err != nil {
		t.Fatal(err)
	}
	state, err := durable.NewManagedPathState(testManagedPathSubject(t, "caseowner"), []target.Target{target.TargetCodex}, target.ScopeProject, destination, hash, realization.PathProjectionDirectory, realization.PathPermissionsNone, 0)
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
	fixture.capture.ManagedPathMutations, fixture.capture.ManagedPathEvidence = []ManagedPathMutation{mutation}, []observe.ManagedPathEvidence{evidence}
	for side, value := range []durable.Snapshot{fixture.whole.envelope.before, fixture.whole.envelope.after} {
		combined, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: append(value.ManagedPaths(), state), ManagedAggregates: value.ManagedAggregates()})
		if err != nil {
			t.Fatal(err)
		}
		if side == 0 {
			fixture.journal.before = combined
		} else {
			fixture.journal.after = combined
		}
	}
	return fixture
}

func TestNativeMixedWriterCaseCollisionRefusesBeforeFinalWriter(t *testing.T) {
	fixture := caseAliasedMixedFixture(t)
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	writer := &mixedNoWriteWriter{}
	fingerprint, err := fixture.journal.stageMixedDocumentJournal(t.Context(), fixture.paths, fixture.capture, []DocumentComparison{fixture.comparison}, writer, fixture.stageOptions(imageBudget(t)))
	if err == nil || fingerprint != "" || writer.writes != 0 || writer.directories != 0 {
		t.Fatalf("case collision reached final writer or returned a usable fingerprint: %v", err)
	}
	if entries, err := os.ReadDir(temporary); err != nil || len(entries) != 0 {
		t.Fatalf("case refusal retained ordinary staging artifacts: %v", err)
	}
	fixture.whole.requireUnchangedHost(t)
}

func TestQualifiedNativeWriterCaseCollisionRefusesBeforeImages(t *testing.T) {
	fixture := caseAliasedMixedFixture(t)
	source, _ := fixture.source(t, imageBudget(t))
	fixture.journal.record.Entries = source.record.Entries
	if err := fixture.journal.record.validateWriterSpellingPartition(t.Context()); err != nil {
		t.Fatalf("case fixture must pass the spelling-only metadata check: %v", err)
	}
	read := fixture.whole.envelope.read
	reader := &nativeNamespaceReader{RootedReader: read.filesystem}
	read.filesystem, read.budget = reader, imageBudget(t)
	loaded, err := fixture.journal.loadQualifiedDocuments(t.Context(), read, fixture.whole.namespace.roots)
	if err == nil || loaded.namespaces != nil || loaded.records.groups != nil || reader.imageReads != 0 {
		t.Fatal("case collision reached artifact reads or returned a qualified prefix")
	}
	fixture.whole.requireUnchangedHost(t)
}

type writerQualificationCapability struct {
	rootedpath.CommitCapability
	closed   *int
	closeErr error
}

func (capability writerQualificationCapability) Close() error {
	*capability.closed += 1
	return errors.Join(capability.CommitCapability.Close(), capability.closeErr)
}

type refusedWriterPathBudget struct {
	err   error
	calls int
}

func (budget *refusedWriterPathBudget) AdmitPathComponents(int) error {
	budget.calls++
	return budget.err
}

func TestWriterObservationUsesSharedBudgetAndClosesOnFailure(t *testing.T) {
	_, roots := nativeNamespaceRecordFixture(t)
	entry, acquire, closed := ordinaryWriterCapability(t, roots.PiAgent, "custom.json")
	destination, err := output.Parse(entry.Path)
	if err != nil {
		t.Fatal(err)
	}
	capability, present, err := acquire(destination, imageBudget(t))
	if err != nil || !present {
		t.Fatalf("acquire fixture capability: %v", err)
	}
	failure := errors.New("injected shared operation path budget refusal")
	shared := &refusedWriterPathBudget{err: failure}
	budget, err := recovery.NewPhysicalWorkBudgetWithPathBudget(0, shared)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := observeNativeWriterCapability(t.Context(), capability, budget)
	if !errors.Is(err, failure) || observation.Validate() == nil || shared.calls == 0 || *closed != 1 {
		t.Fatalf("path observation reset the shared budget or retained usable evidence/capability: %v, calls=%d closes=%d", err, shared.calls, *closed)
	}
}

func ordinaryWriterCapability(t *testing.T, root NativeDocumentRoot, relative string) (recoveryEntry, RootedCapabilityResolver, *int) {
	t.Helper()
	authority, err := root.Root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := authority.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	entry := recoveryEntry{Scope: string(target.ScopeGlobal), Path: "@pi-agent/" + relative, GlobalPathBinding: &recoveryGlobalPathBinding{ResolvedPath: filepath.Join(root.SelectedPath, relative), RootProvenance: persistedNativeRoot(provenance)}}
	closed := new(int)
	acquire := func(destination output.Destination, budget rootedpath.PhysicalTraversalBudget) (rootedpath.CommitCapability, bool, error) {
		if destination.String() != entry.Path || budget == nil {
			return nil, false, errors.New("unexpected destination or missing shared budget")
		}
		selected, err := rootedpath.NewRelativeDestination(destination.RelativePath())
		if err != nil {
			return nil, false, err
		}
		bound, err := authority.Bind(selected)
		if err != nil {
			return nil, false, err
		}
		capability, err := root.Root.AcquireBounded(bound, recovery.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return nil, false, err
		}
		return writerQualificationCapability{CommitCapability: capability, closed: closed}, true, nil
	}
	return entry, acquire, closed
}

func TestQualifiedWriterKeepsCaseSensitiveFilesDistinct(t *testing.T) {
	fixture, roots := nativeNamespaceRecordFixture(t)
	variant := filepath.Join(roots.PiAgent.SelectedPath, "CUSTOM.JSON")
	if _, err := os.Stat(variant); err == nil {
		t.Skip("fixture filesystem does not distinguish these case variants")
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	requireWholeGroupWrite(t, variant, []byte("distinct ordinary file"), 0o600)
	entry, acquire, closed := ordinaryWriterCapability(t, roots.PiAgent, "CUSTOM.JSON")
	fixture.record.Entries = []recoveryEntry{entry}
	journal := nativeJournalSnapshots{record: fixture.record, owner: fixture.claims.owner, before: fixture.before, after: fixture.after}
	read := fixture.read
	read.ordinaryCapabilities, read.budget = acquire, imageBudget(t)
	qualified, err := journal.qualifyDocumentNamespaces(t.Context(), read, roots)
	if err != nil || len(qualified) != 1 || *closed != 1 {
		t.Fatalf("distinct case-sensitive files were merged or a capability leaked: %v, closed=%d", err, *closed)
	}
}

func TestQualifiedWriterKeepsNativeProjectSymlinkAlias(t *testing.T) {
	fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeGlobal, "replace", []byte("before"), []byte("after"))
	roots := fixture.namespace.roots
	if err := os.Symlink(roots.PiAgent.SelectedPath, filepath.Join(roots.Manifest.SelectedPath, ".pi")); err != nil {
		t.Fatal(err)
	}
	project := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/prompts/Caf\u00e9.md")
	namespace, err := ObserveNativeDocumentNamespace(t.Context(), append(fixture.namespace.Addresses(), project), roots, fixture.reader, fixture.input.budget)
	if err != nil {
		t.Fatal(err)
	}
	group, _, err := prepareWholeDocumentGroup(t.Context(), namespace, fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	journal := nativeJournalSnapshots{record: fixture.envelope.record, owner: fixture.input.owner, before: fixture.envelope.before, after: fixture.envelope.after}
	grant, err := roots.Manifest.Root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := grant.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	journal.record.ManifestRootProvenance, journal.record.DocumentGroups = persistedNativeRoot(provenance), []recoveryDocumentGroup{group}
	qualified, err := journal.qualifyDocumentNamespaces(t.Context(), nativeDocumentReadOptions{filesystem: fixture.reader, budget: imageBudget(t)}, roots)
	if err != nil || len(qualified) != 1 || len(qualified[0].aliases) != 2 {
		t.Fatalf("ordinary/native partition rejected a supported project symlink alias: %v", err)
	}
	fixture.requireUnchangedHost(t)
}

func TestQualifiedOrdinaryWriterRequiresSelectedRootAndClosesCapabilities(t *testing.T) {
	for _, change := range []string{"valid", "duplicate", "conflicting duplicate", "missing resolver", "absent capability", "wrong root", "close failure", "resolver error", "canceled"} {
		t.Run(change, func(t *testing.T) {
			fixture, roots := nativeNamespaceRecordFixture(t)
			requireWholeGroupWrite(t, filepath.Join(roots.PiAgent.SelectedPath, "ordinary.json"), []byte("ordinary"), 0o600)
			entry, acquire, closed := ordinaryWriterCapability(t, roots.PiAgent, "ordinary.json")
			ctx := t.Context()
			wantCloses := 1
			switch change {
			case "missing resolver":
				acquire, wantCloses = nil, 0
			case "absent capability":
				acquire, wantCloses = func(output.Destination, rootedpath.PhysicalTraversalBudget) (rootedpath.CommitCapability, bool, error) {
					return nil, false, nil
				}, 0
			case "wrong root":
				entry.GlobalPathBinding.RootProvenance.ObjectFingerprint = fixture.record.ManifestRootProvenance.ObjectFingerprint
			case "close failure", "resolver error":
				original := acquire
				acquire = func(destination output.Destination, budget rootedpath.PhysicalTraversalBudget) (rootedpath.CommitCapability, bool, error) {
					capability, present, err := original(destination, budget)
					if err != nil {
						return capability, present, err
					}
					if change == "resolver error" {
						return capability, present, errors.New("injected resolver failure")
					}
					return writerQualificationCapability{CommitCapability: capability, closed: new(int), closeErr: errors.New("injected close failure")}, present, nil
				}
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				wantCloses = 0
			}
			fixture.record.Entries = []recoveryEntry{entry}
			if change == "duplicate" || change == "conflicting duplicate" {
				second := entry
				if change == "conflicting duplicate" {
					binding := *entry.GlobalPathBinding
					binding.RootProvenance.ObjectFingerprint = fixture.record.ManifestRootProvenance.ObjectFingerprint
					second.GlobalPathBinding = &binding
				}
				fixture.record.Entries = append(fixture.record.Entries, second)
			}
			journal := nativeJournalSnapshots{record: fixture.record, owner: fixture.claims.owner, before: fixture.before, after: fixture.after}
			read := fixture.read
			read.ordinaryCapabilities, read.budget = acquire, imageBudget(t)
			qualified, err := journal.qualifyDocumentNamespaces(ctx, read, roots)
			if (err == nil) != (change == "valid" || change == "duplicate") || err != nil && qualified != nil || *closed != wantCloses {
				t.Fatalf("root/capability failure returned a prefix or leaked a capability: %v, closed=%d want=%d", err, *closed, wantCloses)
			}
		})
	}
}
