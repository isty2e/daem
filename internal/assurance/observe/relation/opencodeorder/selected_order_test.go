package opencodeorder_test

import (
	"path/filepath"
	"slices"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/observe/relation/opencodeorder"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/aggregate"
	opencodeconfig "github.com/isty2e/daem/internal/realization/configrelation/opencode"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestReconstructHandoffRetirementUsesOneSuppliedDocument(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "tui.json")
	const original = `{/* JSONC in a .json candidate */ "plugin":["keep@1","./local.ts"]}`
	content := []byte(original)
	document, err := opencodeorder.NewDocument(opencodeconfig.ConfigTUI, path, content, true)
	if err != nil {
		t.Fatal(err)
	}
	clear(content)
	capability, ok := profile.Profile(target.TargetOpenCode).ExtensionOrder(desiredextension.CarrierOpenCodePlugin, target.ScopeGlobal)
	if !ok {
		t.Fatal("missing OpenCode capability")
	}
	if _, err := opencodeorder.ReconstructHandoffRetirement(capability, opencodeorder.Document{}, nil, nil, observerelation.OrderHandoffs{}); err == nil {
		t.Fatal("zero document was accepted for retirement")
	}
	subject, err := topology.NewSubjectID(topology.SubjectHostRelation, "opencode.plugin-carrier", "local")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := hostrelation.NewExpectedRelation("./local.ts", "host-relation:v1:local")
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
	view, err := opencodeorder.ReconstructHandoffRetirement(capability, document, nil, []opencodeorder.ScopedRelation{retiring}, observerelation.OrderHandoffs{})
	if err != nil {
		t.Fatal(err)
	}
	selected := view.SelectedSequence()
	if _, ordered := selected.Constraint(); ordered {
		t.Fatal("single-document retirement invented a desired order")
	}
	if got := selected.Original().SequenceID(); got != "opencode:global:tui.json.plugins" {
		t.Fatalf("retirement selected sequence %q", got)
	}
	if got := selected.RemovedSlots(); !slices.Equal(got, []int{1}) {
		t.Fatalf("retirement selected slots %v", got)
	}
	wantIdentity, err := opencodeconfig.HostLoadIdentity("./local.ts", path)
	if err != nil {
		t.Fatal(err)
	}
	if view.Path() != path || string(view.OriginalDocument().Content()) != original {
		t.Fatal("loaded image lost its original path or bytes")
	}
	if got := selected.Original().OrderedRows()[1].HostLoadIdentity(); string(got) != wantIdentity {
		t.Fatalf("declaring-file identity = %q, want %q", got, wantIdentity)
	}
	if err := view.VerifySelectedExpected(selected, aggregate.ExistingDocument([]byte(`{"plugin":["keep@1"],"theme":"new"}`))); err != nil {
		t.Fatal(err)
	}
	if err := view.VerifySelectedExpected(selected, view.OriginalDocument()); err == nil {
		t.Fatal("completed image retained the selected retirement")
	}
}
