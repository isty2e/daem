package configrelation

import (
	"path/filepath"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/target"
)

func TestNewRemovalPlanSelectsExactOpenCodeAuthority(t *testing.T) {
	root := t.TempDir()
	server := filepath.Join(root, "opencode.jsonc")
	tui := filepath.Join(root, "tui.json")
	plan, err := NewRemovalPlan(RemovalInput{
		Target:  target.TargetOpenCode,
		Scope:   target.ScopeProject,
		Carrier: desiredextension.CarrierOpenCodePlugin,
		Source:  "@acme/remove",
		AuthorityPaths: []observerelation.AuthorityPath{
			authorityPath(t, filepath.Join(root, "ignored.json"), target.TargetClaudeCode, target.ScopeProject),
			authorityPath(t, server, target.TargetOpenCode, target.ScopeProject),
			authorityPath(t, tui, target.TargetOpenCode, target.ScopeProject),
		},
	})
	if err != nil {
		t.Fatalf("NewRemovalPlan: %v", err)
	}
	if plan.kind != removalKindOpenCodePlugin ||
		plan.target != target.TargetOpenCode ||
		plan.scope != target.ScopeProject ||
		plan.source != "@acme/remove" ||
		len(plan.paths) != 2 ||
		plan.paths[0] != server ||
		plan.paths[1] != tui {
		t.Fatalf("plan = %#v", plan)
	}
	if _, err := plan.PhysicalAuthority(); err != nil {
		t.Fatalf("PhysicalAuthority: %v", err)
	}
}

func TestNewRemovalPlanRejectsIncompleteAmbiguousOrForeignAuthority(t *testing.T) {
	root := t.TempDir()
	server := authorityPath(
		t,
		filepath.Join(root, "opencode.json"),
		target.TargetOpenCode,
		target.ScopeGlobal,
	)
	tui := authorityPath(
		t,
		filepath.Join(root, "tui.jsonc"),
		target.TargetOpenCode,
		target.ScopeGlobal,
	)
	for _, test := range []struct {
		name  string
		paths []observerelation.AuthorityPath
	}{
		{name: "missing TUI", paths: []observerelation.AuthorityPath{server}},
		{
			name: "foreign basename",
			paths: []observerelation.AuthorityPath{
				server,
				authorityPath(t, filepath.Join(root, "settings.json"), target.TargetOpenCode, target.ScopeGlobal),
			},
		},
		{
			name: "duplicate server",
			paths: []observerelation.AuthorityPath{
				server,
				authorityPath(t, filepath.Join(root, "opencode.jsonc"), target.TargetOpenCode, target.ScopeGlobal),
			},
		},
		{
			name: "different roots",
			paths: []observerelation.AuthorityPath{
				server,
				authorityPath(t, filepath.Join(root, "other", "tui.json"), target.TargetOpenCode, target.ScopeGlobal),
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewRemovalPlan(RemovalInput{
				Target:         target.TargetOpenCode,
				Scope:          target.ScopeGlobal,
				Carrier:        desiredextension.CarrierOpenCodePlugin,
				Source:         "@acme/remove",
				AuthorityPaths: test.paths,
			})
			if err == nil {
				t.Fatal("NewRemovalPlan accepted incomplete or ambiguous authority")
			}
		})
	}

	if _, err := NewRemovalPlan(RemovalInput{
		Target:         target.TargetOpenCode,
		Scope:          target.ScopeGlobal,
		Carrier:        desiredextension.CarrierClaudeCodePlugin,
		Source:         "@acme/remove",
		AuthorityPaths: []observerelation.AuthorityPath{server, tui},
	}); err == nil {
		t.Fatal("NewRemovalPlan accepted an unadmitted target/carrier pair")
	}
}

func authorityPath(
	t *testing.T,
	path string,
	selectedTarget target.Target,
	scope target.Scope,
) observerelation.AuthorityPath {
	t.Helper()
	authority, err := observerelation.NewAuthorityPath(path, selectedTarget, scope)
	if err != nil {
		t.Fatal(err)
	}
	return authority
}
