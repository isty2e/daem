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

func TestPiNativeQualificationRefusesKnownGitAdapterSources(t *testing.T) {
	for _, source := range []string{
		"git:github.com/nicobailon/pi-mcp-adapter",
		"git:github.com/nicobailon/pi-mcp-adapter@v2.13.0",
		"git:github:nicobailon/pi-mcp-adapter@main",
		"git:nicobailon/pi-mcp-adapter",
		"https://github.com/nicobailon/pi-mcp-adapter.git",
		"git://github.com/nicobailon/pi-mcp-adapter.git",
		"git:ssh://git@github.com/nicobailon/pi-mcp-adapter.git@main",
		"git:git@github.com:nicobailon/pi-mcp-adapter.git",
	} {
		for _, filtered := range []bool{false, true} {
			for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
				t.Run(fmt.Sprintf("%s/%s/filtered=%t", source, scope, filtered), func(t *testing.T) {
					workDir, agentRoot := t.TempDir(), t.TempDir()
					entry := map[string]any{"source": source}
					if filtered {
						entry["extensions"] = []string{}
					}
					wire, err := json.Marshal(map[string]any{"packages": []any{map[string]any{"source": "npm:pi-mcp-adapter@2.15.0", "extensions": []string{}}, entry}})
					if err != nil {
						t.Fatal(err)
					}
					writeEffectiveConfig(t, filepath.Join(agentRoot, "settings.json"), string(wire))
					writeEffectiveConfig(t, filepath.Join(workDir, ".pi", "settings.json"), `{"packages":[{"source":"npm:pi-mcp-adapter@2.15.0","extensions":[]}]}`)

					contract, _ := profile.PiMCPContractForCodec(aggregate.MCPCodecPiNativeStdio)
					if err := qualifyPiNativeSettings(t.Context(), contract, scope, workDir, agentRoot, profile.ObservePiMCPVersion("1.0.2")); err == nil {
						t.Fatal("known Git Adapter was mistaken for an absent npm Adapter")
					}
				})
			}
		}
	}
}

func TestPiNativeLocalAdapterIntentUsesMetadataAndSettingsBase(t *testing.T) {
	for _, test := range []struct {
		name, spelling, metadata string
		accepted                 bool
	}{
		{"renamed absolute checkout", "absolute", `{"name":"pi-mcp-adapter","pi":{"extensions":["index.ts"]}}`, false},
		{"relative checkout", "relative", `{"name":"pi-mcp-adapter"}`, false},
		{"file URL checkout", "url", `{"name":"pi-mcp-adapter"}`, false},
		{"tilde checkout", "tilde", `{"name":"pi-mcp-adapter"}`, false},
		{"direct file bypasses empty filter", "file", `{"name":"pi-mcp-adapter"}`, false},
		{"basename is not package intent", "basename", `{"name":"unrelated-extension"}`, true},
		{"malformed present metadata", "absolute", `{"name":`, false},
		{"ambiguous metadata", "absolute", `{"name":"pi-mcp-adapter","name":"unrelated-extension"}`, false},
		{"unlabelled custom extension", "file", `{}`, true},
		{"missing package", "missing", "", true},
	} {
		for _, packageScope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			for _, bindingScope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
				t.Run(test.name+"/"+string(packageScope)+"/"+string(bindingScope), func(t *testing.T) {
					workDir, agentRoot, home := t.TempDir(), t.TempDir(), t.TempDir()
					t.Setenv("HOME", home)
					t.Setenv("USERPROFILE", home)
					base := agentRoot
					if packageScope == target.ScopeProject {
						base = filepath.Join(workDir, ".pi")
					}
					packageRoot := filepath.Join(base, "renamed checkout")
					if test.spelling == "basename" {
						packageRoot = filepath.Join(base, "pi-mcp-adapter")
					} else if test.spelling == "tilde" {
						packageRoot = filepath.Join(home, "renamed checkout")
					}
					if test.metadata != "" {
						writeEffectiveConfig(t, filepath.Join(packageRoot, "package.json"), test.metadata)
						writeEffectiveConfig(t, filepath.Join(packageRoot, "index.ts"), "export {};\n")
					}
					source := packageRoot
					switch test.spelling {
					case "relative":
						source = "./renamed checkout"
					case "url":
						urlPath := filepath.ToSlash(packageRoot)
						if filepath.VolumeName(packageRoot) != "" {
							urlPath = "/" + urlPath
						}
						source = (&url.URL{Scheme: "file", Path: urlPath}).String()
					case "tilde":
						source = "~/renamed checkout"
					case "file":
						source = filepath.Join(packageRoot, "index.ts")
					}
					entry := map[string]any{"source": source, "extensions": []string{}}
					wire, err := json.Marshal(map[string]any{"packages": []any{entry}})
					if err != nil {
						t.Fatal(err)
					}
					writeEffectiveConfig(t, filepath.Join(base, "settings.json"), string(wire))

					contract, _ := profile.PiMCPContractForCodec(aggregate.MCPCodecPiNativeStdio)
					qualification := qualifyPiNativeSettings(t.Context(), contract, bindingScope, workDir, agentRoot, profile.ObservePiMCPVersion("1.0.2"))
					if (qualification == nil) != test.accepted {
						t.Fatalf("local-source qualification = %v, accepted=%t", qualification, test.accepted)
					}
				})
			}
		}
	}
}
