package configuration_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

type documentExpectation struct {
	alias, root, locator, relative, policy string
	grammar                                configuration.Grammar
	class                                  configuration.DocumentClass
	observations                           []configuration.Observation
}

func expectedObservations(extra ...configuration.Observation) []configuration.Observation {
	return append([]configuration.Observation{configuration.ReferentAliases, configuration.InstalledClaims}, extra...)
}

func assertDocumentContract(t *testing.T, spec configuration.DocumentSpec, want documentExpectation) {
	t.Helper()
	coordinate := configuration.PointSelf()
	if want.relative != "" {
		var err error
		coordinate, err = configuration.NewDirectoryChild(want.relative)
		if err != nil {
			t.Fatal(err)
		}
	}
	if spec.Destination().String() != want.alias || spec.RootReference().Identity() != want.root ||
		spec.RootReference().LocatorPolicy() != want.locator || spec.Coordinate() != coordinate ||
		spec.PolicyID() != want.policy || spec.Grammar() != want.grammar || spec.Class() != want.class ||
		spec.ModePolicy() != configuration.PrivateCreation || !slices.Equal(spec.RequiredObservations(), want.observations) {
		t.Fatalf("document contract differs: alias=%s root=%s locator=%s coordinate=%v policy=%s grammar=%s class=%s mode=%s observations=%v",
			spec.Destination().String(), spec.RootReference().Identity(), spec.RootReference().LocatorPolicy(), spec.Coordinate(),
			spec.PolicyID(), spec.Grammar(), spec.Class(), spec.ModePolicy(), spec.RequiredObservations())
	}
	binding, err := configuration.RootBindingForAlias(configuration.AliasInput{Target: spec.Target(), Scope: spec.Scope(), Destination: spec.Destination()})
	reference := binding.RootReference()
	if err != nil || binding.Validate() != nil || reference.Identity() != want.root || reference.LocatorPolicy() != want.locator || binding.Coordinate() != coordinate || binding.PolicyID() != want.policy {
		t.Fatalf("alias binding = %v/%v/%s, %v; want %s/%s/%s/%s", reference, binding.Coordinate(), binding.PolicyID(), err, want.root, want.locator, want.relative, want.policy)
	}
	retained, err := configuration.AdmitRecordedAliasBinding(configuration.AliasInput{Target: spec.Target(), Scope: spec.Scope(), Destination: spec.Destination()}, reference, binding.Coordinate(), binding.PolicyID())
	if err != nil || retained != binding {
		t.Fatal("recorded binding does not retain the selected rule family", retained, err)
	}
	if _, native := spec.NativePlacementPath(); native {
		reference, err := configuration.RootReferenceForScope(spec.Target(), spec.Scope())
		if err != nil || reference.Identity() != want.root || reference.LocatorPolicy() != want.locator {
			t.Fatalf("selected layout root = %v, %v; want %s/%s", reference, err, want.root, want.locator)
		}
		if spec.Scope() == target.ScopeProject {
			project, err := configuration.ProjectRootReference()
			if err != nil || project != reference {
				t.Fatalf("project binding differs from its layout: %v, %v", project, err)
			}
		}
	}
}

func TestRootBindingRefusesUnrelatedPortableRootsAndHomePaths(t *testing.T) {
	for _, test := range []struct {
		selected target.Target
		alias    string
	}{
		{target.TargetPi, "@codex-home/a.json"},
		{target.TargetClaudeCode, "~/outside.json"},
	} {
		destination, err := output.Parse(test.alias)
		if err != nil {
			t.Fatal(err)
		}
		binding, err := configuration.RootBindingForAlias(configuration.AliasInput{Target: test.selected, Scope: target.ScopeGlobal, Destination: destination})
		if !errors.Is(err, configuration.ErrDocumentPath) || binding != (configuration.AliasBinding{}) {
			t.Fatalf("unrelated alias %s returned a binding: %v, %v", test.alias, binding, err)
		}
	}
}

func TestRootBindingDoesNotImposePlacementOrGrammarAdmission(t *testing.T) {
	for _, test := range []struct {
		selected target.Target
		scope    target.Scope
		alias    string
		root     string
		relative string
	}{
		{target.TargetPi, target.ScopeProject, "retained/document.bin", "project", "retained/document.bin"},
		{target.TargetCodex, target.ScopeGlobal, "@codex-home/retained.bin", "codex-home", "retained.bin"},
	} {
		destination, err := output.Parse(test.alias)
		if err != nil {
			t.Fatal(err)
		}
		input := configuration.AliasInput{Target: test.selected, Scope: test.scope, Destination: destination, Syntax: hostconfig.Syntax("not-a-grammar")}
		binding, err := configuration.RootBindingForAlias(input)
		child, directory := binding.Coordinate().DirectoryChild()
		if err != nil || binding.RootReference().Identity() != test.root || !directory || child != test.relative || binding.PolicyID() == "" {
			t.Fatalf("retained binding %s = %v, %v", test.alias, binding, err)
		}
		if _, err := configuration.DocumentSpecForAlias(input); err == nil {
			t.Fatal("binding lookup was mistaken for placement admission")
		}
	}
}

func TestRecordedBindingRefusesReferencePathAndPolicyReinterpretation(t *testing.T) {
	destination, err := output.Parse("~/.pi/agent/mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	input := configuration.AliasInput{Target: target.TargetPi, Scope: target.ScopeGlobal, Destination: destination, Syntax: "not-a-grammar"}
	binding, err := configuration.RootBindingForAlias(input)
	if err != nil {
		t.Fatal(err)
	}
	wrongRoot, err := output.NewRootReference(binding.RootReference().Identity(), "pi-agent-location-v2")
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := configuration.NewDirectoryChild("sibling.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		reference  output.RootReference
		coordinate configuration.RootCoordinate
		policy     string
	}{
		{wrongRoot, binding.Coordinate(), binding.PolicyID()},
		{binding.RootReference(), sibling, binding.PolicyID()},
		{binding.RootReference(), configuration.PointSelf(), binding.PolicyID()},
		{binding.RootReference(), binding.Coordinate(), "pi-mcp-carrier-v2"},
		{binding.RootReference(), binding.Coordinate(), "pi-native-v1"},
	} {
		retained, err := configuration.AdmitRecordedAliasBinding(input, test.reference, test.coordinate, test.policy)
		if !errors.Is(err, configuration.ErrDocumentPath) || retained != (configuration.AliasBinding{}) {
			t.Fatal("recorded identity was reinterpreted through another rule family", retained, err)
		}
	}
	if retained, err := configuration.AdmitRecordedAliasBinding(input, binding.RootReference(), binding.Coordinate(), binding.PolicyID()); err != nil || retained != binding {
		t.Fatal("binding admission imposed a producer grammar", retained, err)
	}
}

func TestNativeDocumentSelectionAndInverseCoverRegisteredHosts(t *testing.T) {
	type scopeKey struct {
		target target.Target
		scope  target.Scope
	}
	cases := map[scopeKey]struct {
		path string
		want documentExpectation
	}{
		{target.TargetPi, target.ScopeGlobal}: {"settings.json", documentExpectation{
			"@pi-agent/settings.json", "pi-agent", "pi-agent-location-v1", "settings.json", "pi-native-v1", configuration.JSON, configuration.SharedConfiguration, expectedObservations(configuration.PiLoader),
		}},
		{target.TargetPi, target.ScopeProject}: {"settings.json", documentExpectation{
			".pi/settings.json", "project", "selected-project-v1", ".pi/settings.json", "pi-native-v1", configuration.JSON, configuration.SharedConfiguration, expectedObservations(configuration.PiLoader),
		}},
		{target.TargetCodex, target.ScopeGlobal}: {"review.config.toml", documentExpectation{
			"@codex-home/review.config.toml", "codex-home", "codex-home-location-v1", "review.config.toml", "codex-native-v1", configuration.TOML, configuration.SharedConfiguration, expectedObservations(configuration.CodexRuntimeLocations),
		}},
		{target.TargetCodex, target.ScopeProject}: {"config.toml", documentExpectation{
			".codex/config.toml", "project", "selected-project-v1", ".codex/config.toml", "codex-native-v1", configuration.TOML, configuration.SharedConfiguration, expectedObservations(configuration.CodexRuntimeLocations),
		}},
		{target.TargetClaudeCode, target.ScopeGlobal}: {"settings.json", documentExpectation{
			"~/.claude/settings.json", "claude-preferences", "claude-preferences-location-v1", "settings.json", "claude-native-v1", configuration.JSON, configuration.SharedConfiguration, expectedObservations(),
		}},
		{target.TargetClaudeCode, target.ScopeProject}: {"settings.local.json", documentExpectation{
			".claude/settings.local.json", "project", "selected-project-v1", ".claude/settings.local.json", "claude-native-v1", configuration.JSON, configuration.SharedConfiguration, expectedObservations(configuration.ClaudeProjectLocation),
		}},
		{target.TargetOpenCode, target.ScopeGlobal}: {"opencode.json", documentExpectation{
			"~/.config/opencode/opencode.json", "opencode-configuration", "opencode-location-v1", "opencode.json", "opencode-native-v1", configuration.JSONC, configuration.SharedConfiguration, expectedObservations(configuration.OpenCodeCandidates),
		}},
		{target.TargetOpenCode, target.ScopeProject}: {".opencode/tui.jsonc", documentExpectation{
			".opencode/tui.jsonc", "project", "selected-project-v1", ".opencode/tui.jsonc", "opencode-native-v1", configuration.JSONC, configuration.SharedConfiguration, expectedObservations(configuration.OpenCodeCandidates),
		}},
		{target.TargetAntigravityCLI, target.ScopeGlobal}: {"settings.json", documentExpectation{
			"~/.gemini/antigravity-cli/settings.json", "antigravity-preferences", "antigravity-preferences-location-v1", "settings.json", "antigravity-native-v1", configuration.JSON, configuration.Preferences, expectedObservations(),
		}},
	}
	for _, selected := range target.SupportedTargets() {
		for _, scope := range target.SupportedScopes() {
			t.Run(string(selected)+"/"+string(scope), func(t *testing.T) {
				if selected == target.TargetAntigravityCLI && scope == target.ScopeProject {
					_, err := configuration.NewDocumentSpec(configuration.DocumentInput{Target: selected, Scope: scope, Path: "settings.json"})
					if !errors.Is(err, configuration.ErrNoNativeSurface) {
						t.Fatalf("project preferences layer was invented: %v", err)
					}
					return
				}
				test, present := cases[scopeKey{selected, scope}]
				if !present {
					t.Fatal("registered host/scope has no composition evidence")
				}
				spec, err := configuration.NewDocumentSpec(configuration.DocumentInput{Target: selected, Scope: scope, Path: test.path})
				if err != nil || spec.Validate() != nil {
					t.Fatalf("native document selection: %v", err)
				}
				assertDocumentContract(t, spec, test.want)
				inverse, err := configuration.DocumentSpecForAlias(configuration.AliasInput{Target: selected, Scope: scope, Destination: spec.Destination()})
				if err != nil || inverse != spec {
					t.Fatalf("forward/inverse document contracts disagree: %v", err)
				}
				if nativePath, native := inverse.NativePlacementPath(); !native || nativePath != test.path {
					t.Fatal("native placement was lost on inverse selection")
				}
				observations := spec.RequiredObservations()
				if !slices.Contains(observations, configuration.ReferentAliases) || !slices.Contains(observations, configuration.InstalledClaims) {
					t.Fatal("static selection bypassed required physical observations")
				}
				observations[0] = "forged"
				if !slices.Contains(spec.RequiredObservations(), configuration.ReferentAliases) {
					t.Fatal("caller mutated the document's observation obligations")
				}
			})
		}
	}
}

func TestKnownGrammarPrecedesHintsAndGenericFallback(t *testing.T) {
	for _, test := range []struct {
		selected target.Target
		scope    target.Scope
		path     string
		syntax   hostconfig.Syntax
		grammar  configuration.Grammar
		class    configuration.DocumentClass
	}{
		{target.TargetPi, target.ScopeGlobal, "models.json", "", configuration.JSONLineComments, configuration.ModelCatalog},
		{target.TargetPi, target.ScopeProject, "models.json", "", configuration.JSON, configuration.GenericConfiguration},
		{target.TargetPi, target.ScopeGlobal, "agents/reviewer.md", "", configuration.Opaque, configuration.GenericConfiguration},
		{target.TargetCodex, target.ScopeGlobal, "custom.data", hostconfig.SyntaxJSONC, configuration.JSONC, configuration.GenericConfiguration},
		{target.TargetAntigravityCLI, target.ScopeGlobal, "keybindings.json", "", configuration.JSON, configuration.Preferences},
		{target.TargetOpenCode, target.ScopeProject, "tui.json", "", configuration.JSONC, configuration.SharedConfiguration},
		{target.TargetOpenCode, target.ScopeProject, ".opencode/agents/reviewer.md", "", configuration.Opaque, configuration.GenericConfiguration},
	} {
		spec, err := configuration.NewDocumentSpec(configuration.DocumentInput{Target: test.selected, Scope: test.scope, Path: test.path, Syntax: test.syntax})
		if err != nil || spec.Grammar() != test.grammar || spec.Class() != test.class {
			t.Errorf("%s/%s %s: grammar=%s, error=%v", test.selected, test.scope, test.path, spec.Grammar(), err)
		}
	}
	for _, input := range []configuration.DocumentInput{
		{Target: target.TargetPi, Scope: target.ScopeGlobal, Path: "models.json", Syntax: hostconfig.SyntaxJSONC},
		{Target: target.TargetCodex, Scope: target.ScopeGlobal, Path: "review.config.toml", Syntax: hostconfig.SyntaxJSON},
		{Target: target.TargetOpenCode, Scope: target.ScopeProject, Path: "opencode.json", Syntax: hostconfig.SyntaxJSON},
	} {
		if _, err := configuration.NewDocumentSpec(input); !errors.Is(err, configuration.ErrGrammar) {
			t.Errorf("known grammar was weakened: %v", err)
		}
	}
}

func TestDocumentIdentityDoesNotRetainDeclarationSyntaxHints(t *testing.T) {
	for _, test := range []struct {
		selected target.Target
		path     string
		syntax   hostconfig.Syntax
	}{
		{target.TargetPi, "settings.json", hostconfig.SyntaxJSON},
		{target.TargetPi, "extension-config.json", hostconfig.SyntaxJSON},
		{target.TargetCodex, "config.toml", hostconfig.SyntaxTOML},
		{target.TargetOpenCode, "opencode.json", hostconfig.SyntaxJSONC},
	} {
		input := configuration.DocumentInput{Target: test.selected, Scope: target.ScopeGlobal, Path: test.path}
		implicit, err := configuration.NewDocumentSpec(input)
		if err != nil {
			t.Fatal(err)
		}
		input.Syntax = test.syntax
		explicit, err := configuration.NewDocumentSpec(input)
		if err != nil || explicit != implicit || explicit.Validate() != nil {
			t.Fatalf("equivalent document grammar depended on declaration hint: %v", err)
		}
	}
}

func TestKnownDocumentDescendantRefusalIdentifiesTheConflictingPaths(t *testing.T) {
	for _, test := range []struct {
		selected target.Target
		document string
	}{
		{target.TargetPi, "settings.json"},
		{target.TargetCodex, "review.config.toml"},
		{target.TargetOpenCode, "opencode.json"},
	} {
		child := test.document + "/child.json"
		_, err := configuration.NewDocumentSpec(configuration.DocumentInput{
			Target: test.selected, Scope: target.ScopeGlobal, Path: child,
		})
		if !errors.Is(err, configuration.ErrDocumentPath) || !strings.Contains(err.Error(), child) || !strings.Contains(err.Error(), test.document) {
			t.Fatalf("descendant diagnostic lost its input paths: %v", err)
		}
	}
}

func TestNativeLayoutRejectsUnboundAndNoncanonicalDocuments(t *testing.T) {
	for _, input := range []configuration.DocumentInput{
		{Target: "unknown", Scope: target.ScopeGlobal, Path: "settings.json"},
		{Target: target.TargetPi, Scope: "unknown", Path: "settings.json"},
		{Target: target.TargetPi, Scope: target.ScopeGlobal, Path: "../settings.json"},
		{Target: target.TargetPi, Scope: target.ScopeGlobal, Path: "settings.json/child.json"},
		{Target: target.TargetCodex, Scope: target.ScopeGlobal, Path: "review.config.toml/child.json"},
		{Target: target.TargetOpenCode, Scope: target.ScopeProject, Path: ".opencode/opencode.json/child.json"},
	} {
		if _, err := configuration.NewDocumentSpec(input); err == nil {
			t.Errorf("invalid document was admitted: %v", input)
		}
	}
	for _, nativePath := range []string{"daem.toml", ".daem/state.json", ".mcp.json", ".claude/settings.json", "AGENTS.md", "package.json", ".env", ".opencode"} {
		if _, err := configuration.NewDocumentSpec(configuration.DocumentInput{Target: target.TargetOpenCode, Scope: target.ScopeProject, Path: nativePath}); !errors.Is(err, configuration.ErrDocumentPath) {
			t.Errorf("OpenCode project namespace included %s: %v", nativePath, err)
		}
	}
	destination, err := output.Parse("~/unbound/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := configuration.DocumentSpecForAlias(configuration.AliasInput{Target: target.TargetClaudeCode, Scope: target.ScopeGlobal, Destination: destination}); !errors.Is(err, configuration.ErrDocumentPath) {
		t.Fatalf("unbound inverse alias admitted: %v", err)
	}
	if (configuration.DocumentSpec{}).Validate() == nil {
		t.Fatal("zero document contract admitted")
	}
	if (configuration.AliasBinding{}).Validate() == nil {
		t.Fatal("zero alias binding admitted")
	}
}

func TestClaudeLocalLocationObligationDoesNotInventAGlobalLocalLayer(t *testing.T) {
	project, err := configuration.NewDocumentSpec(configuration.DocumentInput{
		Target: target.TargetClaudeCode, Scope: target.ScopeProject, Path: "settings.local.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	global, err := configuration.NewDocumentSpec(configuration.DocumentInput{
		Target: target.TargetClaudeCode, Scope: target.ScopeGlobal, Path: "settings.local.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if project.Class() != configuration.SharedConfiguration || !slices.Contains(project.RequiredObservations(), configuration.ClaudeProjectLocation) {
		t.Fatal("local project settings lost their actual-location observation")
	}
	if global.Class() != configuration.GenericConfiguration || slices.Contains(global.RequiredObservations(), configuration.ClaudeProjectLocation) {
		t.Fatal("an unknown global file became the project-local settings contract")
	}
}

func TestDedicatedAliasesOutsideNativeLayoutsDoNotInventGenericOwnership(t *testing.T) {
	for _, test := range []struct {
		selected target.Target
		scope    target.Scope
		want     documentExpectation
	}{
		{target.TargetPi, target.ScopeGlobal, documentExpectation{
			"~/.pi/agent/mcp.json", "pi-agent", "pi-agent-location-v1", "mcp.json", "pi-mcp-carrier-v1", configuration.JSON, configuration.SharedConfiguration, expectedObservations(configuration.PiLoader),
		}},
		{target.TargetCodex, target.ScopeGlobal, documentExpectation{
			"~/.codex/config.toml", "codex-home", "codex-home-location-v1", "config.toml", "codex-mcp-carrier-v1", configuration.TOML, configuration.SharedConfiguration, expectedObservations(configuration.CodexRuntimeLocations),
		}},
		{target.TargetClaudeCode, target.ScopeGlobal, documentExpectation{
			"~/.claude.json", "claude-account", "claude-account-point-v2", "", "claude-account-carrier-v2", configuration.JSON, configuration.SharedConfiguration, expectedObservations(),
		}},
		{target.TargetClaudeCode, target.ScopeProject, documentExpectation{
			".mcp.json", "project", "selected-project-v1", ".mcp.json", "claude-mcp-carrier-v1", configuration.JSON, configuration.SharedConfiguration, expectedObservations(),
		}},
		{target.TargetAntigravityCLI, target.ScopeGlobal, documentExpectation{
			"~/.gemini/config/mcp_config.json", "antigravity-shared", "antigravity-shared-point-v2", "", "antigravity-mcp-carrier-v2", configuration.JSON, configuration.SharedConfiguration, expectedObservations(),
		}},
	} {
		destination, err := output.Parse(test.want.alias)
		if err != nil {
			t.Fatal(err)
		}
		spec, err := configuration.DocumentSpecForAlias(configuration.AliasInput{Target: test.selected, Scope: test.scope, Destination: destination})
		if err != nil || spec.Validate() != nil {
			t.Fatalf("dedicated document contract: %v", err)
		}
		assertDocumentContract(t, spec, test.want)
		if nativePath, native := spec.NativePlacementPath(); native || nativePath != "" {
			t.Fatal("dedicated outside-layout alias became generic native placement")
		}
	}
}

func TestOpenCodeCandidatesRetainTheirLiteralLayerAndRequiredObservation(t *testing.T) {
	for _, test := range []struct {
		scope target.Scope
		paths []string
	}{
		{target.ScopeGlobal, []string{"opencode.json", "opencode.jsonc", "tui.json", "tui.jsonc"}},
		{target.ScopeProject, []string{"opencode.json", "opencode.jsonc", "tui.json", "tui.jsonc", ".opencode/opencode.json", ".opencode/opencode.jsonc", ".opencode/tui.json", ".opencode/tui.jsonc"}},
	} {
		for _, nativePath := range test.paths {
			spec, err := configuration.NewDocumentSpec(configuration.DocumentInput{Target: target.TargetOpenCode, Scope: test.scope, Path: nativePath})
			if err != nil {
				t.Fatal(err)
			}
			alternate, present := spec.AlternateFile()
			if !present || alternate == spec.Destination() || spec.Class() != configuration.SharedConfiguration || !slices.Contains(spec.RequiredObservations(), configuration.OpenCodeCandidates) {
				t.Fatal("alternate-file or candidate observation contract was lost")
			}
			other, err := configuration.DocumentSpecForAlias(configuration.AliasInput{Target: target.TargetOpenCode, Scope: test.scope, Destination: alternate})
			if err != nil {
				t.Fatal(err)
			}
			back, present := other.AlternateFile()
			if !present || back != spec.Destination() {
				t.Fatal("candidate relation changed the literal configuration layer")
			}
		}
	}
}
