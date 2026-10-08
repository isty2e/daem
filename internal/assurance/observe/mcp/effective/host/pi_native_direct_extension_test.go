package host

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

func TestPiNativeDirectExtensionQualificationUsesLocalMetadata(t *testing.T) {
	for _, test := range []struct {
		name, spelling, metadata string
		accepted                 bool
	}{
		{"absolute file", "file", `{"name":"pi-mcp-adapter"}`, false},
		{"absolute directory", "directory", `{"name":"pi-mcp-adapter"}`, false},
		{"excluded explicit file", "excluded", `{"name":"pi-mcp-adapter"}`, false},
		{"relative file", "relative", `{"name":"pi-mcp-adapter"}`, false},
		{"padded relative file", "padded", `{"name":"pi-mcp-adapter"}`, false},
		{"local file URL", "url", `{"name":"pi-mcp-adapter"}`, false},
		{"tilde file", "tilde", `{"name":"pi-mcp-adapter"}`, false},
		{"whitespace settings base", "base", `{"name":"pi-mcp-adapter"}`, false},
		{"literal npm-looking path", "npm", `{"name":"pi-mcp-adapter"}`, false},
		{"padded literal plus path", "plus", `{"name":"pi-mcp-adapter"}`, false},
		{"malformed metadata", "file", `{"name":`, false},
		{"ambiguous metadata", "file", `{"name":"pi-mcp-adapter","name":"unrelated"}`, false},
		{"non-regular metadata", "bad metadata", "", false},
		{"unrelated package", "file", `{"name":"unrelated-extension"}`, true},
		{"basename alone", "basename", `{"name":"unrelated-extension"}`, true},
		{"name-free package", "file", `{}`, true},
		{"absent metadata", "file", "", true},
		{"absent file", "missing", `{"name":"pi-mcp-adapter"}`, true},
	} {
		for _, settingsScope := range []target.Scope{target.ScopeGlobal, target.ScopeProject} {
			for _, bindingScope := range []target.Scope{target.ScopeGlobal, target.ScopeProject} {
				t.Run(test.name+"/"+string(settingsScope)+"/"+string(bindingScope), func(t *testing.T) {
					if test.spelling == "npm" && filepath.Separator == '\\' {
						t.Skip("colon is not a native Windows filename")
					}
					workDir, agentRoot, home := t.TempDir(), t.TempDir(), t.TempDir()
					t.Setenv("HOME", home)
					t.Setenv("USERPROFILE", home)
					base := agentRoot
					if settingsScope == target.ScopeProject {
						base = filepath.Join(workDir, ".pi")
					}
					packageRoot := filepath.Join(base, "renamed checkout")
					switch test.spelling {
					case "tilde":
						packageRoot = filepath.Join(home, "renamed checkout")
					case "base":
						packageRoot = base
					case "npm":
						packageRoot = filepath.Join(base, "npm:fixture")
					case "plus":
						packageRoot = filepath.Join(base, "+literal")
					case "basename":
						packageRoot = filepath.Join(base, "pi-mcp-adapter")
					}
					if test.metadata != "" {
						writeEffectiveConfig(t, filepath.Join(packageRoot, "package.json"), test.metadata)
					}
					if test.spelling == "bad metadata" {
						if err := os.MkdirAll(filepath.Join(packageRoot, "package.json"), 0o755); err != nil {
							t.Fatal(err)
						}
					}
					if test.spelling != "missing" {
						writeEffectiveConfig(t, filepath.Join(packageRoot, "index.ts"), "export {};\n")
					}

					entry := filepath.Join(packageRoot, "index.ts")
					switch test.spelling {
					case "directory":
						entry = packageRoot
					case "relative":
						entry = "./renamed checkout/index.ts"
					case "padded":
						entry = " ./renamed checkout/index.ts "
					case "url":
						urlPath := filepath.ToSlash(entry)
						if filepath.VolumeName(entry) != "" {
							urlPath = "/" + urlPath
						}
						entry = (&url.URL{Scheme: "file", Path: urlPath}).String()
					case "tilde":
						entry = "~/renamed checkout/index.ts"
					case "base":
						entry = " \t\n "
					case "npm":
						entry = "npm:fixture/index.ts"
					case "plus":
						entry = " +literal/index.ts "
					}
					entries := []string{entry}
					if test.spelling == "excluded" {
						entries = append(entries, "-"+entry)
					}
					wire, err := json.Marshal(struct {
						Extensions []string `json:"extensions"`
					}{entries})
					if err != nil {
						t.Fatal(err)
					}
					writeEffectiveConfig(t, filepath.Join(base, "settings.json"), string(wire))

					contract, _ := profile.PiMCPContractForCodec(aggregate.MCPCodecPiNativeStdio)
					err = qualifyPiNativeSettings(t.Context(), contract, bindingScope, workDir, agentRoot, profile.ObservePiMCPVersion("1.0.2"))
					if (err == nil) != test.accepted {
						t.Fatalf("direct extension qualification = %v, accepted=%t", err, test.accepted)
					}
				})
			}
		}
	}
}

func TestPiNativeTopLevelPatternsDoNotDiscoverLocalMetadata(t *testing.T) {
	for _, entry := range []string{
		"+renamed/index.ts", "-renamed/index.ts", "!renamed/index.ts",
		"renamed/*.ts", "renamed/?.ts", "+builtin:mcp", "-builtin:mcp",
	} {
		t.Run(entry, func(t *testing.T) {
			base := t.TempDir()
			writeEffectiveConfig(t, filepath.Join(base, "renamed/package.json"), `{"name":`)
			writeEffectiveConfig(t, filepath.Join(base, "renamed/index.ts"), "export {};\n")
			wire, err := json.Marshal(struct {
				Extensions []string `json:"extensions"`
			}{[]string{entry}})
			if err != nil {
				t.Fatal(err)
			}
			writeEffectiveConfig(t, filepath.Join(base, "settings.json"), string(wire))

			if _, err := observePiNativeSettings(t.Context(), filepath.Join(base, "settings.json"), target.ScopeGlobal); err != nil {
				t.Fatalf("pattern-only declaration inspected unrelated local metadata: %v", err)
			}
		})
	}
}
