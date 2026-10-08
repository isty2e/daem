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

func TestPiNativeQualificationChecksUserPackagesWithoutProjectTrust(t *testing.T) {
	for _, test := range []struct {
		name, user, project string
		accepted            bool
	}{
		{"ordinary mask", `"npm:pi-mcp-adapter@2.15.0"`, `{"source":"npm:pi-mcp-adapter@2.15.0","extensions":[]}`, false},
		{"delta mask", `"npm:pi-mcp-adapter@2.15.0"`, `{"source":"npm:pi-mcp-adapter@2.15.0","autoload":false,"extensions":["-index.ts"]}`, false},
		{"both disabled", `{"source":"npm:pi-mcp-adapter@2.15.0","extensions":[]}`, `{"source":"npm:pi-mcp-adapter@2.15.0","extensions":[]}`, true},
	} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			t.Run(test.name+"/"+string(scope), func(t *testing.T) {
				workDir, agentRoot := t.TempDir(), t.TempDir()
				writeEffectiveConfig(t, filepath.Join(agentRoot, "settings.json"), `{"packages":[`+test.user+`]}`)
				writeEffectiveConfig(t, filepath.Join(workDir, ".pi", "settings.json"), `{"packages":[`+test.project+`]}`)
				writeEffectiveConfig(t, filepath.Join(agentRoot, "npm", "node_modules", "pi-mcp-adapter", "package.json"), `{"name":"pi-mcp-adapter","version":"2.15.0","pi":{"extensions":["index.ts"]}}`)

				contract, _ := profile.PiMCPContractForCodec(aggregate.MCPCodecPiNativeStdio)
				err := qualifyPiNativeSettings(t.Context(), contract, scope, workDir, agentRoot, profile.ObservePiMCPVersion("1.0.2"))
				if (err == nil) != test.accepted {
					t.Fatalf("trust-unobserved qualification = %v, accepted=%t", err, test.accepted)
				}
			})
		}
	}
}

func TestPiNativeQualificationNeverExecutesProjectSelectedManager(t *testing.T) {
	for _, manager := range []string{"npm", "pnpm", "bun"} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			t.Run(manager+"/"+string(scope), func(t *testing.T) {
				workDir, agentRoot := t.TempDir(), t.TempDir()
				legacyRoot := filepath.Join(workDir, "legacy", "node_modules")
				writeEffectiveConfig(t, filepath.Join(legacyRoot, "pi-mcp-adapter", "package.json"), `{"name":"pi-mcp-adapter","version":"2.15.0","pi":{"extensions":["index.ts"]}}`)
				_, _ = installNativeRootQueryFixture(t, "npm", legacyRoot, "[]")
				projectCommand := filepath.Join(workDir, "repository", manager)
				marker := filepath.Join(workDir, "project-executed")
				response := legacyRoot
				if manager == "pnpm" {
					wire, err := json.Marshal(filepath.Join(legacyRoot, "pi-mcp-adapter"))
					if err != nil {
						t.Fatal(err)
					}
					response = `[{"dependencies":{"pi-mcp-adapter":{"path":` + string(wire) + `}}}]`
				}
				writeEffectiveConfig(t, projectCommand, "#!/bin/sh\nprintf executed > '"+marker+"'\nprintf '%s\\n' '"+response+"'\n")
				if err := os.Chmod(projectCommand, 0o700); err != nil {
					t.Fatal(err)
				}
				command, err := json.Marshal([]string{projectCommand})
				if err != nil {
					t.Fatal(err)
				}
				writeEffectiveConfig(t, filepath.Join(agentRoot, "settings.json"), `{"packages":[{"source":"npm:pi-mcp-adapter@2.15.0","extensions":["-index.ts"]}]}`)
				writeEffectiveConfig(t, filepath.Join(workDir, ".pi", "settings.json"), `{"npmCommand":`+string(command)+`}`)

				contract, _ := profile.PiMCPContractForCodec(aggregate.MCPCodecPiNativeStdio)
				err = qualifyPiNativeSettings(t.Context(), contract, scope, workDir, agentRoot, profile.ObservePiMCPVersion("1.0.2"))
				if err == nil {
					t.Error("project-selected executable qualified Native")
				}
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatalf("project executable ran before trust/authority: %v", err)
				}
			})
		}
	}
}
