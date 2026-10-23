package profile

import (
	"testing"

	desiredhostconfig "github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/target"
)

func TestHostConfigPlacementRejectsMalformedInput(t *testing.T) {
	valid := hostConfigInput(target.TargetPi, target.ScopeGlobal, "custom.json")
	for _, mutate := range []func(*HostConfigPlacementInput){
		func(input *HostConfigPlacementInput) { input.Target = "" },
		func(input *HostConfigPlacementInput) { input.Target = target.TargetOpenCode },
		func(input *HostConfigPlacementInput) { input.Scope = "system" },
		func(input *HostConfigPlacementInput) { input.Path = "" },
		func(input *HostConfigPlacementInput) { input.Path = "../custom.json" },
		func(input *HostConfigPlacementInput) { input.Path = "/custom.json" },
		func(input *HostConfigPlacementInput) { input.Path = "config//custom.json" },
		func(input *HostConfigPlacementInput) { input.Path = `config\custom.json` },
		func(input *HostConfigPlacementInput) { input.Path = "C:custom.json" },
		func(input *HostConfigPlacementInput) { input.Path = "custom\n.json" },
		func(input *HostConfigPlacementInput) { input.Path = " custom.json" },
		func(input *HostConfigPlacementInput) { input.Ownership = "merge" },
		func(input *HostConfigPlacementInput) { input.Syntax = "yaml" },
	} {
		input := valid
		mutate(&input)
		if _, err := ResolveHostConfigPlacement(input); err == nil {
			t.Fatalf("malformed placement accepted: %#v", input)
		}
	}
	for _, nativePath := range []string{"config/my data.json", "config/値.json", "config/100%25.json"} {
		input := valid
		input.Path = nativePath
		placement := mustHostConfigPlacement(t, input)
		if placement.NativePath() != nativePath {
			t.Fatalf("relative path was rewritten: %q", placement.NativePath())
		}
	}
}

func TestHostConfigPlacementIdentityPartitionsHostAndScope(t *testing.T) {
	seen := make(map[string]bool)
	for _, selected := range []target.Target{target.TargetPi, target.TargetCodex} {
		for _, scope := range []target.Scope{target.ScopeGlobal, target.ScopeProject} {
			placement := mustHostConfigPlacement(t, hostConfigInput(selected, scope, "custom.json"))
			if placement.ID() == "" || seen[placement.ID()] {
				t.Fatalf("host/scope identity collided: %s/%s -> %q", selected, scope, placement.ID())
			}
			seen[placement.ID()] = true
		}
	}
}

func TestHostConfigPlacementRetainsExplicitGrammarIntent(t *testing.T) {
	input := hostConfigInput(target.TargetPi, target.ScopeGlobal, "custom.json")
	automatic := mustHostConfigPlacement(t, input)
	input.Syntax = desiredhostconfig.SyntaxJSON
	explicit := mustHostConfigPlacement(t, input)
	if automatic.Grammar() != explicit.Grammar() || automatic.RequestedSyntax() == explicit.RequestedSyntax() {
		t.Fatal("explicit intent was lost despite identical logical grammar")
	}
	forged := automatic
	forged.grammar = HostConfigJSONC
	if err := forged.Validate(); err == nil {
		t.Fatal("grammar not implied by the retained intent was accepted")
	}
}

func TestHostConfigCodexNamedProfileClassification(t *testing.T) {
	for _, nativePath := range []string{"review.config.toml", "review_2-1.config.toml"} {
		input := hostConfigInput(target.TargetCodex, target.ScopeGlobal, nativePath)
		placement := mustHostConfigPlacement(t, input)
		if err := placement.ValidateStaticFootprint(mustHostConfigKey(t, "plugins")); err == nil {
			t.Fatalf("native profile %q bypassed dedicated ownership", nativePath)
		}
	}
	for _, nativePath := range []string{".config.toml", "review.v2.config.toml", "review v2.config.toml", "設定.config.toml"} {
		input := hostConfigInput(target.TargetCodex, target.ScopeGlobal, nativePath)
		input.Ownership = desiredhostconfig.OwnershipDocument
		placement := mustHostConfigPlacement(t, input)
		if placement.Grammar() != HostConfigTOML {
			t.Fatalf("ordinary TOML %q misclassified: %q", nativePath, placement.Grammar())
		}
	}
}

func TestHostConfigPiLoaderControlSubtreesAreReserved(t *testing.T) {
	for _, nativePath := range []string{
		"extensions/package.json/child.json", "extensions/index.ts/child.json",
		"extensions/index.js/child.json", "extensions/.ignore/child.json",
		"extensions/demo/package.json/child.json", "extensions/demo/index.ts/child.json",
		"extensions/demo/index.js/child.json",
	} {
		input := hostConfigInput(target.TargetPi, target.ScopeGlobal, nativePath)
		input.Ownership = desiredhostconfig.OwnershipDocument
		if _, err := ResolveHostConfigPlacement(input); err == nil {
			t.Fatalf("loader control subtree %q was accepted", nativePath)
		}
	}
	input := hostConfigInput(target.TargetPi, target.ScopeGlobal, "extensions/demo.ts/config.json")
	mustHostConfigPlacement(t, input)
}
