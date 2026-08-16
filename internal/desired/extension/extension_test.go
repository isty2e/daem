package extension

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
)

func TestExtensionConstructorOwnsClosedCarrierMatrix(t *testing.T) {
	tests := []struct {
		name       string
		carrier    Carrier
		target     target.Target
		scope      target.Scope
		sourceKind SourceKind
		ref        string
	}{
		{name: "claude project", carrier: CarrierClaudeCodePlugin, target: target.TargetClaudeCode, scope: target.ScopeProject, sourceKind: SourceKindMarketplace, ref: "plugin@market"},
		{name: "claude global", carrier: CarrierClaudeCodePlugin, target: target.TargetClaudeCode, scope: target.ScopeGlobal, sourceKind: SourceKindMarketplace, ref: "plugin@market"},
		{name: "codex global", carrier: CarrierCodexPlugin, target: target.TargetCodex, scope: target.ScopeGlobal, sourceKind: SourceKindMarketplace, ref: "plugin@market"},
		{name: "opencode project", carrier: CarrierOpenCodePlugin, target: target.TargetOpenCode, scope: target.ScopeProject, sourceKind: SourceKindHostSource, ref: "npm:@acme/plugin"},
		{name: "opencode global", carrier: CarrierOpenCodePlugin, target: target.TargetOpenCode, scope: target.ScopeGlobal, sourceKind: SourceKindHostSource, ref: "npm:@acme/plugin"},
		{name: "pi project", carrier: CarrierPiPackage, target: target.TargetPi, scope: target.ScopeProject, sourceKind: SourceKindHostSource, ref: "npm:@acme/pi"},
		{name: "pi global", carrier: CarrierPiPackage, target: target.TargetPi, scope: target.ScopeGlobal, sourceKind: SourceKindHostSource, ref: "npm:@acme/pi"},
		{name: "antigravity global", carrier: CarrierAntigravityCLIPlugin, target: target.TargetAntigravityCLI, scope: target.ScopeGlobal, sourceKind: SourceKindHostSource, ref: "plugin-name"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sourceRef, err := NewSourceRef(test.sourceKind, test.ref)
			if err != nil {
				t.Fatalf("NewSourceRef returned error: %v", err)
			}
			value, err := New(Spec{Name: "extension-id", Carrier: test.carrier, Target: test.target, Scope: test.scope, Source: sourceRef})
			if err != nil {
				t.Fatalf("New returned error: %v", err)
			}
			if value.ID().Kind() != entity.KindExtension || value.Carrier() != test.carrier || value.Source().Ref() != test.ref || value.Validate() != nil {
				t.Fatalf("extension = %#v", value)
			}
		})
	}
}

func TestSupportedCarriersReturnsStableDefensiveVocabulary(t *testing.T) {
	want := []Carrier{
		CarrierClaudeCodePlugin,
		CarrierCodexPlugin,
		CarrierOpenCodePlugin,
		CarrierPiPackage,
		CarrierAntigravityCLIPlugin,
	}
	got := SupportedCarriers()
	if len(got) != len(want) {
		t.Fatalf("SupportedCarriers = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("SupportedCarriers = %#v, want %#v", got, want)
		}
	}
	got[0] = "mutated"
	if SupportedCarriers()[0] != CarrierClaudeCodePlugin {
		t.Fatal("SupportedCarriers did not return a defensive copy")
	}
	if _, err := ParseCarrier("future-carrier"); err == nil || err.Error() != `unsupported extension carrier "future-carrier"; supported carriers are "claude-code-plugin", "codex-plugin", "opencode-plugin", "pi-package", and "antigravity-cli-plugin"` {
		t.Fatalf("ParseCarrier unsupported error = %v", err)
	}
}

func TestCarrierAdmittedScopesReturnsCompleteStableVocabulary(t *testing.T) {
	cases := []struct {
		carrier Carrier
		want    []target.Scope
	}{
		{carrier: CarrierClaudeCodePlugin, want: []target.Scope{target.ScopeGlobal, target.ScopeProject}},
		{carrier: CarrierCodexPlugin, want: []target.Scope{target.ScopeGlobal}},
		{carrier: CarrierOpenCodePlugin, want: []target.Scope{target.ScopeGlobal, target.ScopeProject}},
		{carrier: CarrierPiPackage, want: []target.Scope{target.ScopeGlobal, target.ScopeProject}},
		{carrier: CarrierAntigravityCLIPlugin, want: []target.Scope{target.ScopeGlobal}},
	}
	for _, tc := range cases {
		t.Run(string(tc.carrier), func(t *testing.T) {
			got := tc.carrier.AdmittedScopes()
			if len(got) != len(tc.want) {
				t.Fatalf("AdmittedScopes = %#v, want %#v", got, tc.want)
			}
			for index := range tc.want {
				if got[index] != tc.want[index] {
					t.Fatalf("AdmittedScopes = %#v, want %#v", got, tc.want)
				}
			}
			got[0] = target.Scope("mutated")
			if tc.carrier.AdmittedScopes()[0] != tc.want[0] {
				t.Fatal("AdmittedScopes did not return a defensive value")
			}
		})
	}
	if got := Carrier("future-carrier").AdmittedScopes(); got != nil {
		t.Fatalf("forged carrier scopes = %#v, want nil", got)
	}
}

func TestCarrierTargetScopeAdmissionIsDesiredPolicy(t *testing.T) {
	cases := []struct {
		name    string
		carrier Carrier
		target  target.Target
		scope   target.Scope
		want    bool
	}{
		{name: "claude project", carrier: CarrierClaudeCodePlugin, target: target.TargetClaudeCode, scope: target.ScopeProject, want: true},
		{name: "codex global", carrier: CarrierCodexPlugin, target: target.TargetCodex, scope: target.ScopeGlobal, want: true},
		{name: "codex project", carrier: CarrierCodexPlugin, target: target.TargetCodex, scope: target.ScopeProject},
		{name: "carrier target mismatch", carrier: CarrierCodexPlugin, target: target.TargetClaudeCode, scope: target.ScopeGlobal},
		{name: "unknown carrier", carrier: Carrier("future-carrier"), target: target.TargetCodex, scope: target.ScopeGlobal},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.carrier.AdmitsTargetScope(tc.target, tc.scope); got != tc.want {
				t.Fatalf("AdmitsTargetScope(%q, %q) = %t, want %t", tc.target, tc.scope, got, tc.want)
			}
		})
	}
}

func TestCarrierContractAccessorsExposeCanonicalFacts(t *testing.T) {
	cases := []struct {
		carrier    Carrier
		wantTarget target.Target
		wantSource SourceKind
		wantLabel  string
	}{
		{carrier: CarrierClaudeCodePlugin, wantTarget: target.TargetClaudeCode, wantSource: SourceKindMarketplace, wantLabel: "Claude Code plugin extension"},
		{carrier: CarrierCodexPlugin, wantTarget: target.TargetCodex, wantSource: SourceKindMarketplace, wantLabel: "Codex plugin extension"},
		{carrier: CarrierOpenCodePlugin, wantTarget: target.TargetOpenCode, wantSource: SourceKindHostSource, wantLabel: "OpenCode plugin extension"},
		{carrier: CarrierPiPackage, wantTarget: target.TargetPi, wantSource: SourceKindHostSource, wantLabel: "Pi package extension"},
		{carrier: CarrierAntigravityCLIPlugin, wantTarget: target.TargetAntigravityCLI, wantSource: SourceKindHostSource, wantLabel: "Antigravity CLI plugin extension"},
	}
	for _, tc := range cases {
		t.Run(string(tc.carrier), func(t *testing.T) {
			gotTarget, targetOK := tc.carrier.AdmittedTarget()
			gotSource, sourceOK := tc.carrier.RequiredSourceKind()
			gotLabel, labelOK := tc.carrier.Label()
			if !targetOK || gotTarget != tc.wantTarget {
				t.Fatalf("AdmittedTarget = (%q, %t), want (%q, true)", gotTarget, targetOK, tc.wantTarget)
			}
			if !sourceOK || gotSource != tc.wantSource {
				t.Fatalf("RequiredSourceKind = (%q, %t), want (%q, true)", gotSource, sourceOK, tc.wantSource)
			}
			if !labelOK || gotLabel != tc.wantLabel {
				t.Fatalf("Label = (%q, %t), want (%q, true)", gotLabel, labelOK, tc.wantLabel)
			}
		})
	}

	forged := Carrier("future-carrier")
	if got, ok := forged.AdmittedTarget(); ok || got != "" {
		t.Fatalf("forged AdmittedTarget = (%q, %t), want zero, false", got, ok)
	}
	if got, ok := forged.RequiredSourceKind(); ok || got != "" {
		t.Fatalf("forged RequiredSourceKind = (%q, %t), want zero, false", got, ok)
	}
	if got, ok := forged.Label(); ok || got != "" {
		t.Fatalf("forged Label = (%q, %t), want zero, false", got, ok)
	}
}

func TestExtensionConstructorRejectsCrossAxisStates(t *testing.T) {
	marketplace, _ := NewSourceRef(SourceKindMarketplace, "plugin@market")
	hostSource, _ := NewSourceRef(SourceKindHostSource, "npm:@acme/plugin")
	base := Spec{Name: "extension-id", Carrier: CarrierClaudeCodePlugin, Target: target.TargetClaudeCode, Scope: target.ScopeProject, Source: marketplace}
	tests := []struct {
		name string
		edit func(*Spec)
		want string
	}{
		{name: "unstable id", edit: func(spec *Spec) { spec.Name = "bad id" }, want: "stable token"},
		{name: "unknown carrier", edit: func(spec *Spec) { spec.Carrier = "generic-plugin" }, want: "unsupported extension carrier"},
		{name: "wrong target", edit: func(spec *Spec) { spec.Target = target.TargetCodex }, want: "supports only target"},
		{name: "codex project", edit: func(spec *Spec) { spec.Carrier = CarrierCodexPlugin; spec.Target = target.TargetCodex }, want: "does not support scope"},
		{name: "wrong source kind", edit: func(spec *Spec) { spec.Source = hostSource }, want: "requires source kind"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := base
			test.edit(&spec)
			if _, err := New(spec); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("New error = %v, want containing %q", err, test.want)
			}
		})
	}
	if err := (Extension{}).Validate(); err == nil {
		t.Fatal("zero Extension validated")
	}
}

func TestExtensionSourceRejectsOptionControlAndMalformedMarketplace(t *testing.T) {
	tests := []struct {
		kind SourceKind
		ref  string
		want string
	}{
		{kind: SourceKindHostSource, ref: "-malicious", want: "must not begin"},
		{kind: SourceKindHostSource, ref: "bad\nsource", want: "control"},
		{kind: SourceKindHostSource, ref: "bad\u0085source", want: "control"},
		{kind: SourceKindHostSource, ref: "safe\u202etxt", want: "control"},
		{kind: SourceKindHostSource, ref: string([]byte{'b', 'a', 'd', 0xff}), want: "valid UTF-8"},
		{kind: SourceKindHostSource, ref: "https://user:secret@example.com/plugin.tgz", want: "inline credentials"},
		{kind: SourceKindHostSource, ref: "https://token@example.com/plugin.tgz", want: "inline credentials"},
		{kind: SourceKindHostSource, ref: "npm:@acme/plugin?access_token=secret", want: "query fields"},
		{kind: SourceKindHostSource, ref: "npm:@acme/plugin?private_token=secret", want: "query fields"},
		{kind: SourceKindHostSource, ref: "npm:@acme/plugin?download=1", want: "query fields"},
		{kind: SourceKindHostSource, ref: "npm:tool@token:actual-secret", want: "credential assignments"},
		{kind: SourceKindHostSource, ref: `plugins\client-secret=actual-secret`, want: "credential assignments"},
		{kind: SourceKindHostSource, ref: "github:acme/plugin#api-key=secret", want: "must not contain assignments"},
		{kind: SourceKindHostSource, ref: "github:acme/plugin#private_token%3Dsecret", want: "must not contain assignments"},
		{kind: SourceKindHostSource, ref: "https://example.com/%zz", want: "URL is malformed"},
		{kind: SourceKindHostSource, ref: "./plugins/%zz#private_token%3Dsecret", want: "URL is malformed"},
		{kind: SourceKindMarketplace, ref: "plugin", want: "PLUGIN@MARKETPLACE"},
		{kind: SourceKindMarketplace, ref: "plugin@market@extra", want: "PLUGIN@MARKETPLACE"},
		{kind: SourceKindMarketplace, ref: "plugin@--help", want: "neither component"},
		{kind: "registry", ref: "plugin", want: "unsupported extension source kind"},
	}
	for _, test := range tests {
		if _, err := NewSourceRef(test.kind, test.ref); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("NewSourceRef(%q, %q) error = %v, want containing %q", test.kind, test.ref, err, test.want)
		}
	}
}

func TestExtensionSourceAllowsCredentialFreeHostNativeReferences(t *testing.T) {
	for _, ref := range []string{
		"git+ssh://git@github.com/acme/tools.git#v1",
		"github:acme/tools#v1",
		"npm:@acme/plugin",
	} {
		if _, err := NewSourceRef(SourceKindHostSource, ref); err != nil {
			t.Errorf("NewSourceRef(%q) returned error: %v", ref, err)
		}
	}
}

func TestMarketplaceSelectorOwnsValidatedComponents(t *testing.T) {
	source, err := NewSourceRef(
		SourceKindMarketplace,
		`team/plugin:beta\"한글@market/path%2Fstable`,
	)
	if err != nil {
		t.Fatalf("NewSourceRef returned error: %v", err)
	}
	selector, ok := source.MarketplaceSelector()
	if !ok {
		t.Fatal("MarketplaceSelector did not return a selector")
	}
	if selector.Plugin() != `team/plugin:beta\"한글` ||
		selector.Marketplace() != "market/path%2Fstable" ||
		selector.String() != source.Ref() {
		t.Fatalf("selector = %#v / %q", selector, selector.String())
	}

	hostSource, err := NewSourceRef(SourceKindHostSource, "github:acme/tools")
	if err != nil {
		t.Fatalf("NewSourceRef host source returned error: %v", err)
	}
	if _, ok := hostSource.MarketplaceSelector(); ok {
		t.Fatal("MarketplaceSelector accepted a host source")
	}
}

func TestExtensionSourceNamespaceRoundTripsDelimiterRichReferences(t *testing.T) {
	tests := []struct {
		kind SourceKind
		ref  string
	}{
		{
			kind: SourceKindMarketplace,
			ref:  `team/plugin:beta\"한글@market/path%2Fstable`,
		},
		{
			kind: SourceKindHostSource,
			ref:  `github:acme/tools:beta#release\"candidate`,
		},
	}
	for _, test := range tests {
		source, err := NewSourceRef(test.kind, test.ref)
		if err != nil {
			t.Fatalf("NewSourceRef(%q, %q): %v", test.kind, test.ref, err)
		}
		parsed, err := ParseSourceRef(source.String())
		if err != nil {
			t.Fatalf("ParseSourceRef(%q): %v", source.String(), err)
		}
		if parsed != source {
			t.Fatalf("source round trip = %#v, want %#v", parsed, source)
		}
	}

	for _, value := range []string{
		"",
		"host-source",
		"registry:plugin",
		"host-source:-option",
		"host-source:bad\u202esource",
	} {
		if _, err := ParseSourceRef(value); err == nil {
			t.Fatalf("ParseSourceRef(%q) accepted malformed namespace", value)
		}
	}
}

func TestCarrierKeyIgnoresDeclarationIDAndOwnsValidation(t *testing.T) {
	sourceRef, _ := NewSourceRef(SourceKindMarketplace, "plugin@market")
	left, _ := New(Spec{Name: "left", Carrier: CarrierClaudeCodePlugin, Target: target.TargetClaudeCode, Scope: target.ScopeProject, Source: sourceRef})
	right, _ := New(Spec{Name: "right", Carrier: CarrierClaudeCodePlugin, Target: target.TargetClaudeCode, Scope: target.ScopeProject, Source: sourceRef})
	if left.CarrierKey() != right.CarrierKey() {
		t.Fatal("carrier key included declaration ID")
	}
	if err := left.CarrierKey().Validate(); err != nil {
		t.Fatalf("CarrierKey.Validate returned error: %v", err)
	}
	if _, err := NewCarrierKey(
		CarrierCodexPlugin,
		target.TargetCodex,
		target.ScopeProject,
		sourceRef,
	); err == nil || !strings.Contains(err.Error(), "does not support scope") {
		t.Fatalf("NewCarrierKey accepted invalid Codex project carrier: %v", err)
	}
}

func TestCarrierKeyEnforcesCodexMarketplaceSegmentGrammarOnlyForCodex(t *testing.T) {
	validSource, err := NewSourceRef(SourceKindMarketplace, "plugin_name-2@market_name-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewCarrierKey(
		CarrierCodexPlugin,
		target.TargetCodex,
		target.ScopeGlobal,
		validSource,
	); err != nil {
		t.Fatalf("valid Codex selector was rejected: %v", err)
	}

	for _, ref := range []string{
		"../plugin@market",
		"plugin@../market",
		"plugin/name@market",
		"plugin@market.name",
		"plugin@한글",
	} {
		source, err := NewSourceRef(SourceKindMarketplace, ref)
		if err != nil {
			t.Fatalf("generic marketplace source %q was rejected: %v", ref, err)
		}
		if _, err := NewCarrierKey(
			CarrierCodexPlugin,
			target.TargetCodex,
			target.ScopeGlobal,
			source,
		); err == nil || !strings.Contains(err.Error(), "Codex plugin carrier") {
			t.Fatalf("Codex carrier accepted selector %q: %v", ref, err)
		}
		if _, err := NewCarrierKey(
			CarrierClaudeCodePlugin,
			target.TargetClaudeCode,
			target.ScopeGlobal,
			source,
		); err != nil {
			t.Fatalf("Claude carrier inherited Codex grammar for %q: %v", ref, err)
		}
	}
}
