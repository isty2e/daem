package relationhost

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe/opencodeplugin"
	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	opencodeconfig "github.com/isty2e/daem/internal/realization/configrelation/opencode"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	localcontext "github.com/isty2e/daem/internal/topology/extension"
)

func selectedRetirementKey(t *testing.T, namespace, name, source string) observerelation.CorrelationKey {
	t.Helper()
	subject, err := topology.NewSubjectID(topology.SubjectHostRelation, namespace, name)
	if err != nil {
		t.Fatal(err)
	}
	key, err := hostrelation.NewSubjectKey(source)
	if err != nil {
		t.Fatal(err)
	}
	managed, err := hostrelation.NewManagedInstanceKey("host-relation:v1:" + name)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := hostrelation.NewExpectedRelation(key, managed)
	if err != nil {
		t.Fatal(err)
	}
	correlation, err := observerelation.NewCorrelationKey(subject, expected)
	if err != nil {
		t.Fatal(err)
	}
	return correlation
}

func TestSelectedPiRetirementDoesNotChangeHistoricalObservation(t *testing.T) {
	root := t.TempDir()
	base := selectedPiFixture(t, root, "pi.package-carrier", `{"packages":[{"source":"npm:b@1","autoload":false},"npm:r@1","npm:foreign@1","npm:a@1"],"model":"slow"}`)
	edits, err := base.SelectedEdits(nil)
	if err != nil {
		t.Fatal(err)
	}
	input, err := base.pi.CaptureInput()
	if err != nil {
		t.Fatal(err)
	}
	context, explicit := input.Relations[0].SourceContext()
	if !explicit {
		t.Fatal("fixture is not explicitly scoped")
	}
	retiring := func(name, source string) pipackage.ScopedRelation {
		relation, err := pipackage.NewScopedRelationWithContext(selectedRetirementKey(t, "pi.package-carrier", name, source), target.ScopeProject, context)
		if err != nil {
			t.Fatal(err)
		}
		return relation
	}
	candidate, exists := base.pi.Candidate()
	view, err := base.pi.WithRetiringRelations([]pipackage.ScopedRelation{retiring("r", "npm:r@1")})
	if err != nil {
		t.Fatal("retirement correlation ran the removal-free normalizer", err)
	}
	selected, err := NewPiDocumentEdit(view)
	if err != nil || edits[0].SameIntent(selected) {
		t.Fatal("retirement did not enter its own factory-bound intent", err)
	}
	completed, err := CompleteSelectedImage(t.Context(), selected.Original(), configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}, nil, []SelectedDocumentEdit{selected})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"packages":["npm:a@1","npm:foreign@1",{"source":"npm:b@1","autoload":false}],"model":"slow"}`
	if string(completed.Expected().Content()) != want || len(view.SelectedSequence().RemovedSlots()) != 1 || view.SelectedSequence().RemovedSlots()[0] != 1 {
		t.Fatal("retirement lost exact positional removal, tuple payload or foreign order")
	}
	after, afterExists := base.pi.Candidate()
	if exists != afterExists || !bytes.Equal(candidate, after) || base.pi.Validate() != nil {
		t.Fatal("retirement mutated the historical16 observation")
	}
	for _, source := range []string{"npm:a@1", "npm:a@2"} {
		if _, err := base.pi.WithRetiringRelations([]pipackage.ScopedRelation{retiring("renamed", source)}); err == nil {
			t.Fatal("retirement stole a desired source/load identity", source)
		}
	}
	for name, relations := range map[string][]pipackage.ScopedRelation{
		"duplicate subject":       {retiring("r", "npm:r@1"), retiring("r", "npm:other@1")},
		"duplicate source":        {retiring("r", "npm:r@1"), retiring("other", "npm:r@1")},
		"duplicate load identity": {retiring("r", "npm:r@1"), retiring("other", "npm:r@2")},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := base.pi.WithRetiringRelations(relations); err == nil {
				t.Fatal("ambiguous Pi retirement correlations were accepted")
			}
		})
	}
	absent, err := base.pi.WithRetiringRelations([]pipackage.ScopedRelation{retiring("absent", "npm:absent@1")})
	if err != nil || len(absent.SelectedSequence().RemovedSlots()) != 0 || view.SameSelection(absent) {
		t.Fatal("pending absent retirement or intent comparison is incorrect", err)
	}
	wrongContext, err := localcontext.NewLocalSourceContext(filepath.Join(root, "different"), context.HomeRoot())
	if err != nil {
		t.Fatal(err)
	}
	relation, err := pipackage.NewScopedRelationWithContext(selectedRetirementKey(t, "pi.package-carrier", "r", "npm:r@1"), target.ScopeProject, wrongContext)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.pi.WithRetiringRelations([]pipackage.ScopedRelation{relation}); err == nil {
		t.Fatal("retirement changed its selected command context")
	}

	pureOriginal := aggregate.ExistingDocument([]byte(`{"packages":["npm:r@1"],"model":"slow"}`))
	pure, err := pipackage.ReconstructRetirement(pipackage.LoadedRetirementInput{Capability: input.Capability, SettingsContext: input.SettingsContext, CommandContext: context, Document: pureOriginal, Relations: []pipackage.ScopedRelation{retiring("r", "npm:r@1")}})
	if err != nil {
		t.Fatal("pure retirement required an invented desired constraint", err)
	}
	selected, err = NewPiDocumentEdit(pure)
	if err != nil {
		t.Fatal(err)
	}
	completed, err = CompleteSelectedImage(t.Context(), pureOriginal, configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}, nil, []SelectedDocumentEdit{selected})
	_, ordered := pure.SelectedSequence().Constraint()
	if err != nil || ordered || !completed.Expected().Exists() || string(completed.Expected().Content()) != `{"packages":[],"model":"slow"}` {
		t.Fatal("last-row retirement invented parent/document deletion", err)
	}
	absentOnly, err := pipackage.ReconstructRetirement(pipackage.LoadedRetirementInput{Capability: input.Capability, SettingsContext: input.SettingsContext, CommandContext: context, Document: aggregate.AbsentDocument(), Relations: []pipackage.ScopedRelation{retiring("r", "npm:r@1")}})
	if err != nil || absentOnly.SelectedSequence().Changed() {
		t.Fatal("pending removal recreated an absent document", err)
	}
}

func TestSelectedOpenCodeRetirementCompletesEachPhysicalDocument(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "global")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "{\n// retained\n\"plugin\":[[\"b@1\",{\"option\":true}],\"r@1\",\"foreign@1\",\"a@1\"],\"theme\":\"dark\"\n}\n"
	for _, name := range []string{"opencode.jsonc", "tui.jsonc"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	capability, admitted := profile.Profile(target.TargetOpenCode).ExtensionOrder(desiredextension.CarrierOpenCodePlugin, target.ScopeGlobal)
	if !admitted {
		t.Fatal("OpenCode capability unavailable")
	}
	var relations []opencodeplugin.ScopedRelation
	var members []hostrelation.RelationOrderMember
	for _, name := range []string{"a", "b"} {
		key := selectedRetirementKey(t, "opencode.plugin-carrier", name, name+"@1")
		relation, err := opencodeplugin.NewScopedRelation(key, target.ScopeGlobal)
		if err != nil {
			t.Fatal(err)
		}
		load, err := opencodeconfig.HostLoadIdentity(name+"@1", filepath.Join(directory, "opencode.json"))
		if err != nil {
			t.Fatal(err)
		}
		member, err := hostrelation.NewRelationOrderMember(key.Subject(), hostrelation.HostLoadIdentity(load))
		if err != nil {
			t.Fatal(err)
		}
		relations, members = append(relations, relation), append(members, member)
	}
	constraint, err := hostrelation.NewRelationOrderConstraint(capability.ClassID(), capability.MemberIdentityContract(), capability.RuntimeMeaning(), members)
	if err != nil {
		t.Fatal(err)
	}
	base, err := opencodeplugin.ReadOrder(opencodeplugin.OrderInput{Inventory: opencodeplugin.InventoryInput{ManifestRoot: root, ConfigRoot: directory, Scope: target.ScopeGlobal}, Constraint: constraint, Relations: relations})
	if err != nil {
		t.Fatal(err)
	}
	edits, err := (OrderObservation{openCode: &base}).SelectedEdits(nil)
	if err != nil || len(edits) != 2 {
		t.Fatal("fixture did not retain both physical documents", err)
	}
	retiring, err := opencodeplugin.NewScopedRelation(selectedRetirementKey(t, "opencode.plugin-carrier", "r", "r@1"), target.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := opencodeplugin.ReadInventory(opencodeplugin.InventoryInput{ManifestRoot: root, ConfigRoot: directory, Scope: target.ScopeGlobal})
	if err != nil {
		t.Fatal(err)
	}
	for index, document := range base.Documents() {
		if index == 0 {
			relationFor := func(name, source string, scope target.Scope) opencodeplugin.ScopedRelation {
				relation, err := opencodeplugin.NewScopedRelation(selectedRetirementKey(t, "opencode.plugin-carrier", name, source), scope)
				if err != nil {
					t.Fatal(err)
				}
				return relation
			}
			for name, relations := range map[string][]opencodeplugin.ScopedRelation{
				"desired source":          {relationFor("renamed", "a@1", target.ScopeGlobal)},
				"desired load identity":   {relationFor("renamed", "a@2", target.ScopeGlobal)},
				"wrong scope":             {relationFor("r", "r@1", target.ScopeProject)},
				"duplicate subject":       {retiring, relationFor("r", "other@1", target.ScopeGlobal)},
				"duplicate source":        {retiring, relationFor("other", "r@1", target.ScopeGlobal)},
				"duplicate load identity": {retiring, relationFor("other", "r@2", target.ScopeGlobal)},
			} {
				t.Run(name, func(t *testing.T) {
					if _, err := document.WithRetiringRelations(relations); err == nil {
						t.Fatal("OpenCode retirement stole desired membership or admitted ambiguous correlations")
					}
				})
			}
			for name, desired := range map[string][]opencodeplugin.ScopedRelation{
				"same source after rename": {relationFor("renamed", "r@1", target.ScopeGlobal)},
				"same load after rename":   {relationFor("renamed", "r@2", target.ScopeGlobal)},
				"retained subject":         {relationFor("r", "different@1", target.ScopeGlobal)},
			} {
				t.Run("classless/"+name, func(t *testing.T) {
					if _, err := opencodeplugin.ReconstructRetirement(capability, inventory.Documents()[index], desired, []opencodeplugin.ScopedRelation{retiring}); err == nil {
						t.Fatal("classless retirement consumed a still-desired relation")
					}
				})
			}
		}
		view, err := document.WithRetiringRelations([]opencodeplugin.ScopedRelation{retiring})
		if err != nil {
			t.Fatal(err)
		}
		selected, err := NewOpenCodeDocumentEdit(view)
		if err != nil || edits[index].SameIntent(selected) {
			t.Fatal("retirement did not enter its own factory-bound intent", err)
		}
		completed, err := CompleteSelectedImage(t.Context(), selected.Original(), configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}, nil, []SelectedDocumentEdit{selected})
		if err != nil {
			t.Fatal(err)
		}
		result := string(completed.Expected().Content())
		if !strings.Contains(result, `"plugin":["a@1","foreign@1",["b@1",{"option":true}]]`) || !strings.Contains(result, "// retained") || !strings.Contains(result, `"theme":"dark"`) {
			t.Fatal("physical completion lost retirement, tuple/comment or sibling content", result)
		}
		if document.Validate() != nil {
			t.Fatal("retirement mutated the base OpenCode observation")
		}

		pure, err := opencodeplugin.ReconstructRetirement(capability, inventory.Documents()[index], relations[:1], []opencodeplugin.ScopedRelation{retiring})
		if err != nil {
			t.Fatal(err)
		}
		selected, err = NewOpenCodeDocumentEdit(pure)
		if err != nil {
			t.Fatal(err)
		}
		completed, err = CompleteSelectedImage(t.Context(), selected.Original(), configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}, nil, []SelectedDocumentEdit{selected})
		_, ordered := pure.SelectedSequence().Constraint()
		if err != nil || ordered || !strings.Contains(string(completed.Expected().Content()), `"plugin":[["b@1",{"option":true}],"foreign@1","a@1"]`) {
			t.Fatal("pure retirement imposed a desired permutation", err)
		}
		unprotected, err := opencodeplugin.ReconstructRetirement(capability, inventory.Documents()[index], nil, []opencodeplugin.ScopedRelation{retiring})
		if err != nil || pure.SameSelection(unprotected) {
			t.Fatal("desired retirement protection disappeared from selection intent", err)
		}
	}
}
