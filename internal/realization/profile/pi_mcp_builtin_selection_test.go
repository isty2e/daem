package profile

import (
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestPiMCPBuiltinSelectionRetainsUnobservedEvidenceAndDecisiveOverrides(t *testing.T) {
	for _, test := range []struct {
		name            string
		global, project []string
		globalState     piMCPBuiltinState
		projectState    piMCPBuiltinState
	}{
		{"observed defaults", nil, nil, piMCPBuiltinEnabled, piMCPBuiltinEnabled},
		{"unmodelled exclusion", []string{"!{builtin:mcp,builtin:other}"}, nil, piMCPBuiltinUnobserved, piMCPBuiltinUnobserved},
		{"uncertain exclusion then force include", []string{"!{builtin:mcp,builtin:other}", "+builtin:mcp"}, nil, piMCPBuiltinEnabled, piMCPBuiltinEnabled},
		{"force include then uncertain exclusion", []string{"+builtin:mcp", "!{builtin:mcp,builtin:other}"}, nil, piMCPBuiltinEnabled, piMCPBuiltinEnabled},
		{"uncertain force exclude", []string{"+builtin:mcp", "-../builtin:mcp"}, nil, piMCPBuiltinUnobserved, piMCPBuiltinUnobserved},
		{"known exclusion dominates uncertainty", []string{"-../builtin:mcp", "-builtin:mcp"}, nil, piMCPBuiltinDisabled, piMCPBuiltinDisabled},
		{"project resolves global uncertainty", []string{"!{builtin:mcp,builtin:other}"}, []string{"+builtin:mcp"}, piMCPBuiltinUnobserved, piMCPBuiltinEnabled},
		{"project uncertainty follows force include", nil, []string{"+builtin:mcp", "!{builtin:mcp,builtin:other}"}, piMCPBuiltinEnabled, piMCPBuiltinUnobserved},
		{"project force include follows uncertainty", nil, []string{"!{builtin:mcp,builtin:other}", "+builtin:mcp"}, piMCPBuiltinEnabled, piMCPBuiltinEnabled},
		{"already disabled remains disabled", []string{"-builtin:mcp"}, []string{"!{builtin:mcp,builtin:other}"}, piMCPBuiltinDisabled, piMCPBuiltinDisabled},
		{"already enabled remains enabled", nil, []string{"+../builtin:mcp"}, piMCPBuiltinEnabled, piMCPBuiltinEnabled},
		{"plain entries are not selectors", []string{"-builtin:mcp", "builtin:mcp"}, []string{"builtin:mcp"}, piMCPBuiltinDisabled, piMCPBuiltinDisabled},
	} {
		t.Run(test.name, func(t *testing.T) {
			selection := ResolvePiMCPBuiltinSelection(test.global, test.project)
			if selection.global != test.globalState || selection.project != test.projectState {
				t.Fatalf("selection = %#v; want %v/%v", selection, test.globalState, test.projectState)
			}
			for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
				accepted := test.projectState == piMCPBuiltinEnabled && (scope == target.ScopeProject || test.globalState == piMCPBuiltinEnabled)
				if err := selection.requireEnabled(scope); (err == nil) != accepted {
					t.Fatalf("%s accepted=%t; want %t: %v", scope, err == nil, accepted, err)
				}
			}
		})
	}
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		if err := (PiMCPBuiltinSelection{}).requireEnabled(scope); err == nil {
			t.Fatalf("zero-value selection qualified %s", scope)
		}
	}
}
