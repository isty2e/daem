package host

import (
	"encoding/json"
	"errors"
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
		"https://github.com/nicobailon/pi-mcp-adapter.git",
		"https://GITHUB.COM/nicobailon/pi-mcp-adapter.git",
		"http://GitHub.Com/nicobailon/pi-mcp-adapter.git",
		"ssh://git@GiThUb.CoM/nicobailon/pi-mcp-adapter.git@main",
		"git:GITHUB.COM/nicobailon/pi-mcp-adapter@main",
		"git://github.com/nicobailon/pi-mcp-adapter.git",
		"git://GITHUB.COM/nicobailon/pi-mcp-adapter.git",
		"git:ssh://git@github.com/nicobailon/pi-mcp-adapter.git@main",
		"git:git@github.com:nicobailon/pi-mcp-adapter.git",
		"git:git@GiThUb.CoM:nicobailon/pi-mcp-adapter.git",
	} {
		for _, filtered := range []bool{false, true} {
			for _, packageScope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
				for _, bindingScope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
					t.Run(fmt.Sprintf("%s/%s/%s/filtered=%t", source, packageScope, bindingScope, filtered), func(t *testing.T) {
						workDir, agentRoot := t.TempDir(), t.TempDir()
						log := installNativeQueryCanary(t)
						base := agentRoot
						if packageScope == target.ScopeProject {
							base = filepath.Join(workDir, ".pi")
						}
						var entry any = source
						if filtered {
							entry = map[string]any{"source": source, "extensions": []string{}}
						}
						wire, err := json.Marshal(map[string]any{"packages": []any{entry}})
						if err != nil {
							t.Fatal(err)
						}
						writeEffectiveConfig(t, filepath.Join(base, "settings.json"), string(wire))

						contract, _ := profile.PiMCPContractForCodec(aggregate.MCPCodecPiNativeStdio)
						err = qualifyPiNativeSettings(t.Context(), contract, bindingScope, workDir, agentRoot, profile.ObservePiMCPVersion("1.0.2"))
						if !errors.Is(err, profile.ErrPiNativeAdapterConfigured) {
							t.Fatalf("known Git Adapter qualification = %v", err)
						}
						assertNativeQueryNotCalled(t, log)
					})
				}
			}
		}
	}
}

func TestPiNativeQualificationRefusesUnobservableGitAdapterSource(t *testing.T) {
	workDir, agentRoot := t.TempDir(), t.TempDir()
	writeEffectiveConfig(t, filepath.Join(agentRoot, "settings.json"), `{"packages":["git:nicobailon/pi-mcp-adapter"]}`)
	contract, _ := profile.PiMCPContractForCodec(aggregate.MCPCodecPiNativeStdio)
	err := qualifyPiNativeSettings(t.Context(), contract, target.ScopeProject, workDir, agentRoot, profile.ObservePiMCPVersion("1.0.2"))
	if err == nil || errors.Is(err, profile.ErrPiNativeAdapterConfigured) {
		t.Fatalf("unobservable Git source qualification = %v", err)
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
