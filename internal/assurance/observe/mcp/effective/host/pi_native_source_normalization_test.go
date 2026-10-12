package host

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

func TestPiNativePaddedLocalSourcesUsePiResolutionBoundary(t *testing.T) {
	for _, spelling := range []string{"relative", "absolute", "file", "URL", "tilde", "base", "npm-looking local"} {
		for _, packageScope := range []target.Scope{target.ScopeGlobal, target.ScopeProject} {
			for _, bindingScope := range []target.Scope{target.ScopeGlobal, target.ScopeProject} {
				t.Run(spelling+"/"+string(packageScope)+"/"+string(bindingScope), func(t *testing.T) {
					if spelling == "npm-looking local" && filepath.Separator == '\\' {
						t.Skip("colon-bearing local directory is a Unix fixture")
					}
					workDir, agentRoot, home := t.TempDir(), t.TempDir(), t.TempDir()
					t.Setenv("HOME", home)
					t.Setenv("USERPROFILE", home)
					base := agentRoot
					if packageScope == target.ScopeProject {
						base = filepath.Join(workDir, ".pi")
					}
					packageRoot := filepath.Join(base, "adapter")
					if spelling == "tilde" {
						packageRoot = filepath.Join(home, "adapter")
					} else if spelling == "base" {
						packageRoot = base
					} else if spelling == "npm-looking local" {
						packageRoot = filepath.Join(base, "npm:fixture")
					}
					writeEffectiveConfig(t, filepath.Join(packageRoot, "package.json"), `{"name":"pi-mcp-adapter"}`)
					writeEffectiveConfig(t, filepath.Join(packageRoot, "index.ts"), "export {}")
					source := packageRoot
					switch spelling {
					case "relative":
						source = "./adapter"
					case "file":
						source = "./adapter/index.ts"
					case "URL":
						urlPath := filepath.ToSlash(packageRoot)
						if filepath.VolumeName(packageRoot) != "" {
							urlPath = "/" + urlPath
						}
						source = (&url.URL{Scheme: "file", Path: urlPath}).String()
					case "tilde":
						source = "~/adapter"
					case "base":
						source = "\t\n"
					case "npm-looking local":
						source = "npm:fixture"
					}
					wire, err := json.Marshal(map[string]any{"packages": []any{map[string]any{"source": " " + source + " ", "extensions": []string{}}}})
					if err != nil {
						t.Fatal(err)
					}
					writeEffectiveConfig(t, filepath.Join(base, "settings.json"), string(wire))
					contract, _ := profile.PiMCPContractForCodec(aggregate.MCPCodecPiNativeStdio)
					if err := qualifyPiNativeSettings(t.Context(), contract, bindingScope, workDir, agentRoot, profile.ObservePiMCPVersion("1.0.2")); err == nil {
						t.Fatal("padded local Adapter path was treated as absence")
					}
				})
			}
		}
	}
}

func TestPiNativeKnownRegistryAliasRefusesBeforeNPMFilterInterpretation(t *testing.T) {
	for _, source := range []string{
		"npm:mcp-shim@npm:pi-mcp-adapter@2.15.0",
		"npm:mcp-shim@npm:pi-mcp-adapter",
		"npm:@team/shim@npm:pi-mcp-adapter@^2.15.0",
		"npm:pi-mcp-adapter@npm:pi-mcp-adapter@2.15.0",
	} {
		for _, filtered := range []bool{false, true} {
			for _, packageScope := range []target.Scope{target.ScopeGlobal, target.ScopeProject} {
				for _, bindingScope := range []target.Scope{target.ScopeGlobal, target.ScopeProject} {
					t.Run(fmt.Sprintf("%s/filtered=%t/%s/%s", source, filtered, packageScope, bindingScope), func(t *testing.T) {
						workDir, agentRoot := t.TempDir(), t.TempDir()
						entry := map[string]any{"source": source}
						if filtered {
							entry["extensions"] = []string{}
						}
						wire, err := json.Marshal(map[string]any{"packages": []any{entry}})
						if err != nil {
							t.Fatal(err)
						}
						base := agentRoot
						if packageScope == target.ScopeProject {
							base = filepath.Join(workDir, ".pi")
						}
						writeEffectiveConfig(t, filepath.Join(base, "settings.json"), string(wire))
						contract, _ := profile.PiMCPContractForCodec(aggregate.MCPCodecPiNativeStdio)
						if err := qualifyPiNativeSettings(t.Context(), contract, bindingScope, workDir, agentRoot, profile.ObservePiMCPVersion("1.0.2")); err == nil {
							t.Fatal("known registry alias supplied a false npm-name exclusion proof")
						}
					})
				}
			}
		}
	}
}
