package selecteddoc

import (
	"path/filepath"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/observe/relation/opencodeorder"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	opencodeconfig "github.com/isty2e/daem/internal/realization/configrelation/opencode"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestCoalescedArrayEditStillVerifiesEachOrigin(t *testing.T) {
	doc, err := opencodeorder.NewDocument(opencodeconfig.ConfigTUI, filepath.Join(t.TempDir(), "tui.json"), []byte(`{"plugin":["a@1","keep@1"]}`), true)
	if err != nil {
		t.Fatal(err)
	}
	capability, _ := profile.Profile(target.TargetOpenCode).ExtensionOrder(desiredextension.CarrierOpenCodePlugin, target.ScopeGlobal)
	expected, err := hostrelation.NewExpectedRelation("a@1", "host-relation:v1:a")
	if err != nil {
		t.Fatal(err)
	}
	var edits []SelectedDocumentEdit
	for _, name := range []string{"first", "second"} {
		subject, err := topology.NewSubjectID(topology.SubjectHostRelation, "opencode.plugin-carrier", name)
		if err != nil {
			t.Fatal(err)
		}
		key, err := observerelation.NewCorrelationKey(subject, expected)
		if err != nil {
			t.Fatal(err)
		}
		retiring, err := opencodeorder.NewScopedRelation(key, target.ScopeGlobal)
		if err != nil {
			t.Fatal(err)
		}
		view, err := opencodeorder.ReconstructHandoffRetirement(capability, doc, nil, []opencodeorder.ScopedRelation{retiring}, observerelation.OrderHandoffs{})
		if err != nil {
			t.Fatal(err)
		}
		edit, err := NewOpenCodeDocumentEdit(view)
		if err != nil {
			t.Fatal(err)
		}
		edits = append(edits, edit)
	}
	original := edits[0].Original()
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	completed, err := CompleteSelectedImage(t.Context(), original, limits, nil, edits)
	if err != nil || len(completed.Sequences()) != 1 {
		t.Fatalf("expected one physical edit: %v", err)
	}
	edits[1].origin = edits[0].origin
	if result, err := CompleteSelectedImage(t.Context(), original, limits, nil, edits); err == nil || result.Validate() == nil {
		t.Fatal("completed a coalesced edit with a mismatched origin")
	}
}
