package publication

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/mutation"

	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/effect/journal/namespace/namespacetest"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/target"
)

func TestQualifiedWriterPartitionProtectsGlobalSymlinkRoutes(t *testing.T) {
	root := nativeNamespaceRoot(t, t.TempDir())
	requireWholeGroupWrite(t, filepath.Join(root.SelectedPath, "referent.json"), []byte("{}"), 0o600)
	if err := os.Symlink("referent.json", filepath.Join(root.SelectedPath, "route.json")); err != nil {
		t.Fatal(err)
	}
	roots := namespacetest.LegacyRoots(t, map[output.RootRole]journalnamespace.RootSelection{output.RootProject: root, output.RootPiAgent: root})
	budget := imageBudget(t)
	alias := nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/route.json")
	namespace, err := journalnamespace.Observe(t.Context(), []aggregate.DocumentAddress{alias}, roots, namespacetest.Reservations(t), journalTestFilesystem(), budget)
	if err != nil {
		t.Fatal(err)
	}
	qualified, err := roots.Qualify(t.Context(), budget)
	if err != nil {
		t.Fatal(err)
	}
	manifest, supplied := qualified.Manifest()
	if !supplied {
		t.Fatal("fixture lost its manifest binding")
	}
	for _, collision := range []bool{false, true} {
		relative := "unrelated.json"
		if collision {
			relative = "route.json"
		}
		entry, acquire, closed := ordinaryWriterCapability(t, root, relative)
		err := journalqualification.QualifyWriterPartition(t.Context(), []journalrecord.PathEntry{entry}, []journalnamespace.DocumentNamespace{namespace}, manifest, acquire, budget, nil)
		if (err != nil) != collision || *closed != 1 {
			t.Fatalf("global route collision=%v: err=%v closed=%d", collision, err, *closed)
		}
	}
}

func TestQualifiedWriterPartitionKeepsMissingRootsAtOneAnchor(t *testing.T) {
	base := nativeNamespaceRoot(t, t.TempDir())
	roots := namespacetest.LegacyRoots(t, map[output.RootRole]journalnamespace.RootSelection{output.RootProject: base})
	budget := imageBudget(t)
	var aliases []aggregate.DocumentAddress
	var selected []string
	for _, spec := range []struct {
		role   output.RootRole
		target target.Target
		alias  string
	}{
		{output.RootPiAgent, target.TargetPi, "@pi-agent/settings.json"},
		{output.RootCodexHome, target.TargetCodex, "@codex-home/settings.json"},
	} {
		path := filepath.Join(base.SelectedPath, "missing-"+string(spec.target))
		selection, err := journalnamespace.CaptureRoot(t.Context(), path, budget)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = selection.Root.Close() })
		roots = namespacetest.WithLegacyRoot(t, roots, spec.role, selection)
		aliases = append(aliases, nativeNamespaceAddress(t, spec.target, target.ScopeGlobal, spec.alias))
		selected = append(selected, path)
	}
	var namespaces []journalnamespace.DocumentNamespace
	for _, alias := range aliases {
		namespace, err := journalnamespace.Observe(t.Context(), []aggregate.DocumentAddress{alias}, roots, namespacetest.Reservations(t), journalTestFilesystem(), budget)
		if err != nil {
			t.Fatal(err)
		}
		namespaces = append(namespaces, namespace)
	}
	qualified, err := roots.Qualify(t.Context(), budget)
	if err != nil {
		t.Fatal(err)
	}
	manifest, supplied := qualified.Manifest()
	if !supplied {
		t.Fatal("fixture lost its manifest binding")
	}
	if err := journalqualification.QualifyWriterPartition(t.Context(), nil, namespaces, manifest, nil, budget, nil); err != nil {
		t.Fatal("disjoint missing roots collapsed to their shared anchor", err)
	}
	coincident, err := journalnamespace.CaptureRoot(t.Context(), selected[0], budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coincident.Root.Close() })
	coincidentRoots := namespacetest.WithLegacyRoot(t, roots, output.RootCodexHome, coincident)
	other, err := journalnamespace.Observe(t.Context(), []aggregate.DocumentAddress{aliases[1]}, coincidentRoots, namespacetest.Reservations(t), journalTestFilesystem(), budget)
	if err != nil {
		t.Fatal(err)
	}
	if err := journalqualification.QualifyWriterPartition(t.Context(), nil, []journalnamespace.DocumentNamespace{namespaces[0], other}, manifest, nil, budget, nil); err == nil {
		t.Fatal("actual overlapping missing-root writers passed physical qualification")
	}
	for _, path := range selected {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("writer qualification created selected-root scaffolding", err)
		}
	}
}

func caseAliasedMixedFixture(t *testing.T) mixedJournalFixture {
	t.Helper()
	fixture := mixedStageFixture(t)
	root := namespacetest.LegacyRoot(t, fixture.whole.roots, output.RootProject)
	actual := filepath.Join(root.SelectedPath, ".agents", "skills", "CaseOwner")
	variant := filepath.Join(root.SelectedPath, ".agents", "skills", "caseowner")
	physical := filepath.Join(actual, "host.md")
	requireWholeGroupWrite(t, physical, fixture.comparison.BeforeDocument().Content(), 0o644)
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
	namespace, err := journalnamespace.Observe(t.Context(), fixture.whole.namespace.Addresses(), fixture.whole.roots, namespacetest.Reservations(t), journalTestFilesystem(), fixture.whole.input.Budget())
	if err != nil {
		t.Fatal(err)
	}
	fixture.whole.namespace = namespace
	exact, known := namespace.Observation().Exact()
	if !known {
		t.Fatal("existing native referent requires exact evidence")
	}
	binding, err := ownership.NewDocumentBinding(fixture.whole.input.Owner(), address.AggregateRoot(), exact)
	if err != nil {
		t.Fatal(err)
	}
	present, err := ownership.PresentDocumentBinding(binding)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := ownership.NewExactDocumentBindingIntent(fixture.whole.input.Owner(), address.AggregateRoot(), present, present)
	if err != nil {
		t.Fatal(err)
	}
	fixture.whole.input.CaptureInput = reviseCaptureInput(fixture.whole.input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) {
		spec.Bindings = []ownership.DocumentBindingIntent{intent}
	})
	group, comparison, err := prepareWholeDocumentGroup(t.Context(), namespace, fixture.whole.input)
	if err != nil {
		t.Fatal(err)
	}
	fixture.comparison = comparison
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
	mutation, err := journalrecord.NewManagedPathRecordMutation(state.Subject(), state.ConsumerTargets(), state.Scope(), state.Destination(), hash, hash, state.ContentKind(), 0, &state)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := observe.NewManagedPathEvidence(state.Subject(), state.Destination(), true, hash, 0)
	if err != nil {
		t.Fatal(err)
	}
	fixture.capture.ManagedPathMutations, fixture.capture.ManagedPathEvidence = []journalrecord.ManagedPathMutation{mutation}, []observe.ManagedPathEvidence{evidence}
	combinedFixture := fixture.whole
	for side, value := range []durable.Snapshot{fixture.whole.envelope.before, fixture.whole.envelope.after} {
		combined, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: append(value.ManagedPaths(), state), ManagedAggregates: value.ManagedAggregates()})
		if err != nil {
			t.Fatal(err)
		}
		if side == 0 {
			combinedFixture.envelope.before = combined
		} else {
			combinedFixture.envelope.after = combined
		}
	}
	fixture.journal = combinedFixture.recordForGroups(t, []journalrecord.SealedDocumentGroup{group})
	return fixture
}

func TestNativeMixedWriterCaseCollisionRefusesBeforeFinalWriter(t *testing.T) {
	fixture := caseAliasedMixedFixture(t)
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	writer := &mixedNoWriteWriter{}
	fingerprint, err := stageMixedDocumentJournal(fixture.journal, t.Context(), fixture.paths, fixture.capture, []journaldocument.Comparison{fixture.comparison}, writer, fixture.stageOptions(imageBudget(t)))
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
	journal, err := fixture.journal.WithOrdinaryEvidence(source.record)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.Encode(t.Context(), fixture.whole.envelope.codec); err != nil {
		t.Fatalf("case fixture must pass the spelling-only metadata check: %v", err)
	}
	read := fixture.whole.envelope.read
	reader := &nativeNamespaceReader{RootedReader: read.Filesystem}
	read.Filesystem, read.Budget = reader, imageBudget(t)
	loaded, err := journalqualification.QualifyDocuments(journal, t.Context(), read, fixture.whole.roots)
	_, hasNamespace := loaded.NamespaceAt(0)
	if err == nil || hasNamespace || loaded.Len() != 0 || reader.imageReads != 0 {
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
	entry, acquire, closed := ordinaryWriterCapability(t, namespacetest.LegacyRoot(t, roots, output.RootPiAgent), "custom.json")
	destination, err := output.Parse(entry.Path())
	if err != nil {
		t.Fatal(err)
	}
	capability, present, err := acquire(destination, imageBudget(t))
	if err != nil || !present {
		t.Fatalf("acquire fixture capability: %v", err)
	}
	failure := errors.New("injected shared operation path budget refusal")
	shared := &refusedWriterPathBudget{err: failure}
	budget, err := physicalwork.NewBudgetWithPathBudget(0, shared)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := journalnamespace.ObserveWriterCapability(t.Context(), capability, budget)
	if !errors.Is(err, failure) || observation.Validate() == nil || shared.calls == 0 || *closed != 1 {
		t.Fatalf("path observation reset the shared budget or retained usable evidence/capability: %v, calls=%d closes=%d", err, shared.calls, *closed)
	}
}

func ordinaryWriterCapability(t *testing.T, root journalnamespace.RootSelection, relative string) (journalrecord.PathEntry, journalnamespace.CapabilityResolver, *int) {
	t.Helper()
	authority, err := root.Root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := authority.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse("@pi-agent/" + relative)
	if err != nil {
		t.Fatal(err)
	}
	physical := filepath.Join(authority.PhysicalRoot(), relative)
	entry := globalEntryForTest(t, destination, physical, journalrecord.NewGlobalBinding(physical, provenance))
	closed := new(int)
	acquire := func(destination output.Destination, budget rootedpath.PhysicalTraversalBudget) (rootedpath.CommitCapability, bool, error) {
		if destination.String() != entry.Path() || budget == nil {
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
		capability, err := root.Root.AcquireBounded(bound, physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return nil, false, err
		}
		return writerQualificationCapability{CommitCapability: capability, closed: closed}, true, nil
	}
	return entry, acquire, closed
}

func TestQualifiedWriterKeepsCaseSensitiveFilesDistinct(t *testing.T) {
	fixture, roots := nativeNamespaceRecordFixture(t)
	variant := filepath.Join(namespacetest.LegacyRoot(t, roots, output.RootPiAgent).SelectedPath, "CUSTOM.JSON")
	if _, err := os.Stat(variant); err == nil {
		t.Skip("fixture filesystem does not distinguish these case variants")
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	requireWholeGroupWrite(t, variant, []byte("distinct ordinary file"), 0o600)
	entry, acquire, closed := ordinaryWriterCapability(t, namespacetest.LegacyRoot(t, roots, output.RootPiAgent), "CUSTOM.JSON")
	namespaces, manifest, protected := writerPartitionInputsForTest(t, fixture, roots)
	err := journalqualification.QualifyWriterPartition(t.Context(), []journalrecord.PathEntry{entry}, namespaces, manifest, acquire, imageBudget(t), protected)
	if err != nil || len(namespaces) != 1 || *closed != 1 {
		t.Fatalf("distinct case-sensitive files were merged or a capability leaked: %v, closed=%d", err, *closed)
	}
}

func TestQualifiedWriterKeepsNativeProjectSymlinkAlias(t *testing.T) {
	fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeGlobal, "replace", []byte("before"), []byte("after"))
	roots := fixture.roots
	if err := os.Symlink(namespacetest.LegacyRoot(t, roots, output.RootPiAgent).SelectedPath, filepath.Join(namespacetest.LegacyRoot(t, roots, output.RootProject).SelectedPath, ".pi")); err != nil {
		t.Fatal(err)
	}
	project := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/prompts/Caf\u00e9.md")
	namespace, err := journalnamespace.Observe(t.Context(), append(fixture.namespace.Addresses(), project), roots, namespacetest.Reservations(t), fixture.reader, fixture.input.Budget())
	if err != nil {
		t.Fatal(err)
	}
	group, _, err := prepareWholeDocumentGroup(t.Context(), namespace, fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	journal := fixture.recordForGroups(t, []journalrecord.SealedDocumentGroup{group})
	qualified, err := journalqualification.LoadNamespaces(journal, t.Context(), journalqualification.ReadOptions{Protection: namespacetest.Reservations(t), Rules: journalqualification.RecordedRuleExpectations(), Filesystem: fixture.reader, Budget: imageBudget(t)}, roots)
	if err != nil || len(qualified) != 1 || len(qualified[0].Aliases()) != 2 {
		t.Fatalf("ordinary/native partition rejected a supported project symlink alias: %v", err)
	}
	fixture.requireUnchangedHost(t)
}

func TestQualifiedOrdinaryWriterRequiresSelectedRootAndClosesCapabilities(t *testing.T) {
	for _, change := range []string{"valid", "duplicate", "conflicting duplicate", "missing resolver", "absent capability", "wrong root", "close failure", "resolver error", "canceled"} {
		t.Run(change, func(t *testing.T) {
			fixture, roots := nativeNamespaceRecordFixture(t)
			requireWholeGroupWrite(t, filepath.Join(namespacetest.LegacyRoot(t, roots, output.RootPiAgent).SelectedPath, "ordinary.json"), []byte("ordinary"), 0o600)
			entry, acquire, closed := ordinaryWriterCapability(t, namespacetest.LegacyRoot(t, roots, output.RootPiAgent), "ordinary.json")
			namespaces, manifest, protected := writerPartitionInputsForTest(t, fixture, roots)
			withOtherRoot := func(value journalrecord.PathEntry) journalrecord.PathEntry {
				provenance := captureFixtureProvenance(t, namespacetest.LegacyRoot(t, roots, output.RootPiAgent).SelectedPath)
				other := captureFixtureProvenance(t, namespacetest.LegacyRoot(t, roots, output.RootProject).SelectedPath)
				changed, err := rootedpath.NewAuthorityProvenance(provenance.PhysicalRoot(), other.ObjectFingerprint(), provenance.MountFingerprint())
				if err != nil {
					t.Fatal(err)
				}
				destination, err := output.Parse(value.Path())
				if err != nil {
					t.Fatal(err)
				}
				return globalEntryForTest(t, destination, value.GlobalPath(), journalrecord.NewGlobalBinding(value.GlobalPath(), changed))
			}
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
				entry = withOtherRoot(entry)
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
			entries := []journalrecord.PathEntry{entry}
			if change == "duplicate" || change == "conflicting duplicate" {
				second := entry
				if change == "conflicting duplicate" {
					second = withOtherRoot(second)
				}
				entries = append(entries, second)
			}
			err := journalqualification.QualifyWriterPartition(ctx, entries, namespaces, manifest, acquire, imageBudget(t), protected)
			if (err == nil) != (change == "valid" || change == "duplicate") || *closed != wantCloses {
				t.Fatalf("root/capability failure was admitted or leaked a capability: %v, closed=%d want=%d", err, *closed, wantCloses)
			}
		})
	}
}

func writerPartitionInputsForTest(t *testing.T, fixture nativeRecordFixture, roots journalnamespace.Roots) ([]journalnamespace.DocumentNamespace, journalnamespace.RootGrant, []pathauthority.Exact) {
	t.Helper()
	namespaces, err := journalqualification.LoadNamespaces(fixture.journal, t.Context(), fixture.read, roots)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := roots.Qualify(t.Context(), nativeFixtureBudget(t))
	if err != nil {
		t.Fatal(err)
	}
	manifest, present := grants.Manifest()
	if !present {
		t.Fatal("writer fixture lost its manifest grant")
	}
	protected := []pathauthority.Exact{fixture.journal.Owner().StatefileAuthority()}
	if fixture.read.Artifacts != nil {
		authority, err := fixture.read.Artifacts.Authority()
		if err != nil {
			t.Fatal(err)
		}
		observation, err := mutation.ObserveDirectoryEntryAuthority(authority.PhysicalRoot())
		if err != nil {
			t.Fatal(err)
		}
		exact, present := observation.Exact()
		if !present {
			t.Fatal("writer fixture artifacts lost exact authority")
		}
		protected = append(protected, exact)
	}
	return namespaces, manifest, protected
}
