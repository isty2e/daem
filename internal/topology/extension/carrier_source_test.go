package extension_test

import (
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestInterpretCarrierSourceMatchesPiSourceClasses(t *testing.T) {
	tests := []struct {
		name         string
		source       string
		wantClass    extensiontopology.CarrierSourceClass
		wantIdentity string
		wantEvidence extensiontopology.RelationEvidenceClass
	}{
		{name: "npm version", source: "npm:@acme/tools@1.2.3", wantClass: extensiontopology.CarrierSourceNPM, wantIdentity: "@acme/tools", wantEvidence: extensiontopology.RelationEvidenceSourceExact},
		{name: "git prefixed shorthand", source: "git:github.com/acme/tools.git@v1", wantClass: extensiontopology.CarrierSourceGit, wantIdentity: "github.com/acme/tools", wantEvidence: extensiontopology.RelationEvidenceSourceExact},
		{name: "git prefixed scp", source: "git:git@github.com:acme/tools.git@v1", wantClass: extensiontopology.CarrierSourceGit, wantIdentity: "github.com/acme/tools", wantEvidence: extensiontopology.RelationEvidenceSourceExact},
		{name: "explicit git URL", source: "https://github.com/acme/tools.git@v1", wantClass: extensiontopology.CarrierSourceGit, wantIdentity: "github.com/acme/tools", wantEvidence: extensiontopology.RelationEvidenceSourceExact},
		{name: "relative local", source: "./packages/tools", wantClass: extensiontopology.CarrierSourceLocal, wantIdentity: "./packages/tools", wantEvidence: extensiontopology.RelationEvidenceSourceExact},
		{name: "bare host path", source: "github.com/acme/tools", wantClass: extensiontopology.CarrierSourceLocal, wantIdentity: "github.com/acme/tools", wantEvidence: extensiontopology.RelationEvidenceSourceExact},
		{name: "github prefix falls back local", source: "github:acme/tools", wantClass: extensiontopology.CarrierSourceLocal, wantIdentity: "github:acme/tools", wantEvidence: extensiontopology.RelationEvidenceSourceExact},
		{name: "uppercase protocol stays local", source: "HTTPS://github.com/acme/tools", wantClass: extensiontopology.CarrierSourceLocal, wantIdentity: "HTTPS://github.com/acme/tools", wantEvidence: extensiontopology.RelationEvidenceSourceExact},
		{name: "invalid remote stays local", source: "https://missing-path", wantClass: extensiontopology.CarrierSourceLocal, wantIdentity: "https://missing-path", wantEvidence: extensiontopology.RelationEvidenceSourceExact},
		{name: "unsafe git stays local", source: "git:git@evil.example:../../victim/repo", wantClass: extensiontopology.CarrierSourceLocal, wantIdentity: "git:git@evil.example:../../victim/repo", wantEvidence: extensiontopology.RelationEvidenceSourceExact},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source, err := desiredextension.NewSourceRef(desiredextension.SourceKindHostSource, test.source)
			if err != nil {
				t.Fatalf("NewSourceRef: %v", err)
			}
			key, err := desiredextension.NewCarrierKey(
				desiredextension.CarrierPiPackage,
				target.TargetPi,
				target.ScopeGlobal,
				source,
			)
			if err != nil {
				t.Fatalf("NewCarrierKey: %v", err)
			}
			interpreted, err := extensiontopology.InterpretCarrierSource(key)
			if err != nil {
				t.Fatalf("InterpretCarrierSource: %v", err)
			}
			if interpreted.Class() != test.wantClass ||
				interpreted.Identity() != test.wantIdentity ||
				interpreted.RelationEvidence() != test.wantEvidence {
				t.Fatalf(
					"source = class:%q identity:%q evidence:%q, want class:%q identity:%q evidence:%q",
					interpreted.Class(),
					interpreted.Identity(),
					interpreted.RelationEvidence(),
					test.wantClass,
					test.wantIdentity,
					test.wantEvidence,
				)
			}
		})
	}
}

func TestInterpretCarrierSourceSeparatesAntigravitySelectorFromOpaqueHostSource(t *testing.T) {
	tests := []struct {
		name             string
		source           string
		wantClass        extensiontopology.CarrierSourceClass
		wantRelationName string
		wantEvidence     extensiontopology.RelationEvidenceClass
	}{
		{
			name:             "marketplace selector",
			source:           "modern-web-guidance@google",
			wantClass:        extensiontopology.CarrierSourceMarketplace,
			wantRelationName: "modern-web-guidance",
			wantEvidence:     extensiontopology.RelationEvidenceBoundedSameSubject,
		},
		{
			name:             "relative local path",
			source:           "./plugins/guidance",
			wantClass:        extensiontopology.CarrierSourceHost,
			wantRelationName: "./plugins/guidance",
			wantEvidence:     extensiontopology.RelationEvidenceUnavailable,
		},
		{
			name:             "path containing at sign",
			source:           "./plugins/guidance@local",
			wantClass:        extensiontopology.CarrierSourceHost,
			wantRelationName: "./plugins/guidance@local",
			wantEvidence:     extensiontopology.RelationEvidenceUnavailable,
		},
		{
			name:             "multiple separators",
			source:           "guidance@google@other",
			wantClass:        extensiontopology.CarrierSourceHost,
			wantRelationName: "guidance@google@other",
			wantEvidence:     extensiontopology.RelationEvidenceUnavailable,
		},
		{
			name:             "non-token plugin name",
			source:           "team/guidance@google",
			wantClass:        extensiontopology.CarrierSourceHost,
			wantRelationName: "team/guidance@google",
			wantEvidence:     extensiontopology.RelationEvidenceUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source, err := desiredextension.NewSourceRef(
				desiredextension.SourceKindHostSource,
				test.source,
			)
			if err != nil {
				t.Fatal(err)
			}
			key, err := desiredextension.NewCarrierKey(
				desiredextension.CarrierAntigravityCLIPlugin,
				target.TargetAntigravityCLI,
				target.ScopeGlobal,
				source,
			)
			if err != nil {
				t.Fatal(err)
			}
			interpreted, err := extensiontopology.InterpretCarrierSource(key)
			if err != nil {
				t.Fatal(err)
			}
			if interpreted.Class() != test.wantClass ||
				interpreted.Identity() != test.source ||
				interpreted.RelationIdentity() != test.wantRelationName ||
				interpreted.RelationEvidence() != test.wantEvidence {
				t.Fatalf(
					"source = class:%q identity:%q relation:%q evidence:%q, want class:%q identity:%q relation:%q evidence:%q",
					interpreted.Class(),
					interpreted.Identity(),
					interpreted.RelationIdentity(),
					interpreted.RelationEvidence(),
					test.wantClass,
					test.source,
					test.wantRelationName,
					test.wantEvidence,
				)
			}
		})
	}

	classes := extensiontopology.CarrierSourceClasses(
		desiredextension.CarrierAntigravityCLIPlugin,
	)
	if len(classes) != 2 ||
		classes[0] != extensiontopology.CarrierSourceMarketplace ||
		classes[1] != extensiontopology.CarrierSourceHost {
		t.Fatalf("Antigravity source classes = %#v", classes)
	}
}

func TestHostVisibleRelationKeyNarrowsOnlyAntigravitySelectorProvenance(t *testing.T) {
	tests := []struct {
		name    string
		carrier desiredextension.Carrier
		target  target.Target
		source  string
		want    string
	}{
		{
			name:    "Antigravity selector",
			carrier: desiredextension.CarrierAntigravityCLIPlugin,
			target:  target.TargetAntigravityCLI,
			source:  "guidance@google",
			want:    "guidance",
		},
		{
			name:    "Antigravity opaque source",
			carrier: desiredextension.CarrierAntigravityCLIPlugin,
			target:  target.TargetAntigravityCLI,
			source:  "./guidance",
			want:    "./guidance",
		},
		{
			name:    "Pi versioned source remains exact",
			carrier: desiredextension.CarrierPiPackage,
			target:  target.TargetPi,
			source:  "npm:@acme/tools@1.2.3",
			want:    "npm:@acme/tools@1.2.3",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source, err := desiredextension.NewSourceRef(
				desiredextension.SourceKindHostSource,
				test.source,
			)
			if err != nil {
				t.Fatal(err)
			}
			key, err := desiredextension.NewCarrierKey(
				test.carrier,
				test.target,
				target.ScopeGlobal,
				source,
			)
			if err != nil {
				t.Fatal(err)
			}
			got, err := extensiontopology.HostVisibleRelationKey(key)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("HostVisibleRelationKey() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestInterpretCarrierSourceRejectsUnadmittedCarrierAndMalformedNPM(t *testing.T) {
	piSource, err := desiredextension.NewSourceRef(desiredextension.SourceKindHostSource, "npm:")
	if err != nil {
		t.Fatal(err)
	}
	piKey, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		target.ScopeGlobal,
		piSource,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := extensiontopology.InterpretCarrierSource(piKey); err == nil {
		t.Fatal("empty npm source was classified")
	}

	marketplace, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindMarketplace,
		"context7@official",
	)
	if err != nil {
		t.Fatal(err)
	}
	claudeKey, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierClaudeCodePlugin,
		target.TargetClaudeCode,
		target.ScopeProject,
		marketplace,
	)
	if err != nil {
		t.Fatal(err)
	}
	claudeSource, err := extensiontopology.InterpretCarrierSource(claudeKey)
	if err != nil {
		t.Fatalf("Claude marketplace source: %v", err)
	}
	if claudeSource.Class() != extensiontopology.CarrierSourceMarketplace ||
		claudeSource.Identity() != "context7@official" {
		t.Fatalf("Claude marketplace source = %#v", claudeSource)
	}
	classes := extensiontopology.CarrierSourceClasses(desiredextension.CarrierPiPackage)
	if len(classes) != 3 {
		t.Fatalf("Pi source classes = %#v", classes)
	}
	classes[0] = "forged"
	if extensiontopology.CarrierSourceClasses(desiredextension.CarrierPiPackage)[0] == "forged" {
		t.Fatal("carrier source classes exposed mutable catalog storage")
	}
	claudeClasses := extensiontopology.CarrierSourceClasses(desiredextension.CarrierClaudeCodePlugin)
	if len(claudeClasses) != 1 || claudeClasses[0] != extensiontopology.CarrierSourceMarketplace {
		t.Fatalf("Claude source classes = %#v, want marketplace", claudeClasses)
	}
	claudeClasses[0] = "forged"
	if extensiontopology.CarrierSourceClasses(desiredextension.CarrierClaudeCodePlugin)[0] == "forged" {
		t.Fatal("Claude source classes exposed mutable catalog storage")
	}

	codexKey, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierCodexPlugin,
		target.TargetCodex,
		target.ScopeGlobal,
		marketplace,
	)
	if err != nil {
		t.Fatal(err)
	}
	codexSource, err := extensiontopology.InterpretCarrierSource(codexKey)
	if err != nil {
		t.Fatalf("Codex marketplace source: %v", err)
	}
	if codexSource.Class() != extensiontopology.CarrierSourceMarketplace ||
		codexSource.Identity() != "context7@official" {
		t.Fatalf("Codex marketplace source = %#v", codexSource)
	}
	codexClasses := extensiontopology.CarrierSourceClasses(desiredextension.CarrierCodexPlugin)
	if len(codexClasses) != 1 || codexClasses[0] != extensiontopology.CarrierSourceMarketplace {
		t.Fatalf("Codex source classes = %#v, want marketplace", codexClasses)
	}
}
