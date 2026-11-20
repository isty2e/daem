package relationhost

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe/relation/piorder"

	"github.com/isty2e/daem/internal/assurance/observe/opencodeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	opencodeconfig "github.com/isty2e/daem/internal/realization/configrelation/opencode"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
)

func TestSelectedPiHandoffUsesExactSourcesAndKeepsOriginalCAS(t *testing.T) {
	base := selectedPiFixture(t, t.TempDir(), "pi.package-carrier", `{"packages":[{"source":"npm:b@1","autoload":false},"npm:foreign@1","npm:a@1"],"model":"slow"}`)
	input, err := base.pi.CaptureInput()
	if err != nil {
		t.Fatal(err)
	}
	context := input.Relations[0].SourceContext()
	var retiring []piorder.ScopedRelation
	var pairs []observerelation.OrderHandoffSpec
	for _, desired := range input.Relations {
		source := "npm:a@1"
		if desired.CorrelationKey().Subject().Key() == "a" {
			source = "npm:b@1"
		}
		key := selectedRetirementKey(t, "pi.package-carrier", desired.CorrelationKey().Subject().Key(), source)
		old, err := piorder.NewScopedRelation(key, target.ScopeProject, context)
		if err != nil {
			t.Fatal(err)
		}
		retiring = append(retiring, old)
		pairs = append(pairs, observerelation.OrderHandoffSpec{Before: key, After: desired.CorrelationKey()})
	}
	extra := selectedRetirementKey(t, "pi.package-carrier", "extra", "npm:foreign@2")
	foreign, err := piorder.NewScopedRelation(extra, target.ScopeProject, context)
	if err != nil {
		t.Fatal(err)
	}
	retiring = append(retiring, foreign)
	pairs = append(pairs, observerelation.OrderHandoffSpec{Before: extra, After: input.Relations[0].CorrelationKey()})
	incomplete, err := observerelation.NewOrderHandoffs(pairs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.pi.WithHandoffRetirements(retiring, incomplete); err == nil {
		t.Fatal("handoff removed another desired source without its acquisition association")
	}
	for _, old := range retiring[:2] {
		for _, desired := range input.Relations {
			if old.CorrelationKey().Subject() != desired.CorrelationKey().Subject() {
				pairs = append(pairs, observerelation.OrderHandoffSpec{Before: old.CorrelationKey(), After: desired.CorrelationKey()})
			}
		}
	}
	events, err := observerelation.NewOrderHandoffs(pairs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.pi.WithRetiringRelations(retiring); err == nil {
		t.Fatal("ordinary deletion admitted a still-desired source")
	}
	before, existed := base.pi.Candidate()
	view, err := base.pi.WithHandoffRetirements(retiring, events)
	if err != nil {
		t.Fatal(err)
	}
	edit, err := NewPiDocumentEdit(view)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := CompleteSelectedImage(t.Context(), edit.Original(), configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}, nil, []SelectedDocumentEdit{edit})
	if err != nil {
		t.Fatal(err)
	}
	if string(completed.Expected().Content()) != `{"packages":["npm:foreign@1"],"model":"slow"}` ||
		len(view.SelectedSequence().RemovedSlots()) != 2 || len(view.SelectedSequence().PrecedenceChanges()) != 0 {
		t.Fatal("handoff stole an equivalent foreign source or counted retiring rows as foreign precedence")
	}
	after, exists := base.pi.Candidate()
	if existed != exists || !bytes.Equal(before, after) || base.pi.Validate() != nil {
		t.Fatal("conditional retirement replaced the actual baseline")
	}
}

func TestSelectedOpenCodeHandoffRetiresEachPhysicalDocument(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "global")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	content := "{\n// retained\n\"plugin\":[[\"b@1\",{\"option\":true}],\"foreign@1\",\"a@1\"],\"theme\":\"dark\"\n}\n"
	for _, name := range []string{"opencode.jsonc", "tui.jsonc"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	capability, _ := profile.Profile(target.TargetOpenCode).ExtensionOrder(desiredextension.CarrierOpenCodePlugin, target.ScopeGlobal)
	var desired, retiring []opencodeplugin.ScopedRelation
	var members []hostrelation.RelationOrderMember
	var pairs []observerelation.OrderHandoffSpec
	for _, name := range []string{"a", "b"} {
		key := selectedRetirementKey(t, "opencode.plugin-carrier", name, name+"@1")
		relation, err := opencodeplugin.NewScopedRelation(key, target.ScopeGlobal)
		if err != nil {
			t.Fatal(err)
		}
		desired = append(desired, relation)
		load, err := opencodeconfig.HostLoadIdentity(name+"@1", filepath.Join(directory, "opencode.json"))
		if err != nil {
			t.Fatal(err)
		}
		member, err := hostrelation.NewRelationOrderMember(key.Subject(), hostrelation.HostLoadIdentity(load))
		if err != nil {
			t.Fatal(err)
		}
		members = append(members, member)
		source := "a@1"
		if name == "a" {
			source = "b@1"
		}
		oldKey := selectedRetirementKey(t, "opencode.plugin-carrier", name, source)
		old, err := opencodeplugin.NewScopedRelation(oldKey, target.ScopeGlobal)
		if err != nil {
			t.Fatal(err)
		}
		retiring = append(retiring, old)
		pairs = append(pairs, observerelation.OrderHandoffSpec{Before: oldKey, After: key})
	}
	constraint, err := hostrelation.NewRelationOrderConstraint(capability.ClassID(), capability.MemberIdentityContract(), capability.RuntimeMeaning(), members)
	if err != nil {
		t.Fatal(err)
	}
	base, err := opencodeplugin.ReadOrder(opencodeplugin.OrderInput{Inventory: opencodeplugin.InventoryInput{ManifestRoot: root, ConfigRoot: directory, Scope: target.ScopeGlobal}, Constraint: constraint, Relations: desired})
	if err != nil {
		t.Fatal(err)
	}
	incomplete, err := observerelation.NewOrderHandoffs(pairs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.Documents()[0].WithHandoffRetirements(retiring, incomplete); err == nil {
		t.Fatal("handoff bypassed an unrelated desired OpenCode source")
	}
	ownedPairs := append([]observerelation.OrderHandoffSpec(nil), pairs...)
	for _, old := range ownedPairs {
		for _, after := range ownedPairs {
			if old.Before.Subject() != after.After.Subject() {
				pairs = append(pairs, observerelation.OrderHandoffSpec{Before: old.Before, After: after.After})
			}
		}
	}
	events, err := observerelation.NewOrderHandoffs(pairs)
	if err != nil {
		t.Fatal(err)
	}
	if len(base.Documents()) != 2 {
		t.Fatal("fixture lost physical document membership")
	}
	for _, document := range base.Documents() {
		if _, err := document.WithRetiringRelations(retiring); err == nil {
			t.Fatal("ordinary deletion admitted a desired-source overlap")
		}
		view, err := document.WithHandoffRetirements(retiring, events)
		if err != nil {
			t.Fatal(err)
		}
		edit, err := NewOpenCodeDocumentEdit(view)
		if err != nil {
			t.Fatal(err)
		}
		completed, err := CompleteSelectedImage(t.Context(), edit.Original(), configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}, nil, []SelectedDocumentEdit{edit})
		if err != nil {
			t.Fatal(err)
		}
		want := "{\n// retained\n\"plugin\":[\"foreign@1\"],\"theme\":\"dark\"\n}\n"
		if string(completed.Expected().Content()) != want || len(view.SelectedSequence().RemovedSlots()) != 2 || len(view.SelectedSequence().PrecedenceChanges()) != 0 {
			t.Fatal("handoff lost per-document exact removal, comments or foreign precedence")
		}
	}
}
