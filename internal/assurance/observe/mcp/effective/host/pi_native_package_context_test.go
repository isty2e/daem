package host

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

func TestPiNativePackageQualificationUsesApplicableManagerContext(t *testing.T) {
	for _, test := range []struct {
		name     string
		scope    target.Scope
		delta    bool
		accepted bool
		calls    string
	}{
		{"project effective manager", target.ScopeProject, false, true, "list -g --depth 0 --json\n"},
		{"project delta retains user artifact", target.ScopeProject, true, true, "list -g --depth 0 --json\n"},
		{"global also requires user-only inventory", target.ScopeGlobal, false, false, "list -g --depth 0 --json\nroot -g\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			workDir, agentRoot := t.TempDir(), t.TempDir()
			packageRoot := filepath.Join(workDir, "pnpm-store", "user-adapter")
			packageJSON, err := json.Marshal(packageRoot)
			if err != nil {
				t.Fatal(err)
			}
			listing := `[{"dependencies":{"pi-mcp-adapter":{"path":` + string(packageJSON) + `}}}]`
			npm, _ := installNativeRootQueryFixture(t, "npm", filepath.Join(workDir, "absent-npm-root"), listing)
			pnpm, log := installNativeRootQueryFixture(t, "pnpm", filepath.Join(workDir, "absent-npm-root"), listing)
			npmJSON, err := json.Marshal([]string{npm})
			if err != nil {
				t.Fatal(err)
			}
			pnpmJSON, err := json.Marshal([]string{pnpm})
			if err != nil {
				t.Fatal(err)
			}
			userPackage := `{"source":"npm:pi-mcp-adapter@^2.13.0","extensions":["-index.ts"]}`
			projectPackage := ""
			if test.delta {
				userPackage = `"npm:pi-mcp-adapter@^2.13.0"`
				projectPackage = `,"packages":[{"source":"npm:pi-mcp-adapter@2.13.0","autoload":false,"extensions":["-index.ts"]}]`
			}
			writeEffectiveConfig(t, filepath.Join(agentRoot, "settings.json"), `{"packages":[`+userPackage+`],"npmCommand":`+string(npmJSON)+`}`)
			writeEffectiveConfig(t, filepath.Join(workDir, ".pi", "settings.json"), `{"npmCommand":`+string(pnpmJSON)+projectPackage+`}`)
			writeEffectiveConfig(t, filepath.Join(packageRoot, "package.json"), `{"name":"pi-mcp-adapter","version":"2.15.0","pi":{"extensions":["index.ts"]}}`)
			contract, ok := profile.PiMCPContractForCodec(aggregate.MCPCodecPiNativeStdio)
			if !ok {
				t.Fatal("native contract fixture is unavailable")
			}

			qualification := qualifyPiNativeSettings(t.Context(), contract, test.scope, workDir, agentRoot, profile.ObservePiMCPVersion("1.0.2"))
			if (qualification == nil) != test.accepted {
				t.Fatalf("manager-context qualification = %v, accepted=%t", qualification, test.accepted)
			}
			assertNativeRootQueryLog(t, log, test.calls)
		})
	}
}

func TestPiNativePackageInventoryNeverRescuesSelectedMetadata(t *testing.T) {
	for _, managed := range []bool{false, true} {
		for _, metadata := range []string{"malformed", `{"name":"pi-mcp-adapter","version":"2.14.0","pi":{"extensions":["index.ts"]}}`} {
			t.Run(fmt.Sprintf("%s/managed=%t", metadata, managed), func(t *testing.T) {
				workDir, agentRoot := t.TempDir(), t.TempDir()
				legacyBase := filepath.Join(workDir, "legacy", "node_modules")
				legacy := filepath.Join(legacyBase, "pi-mcp-adapter")
				writeEffectiveConfig(t, filepath.Join(legacy, "package.json"), metadata)
				if managed {
					writeEffectiveConfig(t, filepath.Join(agentRoot, "npm", "node_modules", "pi-mcp-adapter", "package.json"), metadata)
					writeEffectiveConfig(t, filepath.Join(legacy, "package.json"), `{"name":"pi-mcp-adapter","version":"2.15.0","pi":{"extensions":["index.ts"]}}`)
				}
				command, log := installNativeRootQueryFixture(t, "npm", legacyBase, "[]")
				commandJSON, err := json.Marshal([]string{command})
				if err != nil {
					t.Fatal(err)
				}
				entry := piNativeAdapterPackage{source: "npm:pi-mcp-adapter@2.15.0", scope: target.ScopeGlobal, filtered: true, patterns: []string{"-index.ts"}}
				selected, err := nativeAdapterResourcesSelected(t.Context(), []piNativeAdapterPackage{entry}, piNativePackageContext{workDir: workDir, agentRoot: agentRoot, npmCommand: commandJSON})
				if err == nil || selected {
					t.Fatalf("invalid selected metadata qualified Native: selected=%t, %v", selected, err)
				}
				calls := "root -g\n"
				if managed {
					calls = ""
				}
				assertNativeRootQueryLog(t, log, calls)
			})
		}
	}
}

func TestPiNativeInventoryIndependentSelectionSkipsUnadmittedCommand(t *testing.T) {
	for _, packages := range [][]piNativeAdapterPackage{
		{{source: "npm:pi-mcp-adapter@2.15.0", scope: target.ScopeGlobal, filtered: true}},
		{{source: "npm:pi-mcp-adapter@2.15.0", scope: target.ScopeGlobal, delta: true}},
	} {
		_, log := installNativeRootQueryFixture(t, "npm", "/available", "[]")
		selected, err := nativeAdapterResourcesSelected(t.Context(), packages, piNativePackageContext{workDir: t.TempDir(), agentRoot: t.TempDir(), npmCommand: json.RawMessage(`["npm","install"]`)})
		if err != nil || selected {
			t.Fatalf("inventory-independent disabled selection = %t, %v", selected, err)
		}
		assertNativeRootQueryLog(t, log, "")
	}
}
