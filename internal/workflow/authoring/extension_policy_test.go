package authoring

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	daempaths "github.com/isty2e/daem/internal/paths"
)

func TestExtensionAddBehaviorBuildsEveryAdmittedCarrierRow(t *testing.T) {
	tests := []struct {
		name            string
		request         AddExtensionRequest
		wantCarrier     string
		wantTarget      string
		wantScope       string
		wantMarketplace string
		wantHostSource  string
	}{
		{
			name:            "claude inferred project marketplace",
			request:         AddExtensionRequest{ID: "claude", Source: "plugin@market"},
			wantCarrier:     "claude-code-plugin",
			wantTarget:      "claude-code",
			wantScope:       "project",
			wantMarketplace: "plugin@market",
		},
		{
			name:            "codex explicit global marketplace",
			request:         AddExtensionRequest{ID: "codex", Source: "plugin@market", Targets: []string{"codex"}, Scope: "global"},
			wantCarrier:     "codex-plugin",
			wantTarget:      "codex",
			wantScope:       "global",
			wantMarketplace: "plugin@market",
		},
		{
			name:           "opencode default project host source",
			request:        AddExtensionRequest{ID: "opencode", Source: "@acme/plugin", Targets: []string{"opencode"}},
			wantCarrier:    "opencode-plugin",
			wantTarget:     "opencode",
			wantScope:      "project",
			wantHostSource: "@acme/plugin",
		},
		{
			name:           "pi explicit global host source",
			request:        AddExtensionRequest{ID: "pi", Source: "github:acme/plugin", Targets: []string{"pi"}, Scope: "global"},
			wantCarrier:    "pi-package",
			wantTarget:     "pi",
			wantScope:      "global",
			wantHostSource: "github:acme/plugin",
		},
		{
			name:           "antigravity explicit global host source",
			request:        AddExtensionRequest{ID: "antigravity", Source: "plugin@publisher", Targets: []string{"antigravity-cli"}, Scope: "global"},
			wantCarrier:    "antigravity-cli-plugin",
			wantTarget:     "antigravity-cli",
			wantScope:      "global",
			wantHostSource: "plugin@publisher",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			headerTargets := test.request.Targets
			if len(headerTargets) == 0 {
				headerTargets = []string{"claude-code"}
			}
			extension, err := ExtensionFromAddRequest(test.request, declaration.ManifestHeader{Targets: headerTargets}, daempaths.ManifestOriginExplicit)
			if err != nil {
				t.Fatalf("ExtensionFromAddRequest returned error: %v", err)
			}
			if extension.Carrier != test.wantCarrier ||
				len(extension.Targets) != 1 || extension.Targets[0] != test.wantTarget ||
				extension.Scope != test.wantScope ||
				extension.Source.Marketplace != test.wantMarketplace ||
				extension.Source.HostSource != test.wantHostSource {
				t.Fatalf("extension = %#v, want carrier=%q target=%q scope=%q marketplace=%q host_source=%q", extension, test.wantCarrier, test.wantTarget, test.wantScope, test.wantMarketplace, test.wantHostSource)
			}
		})
	}
}

func TestExtensionAddBehaviorRequiresCanonicalMarketplaceSelector(t *testing.T) {
	for _, marketplace := range []string{"", "context7", "context7@", "@market", "context7@market@extra"} {
		_, err := ExtensionFromAddRequest(AddExtensionRequest{ID: "context7", Source: marketplace, Targets: []string{"claude-code"}}, declaration.ManifestHeader{Targets: []string{"claude-code"}}, daempaths.ManifestOriginExplicit)
		if err == nil || !strings.Contains(err.Error(), "PLUGIN@MARKETPLACE") {
			t.Fatalf("marketplace %q error = %v, want canonical selector diagnostic", marketplace, err)
		}
	}
}

func TestExtensionAuthoringDerivesEveryCarrierFromCanonicalContract(t *testing.T) {
	seenTargets := make(map[string]desiredextension.Carrier)
	for _, carrier := range desiredextension.SupportedCarriers() {
		admittedTarget, targetOK := carrier.AdmittedTarget()
		sourceKind, sourceOK := carrier.RequiredSourceKind()
		if !targetOK || !sourceOK {
			t.Fatalf("carrier %q has incomplete canonical contract", carrier)
		}
		if previous, exists := seenTargets[string(admittedTarget)]; exists {
			t.Fatalf("target %q maps to carriers %q and %q; extension authoring requires an explicit carrier selector before admitting this catalog", admittedTarget, previous, carrier)
		}
		seenTargets[string(admittedTarget)] = carrier

		selected, ok := extensionAuthoringCarrierForTarget(string(admittedTarget))
		if !ok || selected != carrier {
			t.Fatalf("extensionAuthoringCarrierForTarget(%q) = (%q, %t), want (%q, true)", admittedTarget, selected, ok, carrier)
		}
		if label, ok := carrier.Label(); !ok || label == "" {
			t.Fatalf("carrier %q label = (%q, %t), want non-empty", carrier, label, ok)
		}
		source := "host-source"
		if sourceKind == desiredextension.SourceKindMarketplace {
			source = "plugin@market"
		}
		if _, err := extensionAuthoringSource(carrier, source); err != nil {
			t.Fatalf("extensionAuthoringSource(%q, %q) returned error: %v", carrier, source, err)
		}
	}
}

func TestExtensionMarketplaceErrorPreservesCuratedTargetWording(t *testing.T) {
	_, err := ExtensionFromAddRequest(
		AddExtensionRequest{ID: "context7", Targets: []string{"claude-code"}},
		declaration.ManifestHeader{},
		daempaths.ManifestOriginExplicit,
	)
	const want = "extension source must be PLUGIN@MARKETPLACE for --target claude-code or codex"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestExtensionAddBehaviorRejectsUnsupportedTargetAndScope(t *testing.T) {
	tests := []struct {
		name    string
		request AddExtensionRequest
		header  declaration.ManifestHeader
		want    string
	}{
		{
			name:    "codex omitted global scope",
			request: AddExtensionRequest{ID: "context7", Source: "context7@market", Targets: []string{"codex"}},
			want:    "--scope global is required for --target codex",
		},
		{
			name:    "antigravity omitted global scope",
			request: AddExtensionRequest{ID: "guidance", Source: "guidance@publisher", Targets: []string{"antigravity-cli"}},
			want:    "--scope global is required for --target antigravity-cli",
		},
		{
			name:    "ambiguous inherited targets",
			request: AddExtensionRequest{ID: "formatter", Source: "formatter@market"},
			header:  declaration.ManifestHeader{Targets: []string{"claude-code", "opencode"}},
			want:    "ambiguous across manifest targets",
		},
		{
			name:    "user scope",
			request: AddExtensionRequest{ID: "context7", Source: "context7@market", Targets: []string{"claude-code"}, Scope: "user"},
			want:    "supports only --scope project or global",
		},
		{
			name:    "local scope",
			request: AddExtensionRequest{ID: "context7", Source: "context7@market", Targets: []string{"claude-code"}, Scope: "local"},
			want:    "supports only --scope project or global",
		},
		{
			name:    "managed scope",
			request: AddExtensionRequest{ID: "context7", Source: "context7@market", Targets: []string{"claude-code"}, Scope: "managed"},
			want:    "supports only --scope project or global",
		},
		{
			name:    "multiple targets",
			request: AddExtensionRequest{ID: "context7", Source: "context7@market", Targets: []string{"claude-code", "codex"}},
			want:    "accepts at most one distinct --target",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			header := test.header
			if len(header.Targets) == 0 {
				header.Targets = []string{"claude-code"}
			}
			_, err := ExtensionFromAddRequest(test.request, header, daempaths.ManifestOriginExplicit)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err = %v, want %q", err, test.want)
			}
		})
	}
}

func TestExtensionRemoveSelectorKeepsOmittedFiltersAbsent(t *testing.T) {
	selector, err := (RemoveExtensionRequest{ID: "context7"}).normalizedSelector()
	if err != nil {
		t.Fatalf("normalizedSelector returned error: %v", err)
	}
	if selector.ID != "context7" || selector.Targets != nil || selector.Scope != "" {
		t.Fatalf("selector = %#v, want id-only selector with absent filters", selector)
	}
}

func TestExtensionRemoveSelectorValidatesOnlyExplicitTargetScopePairs(t *testing.T) {
	tests := []struct {
		name    string
		request RemoveExtensionRequest
		want    string
	}{
		{
			name:    "codex without scope is a valid filter",
			request: RemoveExtensionRequest{ID: "codex", Targets: []string{"codex"}},
		},
		{
			name:    "global without target is a valid filter",
			request: RemoveExtensionRequest{ID: "global", Scope: "global"},
		},
		{
			name:    "opencode global pair",
			request: RemoveExtensionRequest{ID: "opencode", Targets: []string{"opencode"}, Scope: "global"},
		},
		{
			name:    "codex project pair",
			request: RemoveExtensionRequest{ID: "codex", Targets: []string{"codex"}, Scope: "project"},
			want:    "--target codex supports only --scope global",
		},
		{
			name:    "antigravity project pair",
			request: RemoveExtensionRequest{ID: "antigravity", Targets: []string{"antigravity-cli"}, Scope: "project"},
			want:    "--target antigravity-cli supports only --scope global",
		},
		{
			name:    "multiple target filters",
			request: RemoveExtensionRequest{ID: "multi", Targets: []string{"opencode", "pi"}},
			want:    "accepts at most one --target filter",
		},
		{
			name:    "unsupported target",
			request: RemoveExtensionRequest{ID: "other", Targets: []string{"other"}},
			want:    "extension removal does not support --target other",
		},
		{
			name:    "host scope vocabulary",
			request: RemoveExtensionRequest{ID: "context7", Scope: "user"},
			want:    "extension removal supports only --scope project or global",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selector, err := test.request.normalizedSelector()
			if test.want == "" {
				if err != nil {
					t.Fatalf("normalizedSelector returned error: %v", err)
				}
				if len(selector.Targets) != len(test.request.Targets) || selector.Scope != strings.TrimSpace(test.request.Scope) {
					t.Fatalf("selector = %#v, want normalized explicit filters %#v", selector, test.request)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err = %v, want %q", err, test.want)
			}
		})
	}
}
