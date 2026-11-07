package profile

import (
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	desiredhostconfig "github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestComposedDedicatedKeysSelectOnlyTheDocumentsRoles(t *testing.T) {
	for _, test := range []struct {
		selected target.Target
		scope    target.Scope
		path     string
		reserved []string
		free     []string
	}{
		{target.TargetPi, target.ScopeProject, "settings.json", []string{"packages", "extensions", "skills"}, []string{"prompts", "themes", "npmCommand"}},
		{target.TargetCodex, target.ScopeProject, "config.toml", []string{"mcp_servers", "hooks", "plugins", "marketplaces"}, []string{"model", "features"}},
		{target.TargetClaudeCode, target.ScopeGlobal, "settings.json", []string{"hooks", "enabledPlugins"}, []string{"mcpServers", "extraKnownMarketplaces", "model"}},
		{target.TargetClaudeCode, target.ScopeProject, "settings.local.json", []string{"hooks", "enabledPlugins"}, []string{"mcpServers", "extraKnownMarketplaces", "model"}},
		{target.TargetOpenCode, target.ScopeProject, ".opencode/tui.json", []string{"plugin"}, []string{"mcp", "hooks", "theme"}},
		{target.TargetOpenCode, target.ScopeProject, ".opencode/opencode.json", []string{"mcp", "plugin"}, []string{"hooks", "model"}},
		{target.TargetOpenCode, target.ScopeGlobal, "opencode.jsonc", []string{"mcp", "plugin"}, []string{"hooks", "model"}},
		{target.TargetAntigravityCLI, target.ScopeGlobal, "settings.json", nil, []string{"mcpServers", "plugin", "model"}},
	} {
		t.Run(string(test.selected)+"/"+string(test.scope)+"/"+test.path, func(t *testing.T) {
			t.Parallel()
			facts, err := nativeConfigurationReservationsFor(test.selected, test.scope)
			if err != nil {
				t.Fatal(err)
			}
			spec, err := configuration.NewDocumentSpec(configuration.DocumentInput{Target: test.selected, Scope: test.scope, Path: test.path})
			if err != nil {
				t.Fatal(err)
			}
			if err := facts.ValidateDocument(spec); err != nil {
				t.Fatalf("selected document coverage is incomplete: %v", err)
			}
			for _, field := range test.reserved {
				key, err := output.NewKeyPath(field, "child")
				if err != nil {
					t.Fatal(err)
				}
				if err := facts.RequireUnreservedKey(spec, key); !errors.Is(err, configuration.ErrReservedKey) {
					t.Fatalf("%s subtree lost its dedicated owner: %v", field, err)
				}
			}
			for _, field := range test.free {
				key, err := output.NewKeyPath(field, "child")
				if err != nil {
					t.Fatal(err)
				}
				if err := facts.RequireUnreservedKey(spec, key); err != nil {
					t.Fatalf("unrelated %s became reserved: %v", field, err)
				}
			}
		})
	}
}

func TestNativeDedicatedPathsUseCurrentPlacementsAndDiscoveries(t *testing.T) {
	for _, test := range []struct {
		selected target.Target
		scope    target.Scope
		reserved []string
		free     []string
	}{
		{target.TargetPi, target.ScopeGlobal, []string{"AGENTS.md", "mcp.json", "skills/demo"}, []string{"settings.json", "models.json"}},
		{target.TargetPi, target.ScopeProject, []string{"mcp.json", "skills/demo"}, []string{"AGENTS.md", "settings.json"}},
		{target.TargetCodex, target.ScopeGlobal, []string{"AGENTS.md", "AGENTS.override.md", "hooks.json", "skills/demo"}, []string{"config.toml", "review.config.toml"}},
		{target.TargetCodex, target.ScopeProject, []string{"hooks.json"}, []string{"config.toml", "AGENTS.md", "skills/demo"}},
		{target.TargetClaudeCode, target.ScopeGlobal, []string{"CLAUDE.md", "skills/demo"}, []string{"settings.json", ".claude.json"}},
		{target.TargetClaudeCode, target.ScopeProject, []string{"CLAUDE.md", "skills/demo"}, []string{"settings.json", "settings.local.json"}},
		{target.TargetOpenCode, target.ScopeGlobal, []string{"AGENTS.md", "skills/demo"}, []string{"opencode.json", "tui.json"}},
		{target.TargetOpenCode, target.ScopeProject, []string{".opencode/skills/demo"}, []string{"opencode.json", ".opencode/opencode.json", ".opencode/tui.json"}},
		{target.TargetAntigravityCLI, target.ScopeGlobal, nil, []string{"settings.json", "keybindings.json", "skills/demo"}},
	} {
		t.Run(string(test.selected)+"/"+string(test.scope), func(t *testing.T) {
			facts, err := nativeConfigurationReservationsFor(test.selected, test.scope)
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range test.reserved {
				if err := facts.RequireUnreservedPath(path); !errors.Is(err, configuration.ErrReservedPath) {
					t.Fatalf("%q lost its current owner: %v", path, err)
				}
			}
			for _, path := range test.free {
				if err := facts.RequireUnreservedPath(path); err != nil {
					t.Fatalf("%q acquired another layer's owner: %v", path, err)
				}
			}
		})
	}
	if _, err := nativeConfigurationReservationsFor(target.TargetAntigravityCLI, target.ScopeProject); !errors.Is(err, configuration.ErrNoNativeSurface) {
		t.Fatalf("absent native scope was synthesized: %v", err)
	}
}

func TestNativeConfigurationKeysRefuseUnknownContexts(t *testing.T) {
	if _, err := nativeConfigurationReservationsFor("unknown", target.ScopeGlobal); err == nil {
		t.Fatal("unknown target obtained native namespace facts")
	}
	if _, err := nativeConfigurationReservationsFor(target.TargetPi, "system"); err == nil {
		t.Fatal("unknown scope obtained native namespace facts")
	}
}

func TestNativeOperationRoutesRetainPersistedTokens(t *testing.T) {
	for _, test := range []struct {
		selected  target.Target
		scope     target.Scope
		path      string
		ownership desiredhostconfig.Ownership
		syntax    desiredhostconfig.Syntax
		prefix    string
		version   string
		identity  string
	}{
		{target.TargetPi, target.ScopeGlobal, "settings.json", desiredhostconfig.OwnershipKeys, "", "managed-host-config-key", "native-json-v1", "pi-host-config-global-v1"},
		{target.TargetPi, target.ScopeGlobal, "settings.json", desiredhostconfig.OwnershipKeys, desiredhostconfig.SyntaxJSON, "managed-host-config-key", "native-json-v1", "pi-host-config-global-v1"},
		{target.TargetPi, target.ScopeGlobal, "models.json", desiredhostconfig.OwnershipKeys, "", "managed-host-config-key", "native-json-line-comments-v1", "pi-host-config-global-v1"},
		{target.TargetPi, target.ScopeProject, "models.json", desiredhostconfig.OwnershipKeys, "", "managed-host-config-key", "native-json-v1", "pi-host-config-project-v1"},
		{target.TargetCodex, target.ScopeProject, "config.toml", desiredhostconfig.OwnershipKeys, "", "managed-host-config-key", "native-toml-v1", "codex-host-config-project-v1"},
		{target.TargetCodex, target.ScopeGlobal, "prompts/review.md", desiredhostconfig.OwnershipDocument, "", "managed-host-config-document", "native-document.v1", "codex-host-config-global-v1"},
	} {
		placement := mustHostConfigPlacement(t, configuration.PlacementInput{
			Target: test.selected, Scope: test.scope, Path: test.path, Ownership: test.ownership, Syntax: test.syntax,
		})
		for _, operation := range []Operation{OperationWrite, OperationRemove} {
			route, err := HostConfigOperationRoute(placement, operation)
			if err != nil {
				t.Fatal(err)
			}
			if route.ResourceKind() != entity.KindHostConfig || route.Operation() != operation ||
				route.CorrelationID() != test.identity || route.RouteID() != test.prefix+"."+string(operation) ||
				route.AdapterContractVersion() != test.version {
				t.Fatalf("%s/%s/%s changed persisted route tokens: %+v", test.selected, test.scope, test.path, route)
			}
		}
	}
}

func TestHostConfigProjectionUsesStaticAdmissionInBothDirections(t *testing.T) {
	id, err := entity.New(entity.KindHostConfig, "review")
	if err != nil {
		t.Fatal(err)
	}
	for _, selected := range []target.Target{target.TargetPi, target.TargetCodex} {
		for _, scope := range []target.Scope{target.ScopeGlobal, target.ScopeProject} {
			for _, syntax := range []desiredhostconfig.Syntax{desiredhostconfig.SyntaxJSON, desiredhostconfig.SyntaxJSONC, desiredhostconfig.SyntaxJSONLineComments, desiredhostconfig.SyntaxTOML} {
				placement := mustHostConfigPlacement(t, configuration.PlacementInput{
					Target: selected, Scope: scope, Path: "extensions/subagent/config.data", Syntax: syntax,
				})
				footprint := mustHostConfigKey(t, "providers", "a/b~c", "", "\x00\n")
				contract, err := HostConfigProjectionContract(placement, footprint)
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := HostConfigPlacementForContract(contract)
				if err != nil || decoded.Target() != selected || decoded.Scope() != scope ||
					decoded.NativePath() != placement.NativePath() || decoded.Grammar() != placement.Grammar() {
					t.Fatalf("placement round trip: %v", err)
				}
				key, _ := footprint.Key()
				subject, err := topologyprojection.HostConfigKeySubject(id, selected, scope, key)
				if err != nil {
					t.Fatal(err)
				}
				if err := aggregate.ValidateSubjectContract(subject, contract); err != nil {
					t.Fatal(err)
				}
				if contract.Address().PlacementID() != placement.ID() || contract.Cardinality() != aggregate.ContributionExclusive ||
					contract.Equivalence() != aggregate.EquivalenceCanonicalSemantic ||
					contract.SiblingPreservation() != aggregate.PreserveSiblingsNativeValuesComments {
					t.Fatal("native contract lost its static guarantees")
				}
				if _, err := contract.Address().LegacyContentPath(); !errors.Is(err, output.ErrNotLegacyFootprint) {
					t.Fatalf("native names escaped legacy boundary: %v", err)
				}
			}
		}
	}
}

func TestHostConfigProjectionRefusesReservationsAndContractForgery(t *testing.T) {
	placement := mustHostConfigPlacement(t, hostConfigInput(target.TargetCodex, target.ScopeGlobal, "config.toml"))
	reserved := mustHostConfigKey(t, "mcp_servers", "example")
	if _, err := HostConfigProjectionContract(placement, reserved); !errors.Is(err, configuration.ErrReserved) {
		t.Fatalf("authoring did not refuse reserved field: %v", err)
	}
	for _, tc := range []struct {
		path  string
		codec aggregate.CodecContractID
		key   output.Footprint
	}{
		{"@codex-home/config.toml", "native-toml-v1", reserved},
		{"@codex-home/config.toml", "native-json-v1", mustHostConfigKey(t, "model")},
		{"@codex-home/auth.json", "native-json-v1", mustHostConfigKey(t, "key")},
		{"@pi-agent/custom.json", "native-json-v1", mustHostConfigKey(t, "key")},
		{"~/arbitrary.json", "native-json-v1", mustHostConfigKey(t, "key")},
	} {
		root, err := output.Parse(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		forged, err := aggregate.NewHostConfigProjectionContract(target.TargetCodex, target.ScopeGlobal, root, tc.key, tc.codec)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := HostConfigPlacementForContract(forged); err == nil {
			t.Fatalf("decoded contract bypassed admission for %s", tc.path)
		}
	}
	if _, err := HostConfigProjectionContract(placement, output.WholePathFootprint()); err == nil {
		t.Fatal("whole path became native key projection")
	}
	document := mustHostConfigPlacement(t, configuration.PlacementInput{
		Target: target.TargetPi, Scope: target.ScopeGlobal, Path: "custom.json", Ownership: desiredhostconfig.OwnershipDocument,
	})
	if _, err := HostConfigProjectionContract(document, mustHostConfigKey(t, "key")); err == nil {
		t.Fatal("whole-document declaration became a key declaration")
	}
}

func TestHostConfigContractShapeDoesNotGrantAnUnboundDestination(t *testing.T) {
	footprint := mustHostConfigKey(t, "theme")
	for _, selected := range target.SupportedTargets() {
		for _, scope := range target.SupportedScopes() {
			t.Run(string(selected)+"/"+string(scope), func(t *testing.T) {
				path := "not-a-native-surface/settings.json"
				if scope == target.ScopeGlobal {
					path = "~/" + path
				}
				root, err := output.Parse(path)
				if err != nil {
					t.Fatal(err)
				}
				contract, err := aggregate.NewHostConfigProjectionContract(selected, scope, root, footprint, "native-json-v1")
				if err != nil {
					t.Fatalf("shape refused before profile admission: %v", err)
				}
				if _, err := HostConfigPlacementForContract(contract); err == nil {
					t.Fatal("shape construction granted an unbound destination")
				}
			})
		}
	}
}

func TestHostConfigKeyIdentityIsIndependentOfGrammarAndDocument(t *testing.T) {
	id, err := entity.New(entity.KindHostConfig, "review")
	if err != nil {
		t.Fatal(err)
	}
	key, _ := output.NewKeyPath("theme")
	first, err := topologyprojection.HostConfigKeySubject(id, target.TargetPi, target.ScopeGlobal, key)
	if err != nil {
		t.Fatal(err)
	}
	for _, nativePath := range []string{"a.json", "b.toml"} {
		placement := mustHostConfigPlacement(t, hostConfigInput(target.TargetPi, target.ScopeGlobal, nativePath))
		contract, err := HostConfigProjectionContract(placement, mustHostConfigKey(t, "theme"))
		if err != nil {
			t.Fatal(err)
		}
		if err := aggregate.ValidateSubjectContract(first, contract); err != nil {
			t.Fatal(err)
		}
	}
}
