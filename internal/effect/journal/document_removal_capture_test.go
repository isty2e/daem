package journal

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeRemovalQualifiedReadRejectsStaleCleanupParentBeforeImages(t *testing.T) {
	fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeProject, "remove", []byte("before"), nil)
	group, _, err := prepareWholeDocumentGroup(t.Context(), fixture.namespace, fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	published := fixture.envelope
	published.journal = fixture.recordForGroups(t, []journalrecord.SealedDocumentGroup{group})
	options := fixture.envelope.read
	options.Filesystem, options.Budget = fixture.reader, imageBudget(t)
	if _, err := journalqualification.LoadNamespaces(published.journal, t.Context(), options, fixture.roots); err != nil {
		t.Fatal("valid cleanup namespace did not qualify", err)
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
	journal := publishedNativeGroupVariant(t, published, func(group map[string]json.RawMessage) {
		var removal map[string]any
		if err := json.Unmarshal(group["removal"], &removal); err != nil {
			t.Fatal(err)
		}
		namespace := removal["namespace_authority"].(map[string]any)
		parent := namespace["parent_provenance"].(map[string]any)
		if parent["object_fingerprint"] == otherProvenance.ObjectFingerprint() {
			t.Fatal("stale namespace fixture did not change its incarnation")
		}
		parent["object_fingerprint"] = otherProvenance.ObjectFingerprint()
		group["removal"] = nativeRecordJSON(t, removal)
	})
	document, err := journal.DocumentAt(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, present, err := document.Removal(); err != nil || !present {
		t.Fatal("stale namespace fixture is not syntactically valid", err)
	}
	options.Budget = imageBudget(t)
	reads := fixture.reader.imageReads
	result, err := loadQualifiedDocuments(journal, t.Context(), options, fixture.roots)
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
			journal := fixture.recordForGroups(t, []journalrecord.SealedDocumentGroup{group})
			document, err := journal.DocumentAt(0)
			if err != nil {
				t.Fatal(err)
			}
			removal, present, err := document.Removal()
			if err != nil {
				t.Fatal(err)
			}
			if action == "replace" || action == "record" {
				if present {
					t.Fatal("in-place file work acquired removal authority")
				}
				return
			}
			if !present {
				t.Fatal("root transition omitted removal authority")
			}
			if removal.Demands().Len() != 1 {
				t.Fatal("actual logical relation lost")
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
			if _, err := journal.PrepareDocuments(t.Context(), []journaldocument.Comparison{comparison}, fixture.input.Limits(), fixture.envelope.read.Codecs); err != nil {
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
	removal := *record
	if removal.Demands().Len() != 2 {
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
	if observedOnly.Demands().Len() != 1 {
		t.Fatalf("unmodified namespace aliases gained removal authority: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(pi.SelectedPath, "settings.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("capture created a host document: %v", err)
	}
}

func TestNativeRemovalRefusesCancellationAndUnallocatedNamespaceWork(t *testing.T) {
	fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeProject, "create", nil, []byte("created"))
	comparison, err := PrepareWholeDocument(t.Context(), WholeDocumentPreparationInput{Before: aggregate.AbsentDocument(), Limits: fixture.input.Limits()}, fixture.input.mutation, fixture.input.payloads)
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
	if record, err := captureDocumentRemoval(ctx, fixture.namespace, comparison, relations, fixture.input.Budget()); !errors.Is(err, context.Canceled) || record != nil {
		t.Fatalf("cancellation exposed removal authority: %v", err)
	}
}
