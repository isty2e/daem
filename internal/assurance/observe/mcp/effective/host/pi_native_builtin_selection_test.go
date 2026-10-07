package host

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

func TestPiNativeBuiltinQualificationMatchesUpstreamSelections(t *testing.T) {
	// The admission outcomes come from Pi v1.0.2's package-manager resolver,
	// not from the local selector implementation.
	content, err := os.ReadFile("testdata/pi_builtin_selection_1_0_2.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name           string   `json:"name"`
		Global         []string `json:"global"`
		Project        []string `json:"project"`
		GlobalEnabled  bool     `json:"globalEnabled"`
		ProjectEnabled bool     `json:"projectEnabled"`
	}
	if err := json.Unmarshal(content, &cases); err != nil {
		t.Fatal(err)
	}
	contract, _ := profile.PiMCPContractForCodec(aggregate.MCPCodecPiNativeStdio)
	for _, test := range cases {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			t.Run(test.Name+"/"+string(scope), func(t *testing.T) {
				root := t.TempDir()
				workDir, agentRoot := filepath.Join(root, "project"), filepath.Join(root, "agent")
				for path, directives := range map[string][]string{
					filepath.Join(agentRoot, "settings.json"):      test.Global,
					filepath.Join(workDir, ".pi", "settings.json"): test.Project,
				} {
					settings, err := json.Marshal(map[string][]string{"extensions": directives})
					if err != nil {
						t.Fatal(err)
					}
					writeEffectiveConfig(t, path, string(settings))
				}

				accepted := test.ProjectEnabled && (scope == target.ScopeProject || test.GlobalEnabled)
				err := qualifyPiNativeSettings(contract, scope, workDir, agentRoot, profile.ObservePiMCPVersion("1.0.2"))
				if (err == nil) != accepted {
					t.Fatalf("qualification accepted=%t, want %t: %v", err == nil, accepted, err)
				}
			})
		}
	}
}
