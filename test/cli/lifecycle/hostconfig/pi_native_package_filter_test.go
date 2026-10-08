package cli_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/test/testkit"
)

func TestPiNativeMCPAdapterResourceSelection(t *testing.T) {
	t.Setenv("SOURCE_TOKEN", "fixture-runtime-secret")
	for _, test := range []struct {
		name, user, project string
		local, global       bool
	}{
		{"user empty", `{"packages":[{"source":"npm:pi-mcp-adapter@2.15.0","extensions":[]}]}`, `{}`, true, true},
		{"project empty", `{}`, `{"packages":[{"source":"npm:pi-mcp-adapter@2.15.0","extensions":[]}]}`, true, true},
		{"project cannot mask pretrust user", `{"packages":["npm:pi-mcp-adapter@2.13.0"]}`, `{"packages":[{"source":"npm:pi-mcp-adapter@2.15.0","extensions":[]}]}`, false, false},
		{"enabled project replaces disabled user", `{"packages":[{"source":"npm:pi-mcp-adapter@2.13.0","extensions":[]}]}`, `{"packages":["npm:pi-mcp-adapter@2.15.0"]}`, false, false},
		{"empty delta preserves user", `{"packages":["npm:pi-mcp-adapter@2.15.0"]}`, `{"packages":[{"source":"npm:pi-mcp-adapter@2.15.0","extensions":[],"autoload":false}]}`, false, false},
		{"empty delta alone", `{}`, `{"packages":[{"source":"npm:pi-mcp-adapter@2.15.0","extensions":[],"autoload":false}]}`, true, true},
		{"user duplicate first disabled", `{"packages":[{"source":"npm:pi-mcp-adapter@2.15.0","extensions":[]},"npm:pi-mcp-adapter@2.13.0"]}`, `{}`, true, true},
		{"user duplicate first enabled", `{"packages":["npm:pi-mcp-adapter@2.15.0",{"source":"npm:pi-mcp-adapter@2.13.0","extensions":[]}]}`, `{}`, false, false},
		{"project duplicate last disabled", `{}`, `{"packages":["npm:pi-mcp-adapter@2.13.0",{"source":"npm:pi-mcp-adapter@2.15.0","extensions":[]}]}`, true, true},
		{"root exclusion", `{}`, adapterResourceSettings(`["!index.ts"]`, false), true, true},
		{"exact normalized exclusion", `{}`, adapterResourceSettings(`["-./index.ts"]`, false), true, true},
		{"positive excludes root", `{}`, adapterResourceSettings(`["extensions/*.ts"]`, false), true, true},
		{"plain dot prefix not normalized", `{}`, adapterResourceSettings(`["./index.ts"]`, false), true, true},
		{"unmatched exclusion leaves root", `{}`, adapterResourceSettings(`["!extensions/*.ts"]`, false), false, false},
		{"positive enables root", `{}`, adapterResourceSettings(`["index.ts"]`, false), false, false},
		{"force inclusion overrides exclusion", `{}`, adapterResourceSettings(`["!index.ts","+./index.ts"]`, false), false, false},
		{"normal force exclusion wins", `{}`, adapterResourceSettings(`["-index.ts","+index.ts"]`, false), true, true},
		{"delta last inclusion wins", `{}`, adapterResourceSettings(`["-index.ts","+index.ts"]`, true), false, false},
		{"delta last exclusion wins", `{}`, adapterResourceSettings(`["+index.ts","-index.ts"]`, true), true, true},
		{"delta unmatched preserves user", `{"packages":["npm:pi-mcp-adapter@2.15.0"]}`, adapterResourceSettings(`["other.ts"]`, true), false, false},
		{"delta cannot mask pretrust user", `{"packages":["npm:pi-mcp-adapter@2.15.0"]}`, adapterResourceSettings(`["-index.ts"]`, true), false, false},
		{"empty top-level does not disable package", `{}`, `{"extensions":[],"packages":["npm:pi-mcp-adapter@2.15.0"]}`, false, false},
		{"package builtin pattern does not disable builtin", `{}`, adapterResourceSettings(`["-builtin:mcp"]`, false), false, false},
		{"null filter refused", `{}`, adapterResourceSettings(`null`, false), false, false},
		{"null filter member refused", `{}`, adapterResourceSettings(`[null]`, false), false, false},
		{"wrong filter type refused", `{}`, adapterResourceSettings(`true`, false), false, false},
		{"unsupported glob refused", `{}`, adapterResourceSettings(`["**/*.ts"]`, false), false, false},
	} {
		for _, scope := range []string{"project", "global"} {
			t.Run(test.name+"/"+scope, func(t *testing.T) {
				project := newMCPCLIProject(t)
				installNativeVersionFixture(t, project.root, "1.0.2")
				agentRoot := filepath.Join(project.root, "agent")
				t.Setenv("PI_CODING_AGENT_DIR", agentRoot)
				testkit.WriteFile(t, agentRoot, "settings.json", test.user)
				testkit.WriteFile(t, project.root, ".pi/settings.json", test.project)
				metadata := `{"name":"pi-mcp-adapter","version":"2.15.0","pi":{"extensions":["./index.ts"]}}`
				for _, root := range []string{agentRoot, filepath.Join(project.root, ".pi")} {
					testkit.WriteFile(t, root, "npm/node_modules/pi-mcp-adapter/package.json", metadata)
					testkit.WriteFile(t, root, "npm/node_modules/pi-mcp-adapter/index.ts", "// Not executed.\n")
				}
				testkit.WriteFile(t, project.root, "daem.toml", nativePiManifest(scope))
				runMCPLock(t, project)
				lockBefore := string(testkit.ReadFile(t, project.lockfilePath))
				exit, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes")
				accepted := test.local
				configPath := filepath.Join(project.root, aggregate.PiProjectMCPConfigPath)
				if scope == "global" {
					accepted = test.global
					configPath = filepath.Join(agentRoot, "mcp.json")
				}
				if (exit == 0) != accepted {
					t.Fatalf("filtered package qualification = %d, want accepted=%t: %s/%s", exit, accepted, stdout, stderr)
				}
				if accepted {
					exit, stdout, stderr = runMCPCLI(t, "status", "--manifest", project.manifestPath, "--check", "--json")
					if exit != 0 {
						t.Fatalf("filtered package convergence = %d: %s/%s", exit, stdout, stderr)
					}
				} else {
					testkit.AssertPathMissing(t, configPath)
					testkit.AssertPathMissing(t, filepath.Join(project.root, ".daem", "state.json"))
				}
				testkit.AssertFileContent(t, project.lockfilePath, lockBefore)
				testkit.AssertFileContent(t, filepath.Join(agentRoot, "settings.json"), test.user)
				testkit.AssertFileContent(t, filepath.Join(project.root, ".pi", "settings.json"), test.project)
				for _, root := range []string{agentRoot, filepath.Join(project.root, ".pi")} {
					testkit.AssertFileContent(t, filepath.Join(root, "npm", "node_modules", "pi-mcp-adapter", "package.json"), metadata)
				}
			})
		}
	}
}

func adapterResourceSettings(patterns string, delta bool) string {
	return fmt.Sprintf(`{"packages":[{"source":"npm:pi-mcp-adapter@2.15.0","extensions":%s,"autoload":%t}]}`, patterns, !delta)
}

func TestPiNativeMCPUnobservedFilteredPackagePreservesManagedOutput(t *testing.T) {
	project := newMCPCLIProject(t)
	installNativeVersionFixture(t, project.root, "1.0.2")
	t.Setenv("SOURCE_TOKEN", "fixture-runtime-secret")
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(project.root, "agent"))
	testkit.WriteFile(t, project.root, "daem.toml", nativePiManifest("project"))
	testkit.WriteFile(t, project.root, ".pi/settings.json", adapterResourceSettings(`[]`, false))
	runMCPLock(t, project)
	exit, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes")
	if exit != 0 {
		t.Fatalf("inventory-independent empty filter = %d: %s/%s", exit, stdout, stderr)
	}
	configPath := filepath.Join(project.root, aggregate.PiProjectMCPConfigPath)
	statePath := filepath.Join(project.root, ".daem", "state.json")
	configBefore, stateBefore := string(testkit.ReadFile(t, configPath)), string(testkit.ReadFile(t, statePath))
	lockBefore := string(testkit.ReadFile(t, project.lockfilePath))
	settings := adapterResourceSettings(`["-index.ts"]`, false)
	testkit.WriteFile(t, project.root, ".pi/settings.json", settings)
	exit, stdout, stderr = runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes")
	if exit == 0 {
		t.Fatalf("unobserved installed inventory permitted Native publication: %s/%s", stdout, stderr)
	}
	testkit.AssertFileContent(t, configPath, configBefore)
	testkit.AssertFileContent(t, statePath, stateBefore)
	testkit.AssertFileContent(t, project.lockfilePath, lockBefore)
	testkit.AssertFileContent(t, filepath.Join(project.root, ".pi", "settings.json"), settings)
}
