package extension

import (
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/target"
)

func TestPiSourceInterpretationDifferenceUsesCarrierMeaning(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, test := range []struct {
			ref  string
			want PiSourceInterpretationDifference
		}{
			{"github:acme/tool#v1", PiSourceDifferenceGitHubShorthand},
			{"git+http://example.test/acme/tool.git", PiSourceDifferenceGitPlusURL},
			{"git+https://example.test/acme/tool.git#v1", PiSourceDifferenceGitPlusURL},
			{"git+ssh://example.test/acme/tool.git", PiSourceDifferenceGitPlusURL},
			{"git+https://router.home.arpa/acme/private.git#v1", PiSourceDifferenceGitPlusURL},
			{"git@github.com:acme/tool", PiSourceDifferenceBareSCP},
			{"git@github.com:acme/tool.git@v1", PiSourceDifferenceBareSCP},
			{"deploy@forge.example:team/tool.git", PiSourceDifferenceBareSCP},
			{"git@router.home.arpa:acme/private.git#v1", PiSourceDifferenceBareSCP},
			{"github:", PiSourceDifferenceNone},
			{"./github:acme/tool", PiSourceDifferenceNone},
			{"git@github.com", PiSourceDifferenceNone},
			{"./git@github.com:acme/tool", PiSourceDifferenceNone},
			{"git:local-fixture", PiSourceDifferenceNone},
			{"git:github:acme/tool", PiSourceDifferenceNone},
			{"git:github.com/acme/tool", PiSourceDifferenceNone},
			{"git:git@github.com:acme/tool", PiSourceDifferenceNone},
			{"ssh://git@github.com/acme/tool", PiSourceDifferenceNone},
			{"https://github.com/acme/tool.git", PiSourceDifferenceNone},
			{"npm:tool@1.0.0", PiSourceDifferenceNone},
		} {
			t.Run(string(scope)+"/"+test.ref, func(t *testing.T) {
				key := piSourceDifferenceKey(t, scope, test.ref)
				before, err := InterpretCarrierSource(key)
				if err != nil {
					t.Fatal(err)
				}
				got, err := PiPackageSourceInterpretationDifference(key)
				if err != nil || got != test.want {
					t.Fatalf("difference=%v error=%v, want %v", got, err, test.want)
				}
				after, err := InterpretCarrierSource(key)
				if err != nil || before != after || key.Source().Ref() != test.ref {
					t.Fatal("difference query changed carrier interpretation or source")
				}
				if got != PiSourceDifferenceNone && before.Class() != CarrierSourceGit {
					t.Fatal("difference was reported without existing Git interpretation")
				}
			})
		}
	}
}

func TestPiSourceInterpretationDifferenceRejectsOtherCarriers(t *testing.T) {
	source, err := desiredextension.NewAuthoredSourceRef(desiredextension.SourceKindHostSource, "github:acme/tool")
	if err != nil {
		t.Fatal(err)
	}
	key, err := desiredextension.NewCarrierKey(desiredextension.CarrierOpenCodePlugin, target.TargetOpenCode, target.ScopeProject, source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PiPackageSourceInterpretationDifference(key); err == nil {
		t.Fatal("Pi-specific query accepted a non-Pi carrier")
	}
}

func piSourceDifferenceKey(t *testing.T, scope target.Scope, ref string) desiredextension.CarrierKey {
	t.Helper()
	source, err := desiredextension.NewAuthoredSourceRef(desiredextension.SourceKindHostSource, ref)
	if err != nil {
		t.Fatal(err)
	}
	key, err := desiredextension.NewCarrierKey(desiredextension.CarrierPiPackage, target.TargetPi, scope, source)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
