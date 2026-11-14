package configuration

import (
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestRecordedPoliciesDoNotDependOnTheCurrentPlacementCatalog(t *testing.T) {
	var inputs []AliasInput
	var bindings []AliasBinding
	for _, test := range []struct {
		host  target.Target
		scope target.Scope
		path  string
	}{
		{target.TargetPi, target.ScopeGlobal, "@pi-agent/custom.json"},
		{target.TargetCodex, target.ScopeProject, ".codex/custom.toml"},
		{target.TargetClaudeCode, target.ScopeGlobal, aggregate.ClaudeGlobalMCPConfigPath},
		{target.TargetOpenCode, target.ScopeProject, ".opencode/tui.jsonc"},
		{target.TargetAntigravityCLI, target.ScopeGlobal, "~/.gemini/antigravity-cli/settings.json"},
	} {
		destination, err := output.Parse(test.path)
		if err != nil {
			t.Fatal(err)
		}
		input := AliasInput{Target: test.host, Scope: test.scope, Destination: destination}
		binding, err := RootBindingForAlias(input)
		if err != nil {
			t.Fatal(err)
		}
		inputs, bindings = append(inputs, input), append(bindings, binding)
	}
	oldLayouts, oldDedicated := nativeLayouts, dedicatedDocumentAliases
	nativeLayouts, dedicatedDocumentAliases = nil, nil
	t.Cleanup(func() { nativeLayouts, dedicatedDocumentAliases = oldLayouts, oldDedicated })
	for index, input := range inputs {
		expected := bindings[index]
		actual, err := AdmitRecordedAliasBinding(input, expected.RootReference(), expected.Coordinate(), expected.PolicyID())
		if err != nil || actual != expected {
			t.Fatalf("retained policy was reinterpreted through current selection: %v", err)
		}
		if _, err := RootBindingForAlias(input); err == nil {
			t.Fatal("fixture did not remove the current placement selection")
		}
	}
}

func TestRecordedPointPolicyDoesNotReinterpretTheDirectoryRevision(t *testing.T) {
	destination, err := output.Parse(aggregate.ClaudeGlobalMCPConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	input := AliasInput{Target: target.TargetClaudeCode, Scope: target.ScopeGlobal, Destination: destination}
	point, err := output.NewRootReference("claude-account", "claude-account-point-v2")
	if err != nil {
		t.Fatal(err)
	}
	binding, err := AdmitRecordedAliasBinding(input, point, PointSelf(), "claude-account-carrier-v2")
	if err != nil || binding.Coordinate() != PointSelf() {
		t.Fatal("recorded point policy was not admitted independently of current placement", err)
	}
	current, err := RootBindingForAlias(input)
	if err != nil || current != binding {
		t.Fatal("current point binding differs from its recorded policy", current, err)
	}
	legacyRoot, err := output.NewRootReference("claude-account", "claude-account-location-v1")
	if err != nil {
		t.Fatal(err)
	}
	child, err := NewDirectoryChild(".claude.json")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := AdmitRecordedAliasBinding(input, legacyRoot, child, "claude-account-carrier-v1")
	if err != nil || legacy.Coordinate() != child {
		t.Fatal("retained directory policy was changed by point activation", legacy, err)
	}
	if _, err := AdmitRecordedAliasBinding(input, legacy.RootReference(), PointSelf(), legacy.PolicyID()); err == nil {
		t.Fatal("an old directory binding became a file point")
	}
}
