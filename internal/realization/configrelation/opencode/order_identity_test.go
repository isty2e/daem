package opencode_test

import (
	"path/filepath"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	opencodeconfig "github.com/isty2e/daem/internal/realization/configrelation/opencode"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

func TestPhysicalSequenceIDDerivesEveryAdmittedDocumentRole(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		capability, admitted := profile.Profile(target.TargetOpenCode).ExtensionOrder(
			desiredextension.CarrierOpenCodePlugin,
			scope,
		)
		if !admitted {
			t.Fatalf("OpenCode %s order capability is not admitted", scope)
		}
		for _, kind := range []opencodeconfig.ConfigKind{
			opencodeconfig.ConfigServer,
			opencodeconfig.ConfigTUI,
		} {
			for _, variant := range []string{"json", "jsonc"} {
				t.Run(string(scope)+"/"+string(kind)+"/"+variant, func(t *testing.T) {
					sequenceID, err := opencodeconfig.PhysicalSequenceID(
						scope,
						kind,
						filepath.Join("config", string(kind)+"."+variant),
					)
					if err != nil {
						t.Fatal(err)
					}
					want := "opencode:" + string(scope) + ":" +
						string(kind) + "." + variant + ".plugins"
					if string(sequenceID) != want {
						t.Fatalf("sequence id = %q, want %q", sequenceID, want)
					}
					if !capability.AdmitsPhysicalSequenceID(sequenceID) {
						t.Fatalf("OpenCode %s capability does not admit %q", scope, sequenceID)
					}
				})
			}
		}
	}
}

func TestPhysicalSequenceIDRejectsUnsupportedAxes(t *testing.T) {
	tests := []struct {
		name  string
		scope target.Scope
		kind  opencodeconfig.ConfigKind
		path  string
	}{
		{name: "scope", scope: "workspace", kind: opencodeconfig.ConfigServer, path: "opencode.json"},
		{name: "kind", scope: target.ScopeProject, kind: "desktop", path: "opencode.json"},
		{name: "variant", scope: target.ScopeProject, kind: opencodeconfig.ConfigServer, path: "opencode.toml"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := opencodeconfig.PhysicalSequenceID(
				test.scope,
				test.kind,
				test.path,
			); err == nil {
				t.Fatal("PhysicalSequenceID accepted unsupported document identity")
			}
		})
	}
}
