package journal

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

func fixtureDocumentRemoval(t *testing.T, group *recoveryDocumentGroup, before, expected aggregate.Document) {
	t.Helper()
	states, err := journaldocument.RemovalStates(before, group.Original.Mode, expected, group.Expected.Mode)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) == 0 {
		group.Removal = nil
		return
	}
	demands := make([]recovery.RemovalDemand, 0, len(group.Aliases))
	for _, item := range group.Aliases {
		alias, err := item.Address.canonical()
		if err != nil {
			t.Fatal(err)
		}
		demand, err := recovery.NewRemovalDemand(alias.Scope(), alias.AggregateRoot(), states)
		if err != nil {
			t.Fatal(err)
		}
		demands = append(demands, demand)
	}
	set, err := recovery.NewRemovalDemandSet(demands)
	if err != nil {
		t.Fatal(err)
	}
	budget, err := physicalwork.NewBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(group.Writer.Root.PhysicalRoot, filepath.FromSlash(group.Writer.RelativePath))
	namespace, _, err := captureRemovalNamespace(t.Context(), path, make(map[string]struct{}), budget)
	if err != nil {
		t.Fatal(err)
	}
	value, err := recovery.NewDocumentRemoval(set, namespace)
	if err != nil {
		t.Fatal(err)
	}
	record, err := persistedDocumentRemoval(value)
	if err != nil {
		t.Fatal(err)
	}
	group.Removal = &record
}

func TestNativeSelectedRemovalUsesActualEditsNotObservedAliases(t *testing.T) {
	fixture := nativeJournalFixture(t)
	fixture.before = durable.EmptySnapshot()
	selected := fixture.after.ManagedAggregates()[0]
	original := aggregate.AbsentDocument()
	edits := selectedCaptureEdits(t, fixture, original, []aggregate.ProjectionContract{selected.Contribution().Contract()}, []durable.ManagedAggregateState{selected})
	capture := selectedCaptureFixture(t, fixture, original, edits, nil, journalnamespace.Roots{})
	roots := capture.roots
	if err := os.Symlink(roots.PiAgent.SelectedPath, filepath.Join(roots.Manifest.SelectedPath, ".pi")); err != nil {
		t.Fatal(err)
	}
	observedAlias := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/"+fixture.claims.alias.AggregateRoot().RelativePath())
	namespace, err := journalnamespace.Observe(t.Context(), []aggregate.DocumentAddress{fixture.claims.alias, observedAlias}, roots, capture.reader, capture.input.budget)
	if err != nil {
		t.Fatal(err)
	}
	group, comparison, err := prepareSelectedDocumentGroup(t.Context(), namespace, capture.input, capture.subjects)
	if err != nil || group.Removal == nil || len(group.Aliases) != 2 || len(group.Removal.Demands) != 1 {
		t.Fatalf("observed alias gained removal debt: %v", err)
	}
	if group.Removal.Demands[0].Scope != string(fixture.claims.alias.Scope()) || group.Removal.Demands[0].Destination != fixture.claims.alias.AggregateRoot().String() {
		t.Fatal("actual edit relation was replaced by the observed alias")
	}
	capture.namespace = namespace
	capture.requireCompleteStage(t, group, comparison)
	bad := group
	removal := *group.Removal
	removal.Demands = append([]recoveryDocumentRemovalDemand(nil), removal.Demands...)
	extra := removal.Demands[0]
	extra.Scope, extra.Destination = string(observedAlias.Scope()), observedAlias.AggregateRoot().String()
	removal.Demands = append(removal.Demands, extra)
	bad.Removal = &removal
	copy := capture.fixture
	copy.record.DocumentGroups = []recoveryDocumentGroup{bad}
	content, err := marshalNativeJournalRecord(t.Context(), copy.record, copy.before, copy.after, copy.codec)
	if err != nil {
		t.Fatalf("extra alias fixture failed structural admission rather than semantic coverage: %v", err)
	}
	decoded, err := decodeNativeJournalRecord(t.Context(), content, copy.codec)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := decoded.prepareDocuments(t.Context(), []journaldocument.Comparison{comparison}, copy.read.limits, copy.read.codecs); err == nil || result.groups != nil {
		t.Fatal("semantic preparation accepted removal debt outside its actual selected frontier")
	}
}

func TestNativeSelectedLastKeyReleasePreservesExistingRoot(t *testing.T) {
	for _, content := range []string{`{"model":true}`, `{"model":true,"foreign":1}`} {
		t.Run(content, func(t *testing.T) {
			fixture := nativeJournalFixture(t)
			before := fixture.before.ManagedAggregates()[0]
			fixture.after = durable.EmptySnapshot()
			original := aggregate.ExistingDocument([]byte(content))
			edits := selectedCaptureEdits(t, fixture, original, []aggregate.ProjectionContract{before.Contribution().Contract()}, nil)
			capture := selectedCaptureFixture(t, fixture, original, edits, nil, journalnamespace.Roots{})
			group, comparison, err := prepareSelectedDocumentGroup(t.Context(), capture.namespace, capture.input, capture.subjects)
			if err != nil || !comparison.ExpectedDocument().Exists() || group.Expected.Kind != "file" || group.Removal != nil {
				t.Fatalf("last selected-key release acquired root deletion: %v", err)
			}
			var remaining map[string]json.RawMessage
			if err := json.Unmarshal(comparison.ExpectedDocument().Content(), &remaining); err != nil {
				t.Fatal(err)
			}
			if _, retained := remaining["model"]; retained {
				t.Fatal("actual edit did not release the selected key")
			}
			capture.requireCompleteStage(t, group, comparison)
		})
	}
}

func TestNativeRemovalUsesExecutableHostHashNotArtifactDigest(t *testing.T) {
	document := aggregate.ExistingDocument([]byte("#!/bin/sh\nexit 0\n"))
	mode := recovery.NewPermissionMode(0o700)
	states, err := journaldocument.RemovalStates(document, mode, aggregate.AbsentDocument(), nil)
	if err != nil || len(states) != 1 {
		t.Fatalf("executable before-state lost: %v", err)
	}
	before, present := states[0].Before()
	if !present || before.ContentHash != string(artifact.HashFileContentWithExecutable(document.Content(), true)) || before.ContentHash == string(artifact.HashFileContent(document.Content())) {
		t.Fatal("removal basis substituted the private non-executable artifact hash")
	}
}

func TestNativeRemovalWireRoundtripAndSingularShapeRefusal(t *testing.T) {
	fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeProject, "create", nil, []byte("created"))
	group, _, err := prepareWholeDocumentGroup(t.Context(), fixture.namespace, fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(group)
	if err != nil {
		t.Fatal(err)
	}
	var decoded recoveryDocumentGroup
	if err := json.Unmarshal(content, &decoded); err != nil {
		t.Fatal(err)
	}
	actual, err := decoded.Removal.canonical()
	if err != nil {
		t.Fatal(err)
	}
	expected, err := group.Removal.canonical()
	if err != nil || !actual.Demands().Equal(expected.Demands()) {
		t.Fatalf("wire discarded logical relations: %v", err)
	}
	physical, err := expected.PhysicalIntent()
	if err != nil {
		t.Fatal(err)
	}
	legacyShape, err := persistedRecoveryRemovalIntents([]recovery.RemovalIntent{physical})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		t.Fatal(err)
	}
	fields["removal"] = nativeRecordJSON(t, legacyShape[0])
	if err := json.Unmarshal(nativeRecordJSON(t, fields), &decoded); err == nil {
		t.Fatal("native group silently reinterpreted the obsolete one-relation shape")
	}
}

func TestNativeRemovalQualifiedReadRejectsStaleCleanupParentBeforeImages(t *testing.T) {
	fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeProject, "remove", []byte("before"), nil)
	group, _, err := prepareWholeDocumentGroup(t.Context(), fixture.namespace, fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	other := nativeNamespaceRoot(t, t.TempDir())
	authority, err := other.Root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	otherProvenance, err := authority.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	group.Removal.Namespace.ParentProvenance.ObjectFingerprint = otherProvenance.ObjectFingerprint()
	if err := group.validateRemoval(); err != nil {
		t.Fatalf("stale namespace fixture is not syntactically valid: %v", err)
	}
	manifestAuthority, err := fixture.roots.Manifest.Root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := manifestAuthority.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	record := fixture.envelope.record
	record.ManifestRootProvenance, record.DocumentGroups = persistedNativeRoot(manifest), []recoveryDocumentGroup{group}
	journal := nativeJournalSnapshots{record: record, owner: fixture.input.owner, before: fixture.envelope.before, after: fixture.envelope.after}
	options := fixture.envelope.read
	options.filesystem, options.budget = fixture.reader, imageBudget(t)
	reads := fixture.reader.imageReads
	result, err := journal.loadQualifiedDocuments(t.Context(), options, fixture.roots)
	if err == nil || result.namespaces != nil || fixture.reader.imageReads != reads {
		t.Fatal("stale cleanup parent reached artifact reads or usable qualified groups")
	}
}

func TestNativeRemovalDerivesWholeTransitionDebt(t *testing.T) {
	for _, action := range []string{"create", "remove", "replace", "record"} {
		t.Run(action, func(t *testing.T) {
			fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeGlobal, action, []byte("before"), []byte("after"))
			group, comparison, err := prepareWholeDocumentGroup(t.Context(), fixture.namespace, fixture.input)
			if err != nil {
				t.Fatal(err)
			}
			if action == "replace" || action == "record" {
				if group.Removal != nil {
					t.Fatal("in-place file work acquired removal authority")
				}
				return
			}
			if group.Removal == nil {
				t.Fatal("root transition omitted removal authority")
			}
			removal, err := group.Removal.canonical()
			if err != nil || removal.Demands().Len() != 1 {
				t.Fatalf("actual logical relation lost: %v", err)
			}
			states := removal.Demands().Demands()[0].States()
			if len(states) != 1 {
				t.Fatal("root transition invented state alternatives")
			}
			_, before := states[0].Before()
			_, expected := states[0].Expected()
			if before != (action == "remove") || expected != (action == "create") {
				t.Fatal("create rollback and forward removal debt were conflated")
			}
			images, err := preparedNativeDocumentImages(t.Context(), group, comparison, fixture.input.limits)
			if err != nil || group.validateRemovalImages(images) != nil {
				t.Fatalf("removal lost actual image facts: %v", err)
			}
			fixture.stageAndReconstruct(t, group, comparison)
			fixture.requireUnchangedHost(t)
		})
	}
}

func TestNativeRemovalPreservesLogicalAliasesWithOneNamespace(t *testing.T) {
	manifest := nativeNamespaceRoot(t, t.TempDir())
	pi := nativeNamespaceRoot(t, t.TempDir())
	if err := os.Symlink(pi.SelectedPath, filepath.Join(manifest.SelectedPath, ".pi")); err != nil {
		t.Fatal(err)
	}
	global := nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/settings.json")
	project := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/settings.json")
	sameRelation := nativeNamespaceAddress(t, target.TargetCodex, target.ScopeProject, ".pi/settings.json")
	aliases := []aggregate.DocumentAddress{global, project, sameRelation}
	budget, err := physicalwork.NewBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := journalnamespace.Observe(t.Context(), aliases, journalnamespace.Roots{Manifest: manifest, PiAgent: pi}, journalTestFilesystem(), budget)
	if err != nil {
		t.Fatal(err)
	}
	comparison, err := journaldocument.NewWholeComparison(t.Context(), journaldocument.ComparisonInput{Before: aggregate.AbsentDocument(), Expected: aggregate.ExistingDocument([]byte("created")), ExpectedMode: recovery.NewPermissionMode(0o600), Limits: configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}})
	if err != nil {
		t.Fatal(err)
	}
	record, err := captureDocumentRemoval(t.Context(), namespace, comparison, aliases, budget)
	if err != nil || record == nil {
		t.Fatalf("group cleanup did not capture once: %v", err)
	}
	removal, err := record.canonical()
	if err != nil || removal.Demands().Len() != 2 {
		t.Fatalf("target aliases were not one logical relation, or distinct relations collapsed: %v", err)
	}
	physical, err := removal.PhysicalIntent()
	if err != nil || !physical.Namespace().Names().Equal(removal.Namespace().Names()) {
		t.Fatalf("physical cleanup namespace split: %v", err)
	}
	otherOperation, err := physicalwork.NewBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	observedOnly, err := captureDocumentRemoval(t.Context(), namespace, comparison, []aggregate.DocumentAddress{global}, otherOperation)
	if err != nil {
		t.Fatal(err)
	}
	one, err := observedOnly.canonical()
	if err != nil || one.Demands().Len() != 1 {
		t.Fatalf("unmodified namespace aliases gained removal authority: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(pi.SelectedPath, "settings.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("capture created a host document: %v", err)
	}
}

func TestNativeRemovalRefusesCancellationAndUnallocatedNamespaceWork(t *testing.T) {
	fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeProject, "create", nil, []byte("created"))
	comparison, err := PrepareWholeDocument(t.Context(), WholeDocumentPreparationInput{Before: aggregate.AbsentDocument(), Limits: fixture.input.limits}, fixture.input.mutation, fixture.input.payloads)
	if err != nil {
		t.Fatal(err)
	}
	relations := fixture.namespace.Addresses()
	zero := imageBudget(t)
	if record, err := captureDocumentRemoval(t.Context(), fixture.namespace, comparison, relations, zero); err == nil || record != nil {
		t.Fatal("capture reset an unallocated namespace budget")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if record, err := captureDocumentRemoval(ctx, fixture.namespace, comparison, relations, fixture.input.budget); !errors.Is(err, context.Canceled) || record != nil {
		t.Fatalf("cancellation exposed removal authority: %v", err)
	}
}

func TestNativeRemovalChecksActualImageHashAndMode(t *testing.T) {
	fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeProject, "remove", []byte("before"), nil)
	group, comparison, err := prepareWholeDocumentGroup(t.Context(), fixture.namespace, fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	images, err := preparedNativeDocumentImages(t.Context(), group, comparison, fixture.input.limits)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"hash", "mode", "phase", "missing", "namespace"} {
		t.Run(change, func(t *testing.T) {
			bad := group
			copy := *group.Removal
			copy.Demands = append([]recoveryDocumentRemovalDemand(nil), copy.Demands...)
			copy.States = append([]recoveryRemovalState(nil), copy.States...)
			before := *copy.States[0].Before
			copy.States[0].Before = &before
			bad.Removal = &copy
			switch change {
			case "hash":
				before.ContentHash = string(artifact.HashFileContent([]byte("different")))
			case "mode":
				before.PathMode = recovery.NewPermissionMode(0o700)
			case "phase":
				copy.States[0] = recoveryRemovalState{ExpectedAfter: &recoveryExpectedPathDTO{Existed: true, Kind: recovery.PathKindFile, ContentHash: before.ContentHash, PathMode: before.PathMode}}
			case "missing":
				bad.Removal = nil
			case "namespace":
				parent := *copy.Namespace.ParentProvenance
				parent.PhysicalRoot = filepath.Dir(parent.PhysicalRoot)
				copy.Namespace.ParentProvenance = &parent
			}
			if err := bad.validateRemovalImages(images); err == nil {
				t.Fatal("invalid physical removal basis admitted")
			}
		})
	}
}
