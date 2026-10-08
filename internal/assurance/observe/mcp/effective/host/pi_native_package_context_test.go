package host

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

func TestPiNativePackageQualificationUsesApplicableManagerContext(t *testing.T) {
	for _, test := range []struct {
		name, user, project, argv string
		managed, delta, accepted  bool
	}{
		{"same user-approved manager", `["pnpm"]`, `["pnpm"]`, "list -g --depth 0 --json", false, false, true},
		{"project null default", "", "null", "root -g", false, false, true},
		{"project empty default", "null", "[]", "root -g", false, false, true},
		{"project direct default", "[]", `["npm"]`, "root -g", false, false, true},
		{"different project manager", `["pnpm"]`, `["npm"]`, "", false, false, false},
		{"project delta cannot authorize manager", `["pnpm"]`, `["npm"]`, "", false, true, false},
		{"managed inventory needs no command", `["pnpm"]`, `["npm","install"]`, "", true, false, true},
	} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			t.Run(test.name+"/"+string(scope), func(t *testing.T) {
				workDir, agentRoot := t.TempDir(), t.TempDir()
				legacyBase := filepath.Join(workDir, "legacy", "node_modules")
				packageRoot := filepath.Join(legacyBase, "pi-mcp-adapter")
				wire, err := json.Marshal(packageRoot)
				if err != nil {
					t.Fatal(err)
				}
				listing := `[{"dependencies":{"pi-mcp-adapter":{"path":` + string(wire) + `}}}]`
				manager := "npm"
				if test.user == `["pnpm"]` {
					manager = "pnpm"
				}
				_, log := installNativeRootQueryFixture(t, manager, legacyBase, listing)
				if test.managed {
					packageRoot = filepath.Join(agentRoot, "npm", "node_modules", "pi-mcp-adapter")
				}
				userPackage := `{"source":"npm:pi-mcp-adapter@^2.13.0","extensions":["-index.ts"]}`
				projectPackage := ""
				if test.delta {
					projectPackage = `,"packages":[{"source":"npm:pi-mcp-adapter@2.13.0","autoload":false,"extensions":["-index.ts"]}]`
				}
				userCommand := ""
				if test.user != "" {
					userCommand = `,"npmCommand":` + test.user
				}
				writeEffectiveConfig(t, filepath.Join(agentRoot, "settings.json"), `{"packages":[`+userPackage+`]`+userCommand+`}`)
				writeEffectiveConfig(t, filepath.Join(workDir, ".pi", "settings.json"), `{"npmCommand":`+test.project+projectPackage+`}`)
				writeEffectiveConfig(t, filepath.Join(packageRoot, "package.json"), `{"name":"pi-mcp-adapter","version":"2.15.0","pi":{"extensions":["index.ts"]}}`)
				contract, _ := profile.PiMCPContractForCodec(aggregate.MCPCodecPiNativeStdio)

				qualification := qualifyPiNativeSettings(t.Context(), contract, scope, workDir, agentRoot, profile.ObservePiMCPVersion("1.0.2"))
				if (qualification == nil) != test.accepted {
					t.Fatalf("manager-context qualification = %v, accepted=%t", qualification, test.accepted)
				}
				if test.argv == "" {
					assertNativeRootQueryLog(t, log, "")
					return
				}
				calls, err := os.ReadFile(log)
				if err != nil || len(calls) == 0 {
					t.Fatalf("missing approved location query: %v", err)
				}
				for _, call := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
					if call != test.argv {
						t.Fatalf("unexpected manager operation %q", call)
					}
				}
			})
		}
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
