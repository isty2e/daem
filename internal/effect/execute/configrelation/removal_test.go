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
	candidates := openCodeCandidateAuthorities(t, root, target.ScopeProject)
	plan, err := NewRemovalPlan(RemovalInput{
		Target:  target.TargetOpenCode,
		Scope:   target.ScopeProject,
		Carrier: desiredextension.CarrierOpenCodePlugin,
		Source:  "@acme/remove",
		AuthorityPaths: []observerelation.AuthorityPath{
			authorityPath(t, filepath.Join(root, "ignored.json"), target.TargetClaudeCode, target.ScopeProject),
			candidates[0],
			candidates[1],
			candidates[2],
			candidates[3],
		},
	})
	if err != nil {
		t.Fatalf("NewRemovalPlan: %v", err)
	}
	if plan.kind != removalKindOpenCodePlugin ||
		plan.target != target.TargetOpenCode ||
		plan.scope != target.ScopeProject ||
		plan.source != "@acme/remove" ||
		len(plan.paths) != 4 ||
		plan.paths[0] != filepath.Join(root, "opencode.json") ||
		plan.paths[1] != filepath.Join(root, "opencode.jsonc") ||
		plan.paths[2] != filepath.Join(root, "tui.json") ||
		plan.paths[3] != filepath.Join(root, "tui.jsonc") {
		t.Fatalf("plan = %#v", plan)
	}
	if _, err := plan.PhysicalAuthority(); err != nil {
		t.Fatalf("PhysicalAuthority: %v", err)
	}
}

func TestNewRemovalPlanRejectsIncompleteAmbiguousOrForeignAuthority(t *testing.T) {
	root := t.TempDir()
	complete := openCodeCandidateAuthorities(t, root, target.ScopeGlobal)
	for _, test := range []struct {
		name  string
		paths []observerelation.AuthorityPath
	}{
		{name: "missing candidate", paths: complete[:3]},
		{
			name: "foreign basename",
			paths: append(
				append([]observerelation.AuthorityPath(nil), complete[:3]...),
				authorityPath(t, filepath.Join(root, "settings.json"), target.TargetOpenCode, target.ScopeGlobal),
			),
		},
		{
			name:  "duplicate candidate",
			paths: append(append([]observerelation.AuthorityPath(nil), complete...), complete[0]),
		},
		{
			name: "different roots",
			paths: append(
				append([]observerelation.AuthorityPath(nil), complete[:3]...),
				authorityPath(t, filepath.Join(root, "other", "tui.jsonc"), target.TargetOpenCode, target.ScopeGlobal),
			),
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
		AuthorityPaths: complete,
	}); err == nil {
		t.Fatal("NewRemovalPlan accepted an unadmitted target/carrier pair")
	}
}

func TestNewRemovalPlanRejectsDurableSourceWithoutEffectAuthority(t *testing.T) {
	root := t.TempDir()
	_, err := NewRemovalPlan(RemovalInput{
		Target:         target.TargetOpenCode,
		Scope:          target.ScopeGlobal,
		Carrier:        desiredextension.CarrierOpenCodePlugin,
		Source:         "npm:tool@token = actual-secret",
		AuthorityPaths: openCodeCandidateAuthorities(t, root, target.ScopeGlobal),
	})
	if err == nil {
		t.Fatal("NewRemovalPlan admitted a source without credential authority")
	}
}

func openCodeCandidateAuthorities(
	t *testing.T,
	root string,
	scope target.Scope,
) []observerelation.AuthorityPath {
	t.Helper()
	paths := make([]observerelation.AuthorityPath, 0, 4)
	for _, name := range []string{
		"opencode.json",
		"opencode.jsonc",
		"tui.json",
		"tui.jsonc",
	} {
		paths = append(
			paths,
			authorityPath(t, filepath.Join(root, name), target.TargetOpenCode, scope),
		)
	}
	return paths
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
