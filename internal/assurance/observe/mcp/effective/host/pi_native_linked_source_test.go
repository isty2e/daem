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

func TestPiNativeLinkedLocalSourceRetainsBothMetadataContexts(t *testing.T) {
	for _, test := range []struct {
		name, link, configuredMetadata, targetMetadata string
		accepted                                       bool
	}{
		{"target Adapter", "file", `{}`, `{"name":"pi-mcp-adapter"}`, false},
		{"configured Adapter", "file", `{"name":"pi-mcp-adapter"}`, `{"name":"unrelated"}`, false},
		{"malformed target", "file", `{}`, `{"name":`, false},
		{"malformed configured", "file", `{"name":`, `{"name":"unrelated"}`, false},
		{"relative target", "relative", `{}`, `{"name":"pi-mcp-adapter"}`, false},
		{"chained file", "chain", `{}`, `{"name":"pi-mcp-adapter"}`, false},
		{"directory link", "directory", `{}`, `{"name":"pi-mcp-adapter"}`, false},
		{"unrelated names", "file", `{"name":"unrelated"}`, `{"name":"another"}`, true},
		{"absent metadata", "file", "", "", true},
		{"name-free target", "file", `{}`, `{}`, true},
		{"dangling file", "missing", `{}`, `{"name":"pi-mcp-adapter"}`, true},
	} {
		for _, field := range []string{"packages", "extensions"} {
			for _, settingsScope := range []target.Scope{target.ScopeGlobal, target.ScopeProject} {
				for _, bindingScope := range []target.Scope{target.ScopeGlobal, target.ScopeProject} {
					t.Run(test.name+"/"+field+"/"+string(settingsScope)+"/"+string(bindingScope), func(t *testing.T) {
						workDir, agentRoot, targetRoot := t.TempDir(), t.TempDir(), t.TempDir()
						base := agentRoot
						if settingsScope == target.ScopeProject {
							base = filepath.Join(workDir, ".pi")
						}
						configuredRoot := filepath.Join(base, "links")
						if err := os.MkdirAll(configuredRoot, 0o755); err != nil {
							t.Fatal(err)
						}
						if test.configuredMetadata != "" {
							writeEffectiveConfig(t, filepath.Join(configuredRoot, "package.json"), test.configuredMetadata)
						}
						if test.targetMetadata != "" {
							writeEffectiveConfig(t, filepath.Join(targetRoot, "package.json"), test.targetMetadata)
						}
						entry := filepath.Join(targetRoot, "index.ts")
						if test.link != "missing" {
							writeEffectiveConfig(t, entry, "export {};\n")
						}
						link := filepath.Join(configuredRoot, "link.ts")
						switch test.link {
						case "directory":
							entry = targetRoot
						case "relative":
							var err error
							entry, err = filepath.Rel(configuredRoot, entry)
							if err != nil {
								t.Fatal(err)
							}
						case "chain":
							hop := filepath.Join(configuredRoot, "hop.ts")
							if err := os.Symlink(entry, hop); err != nil {
								t.Skipf("file symlink unavailable: %v", err)
							}
							entry = hop
						}
						if err := os.Symlink(entry, link); err != nil {
							t.Skipf("symlink unavailable: %v", err)
						}
						wire, err := json.Marshal(map[string][]string{field: {link}})
						if err != nil {
							t.Fatal(err)
						}
						writeEffectiveConfig(t, filepath.Join(base, "settings.json"), string(wire))

						contract, _ := profile.PiMCPContractForCodec(aggregate.MCPCodecPiNativeStdio)
						err = qualifyPiNativeSettings(t.Context(), contract, bindingScope, workDir, agentRoot, profile.ObservePiMCPVersion("1.0.2"))
						if (err == nil) != test.accepted {
							t.Fatalf("linked local source qualification = %v, accepted=%t", err, test.accepted)
						}
					})
				}
			}
		}
	}
}

func TestPiNativePackageLocalSpellingsDoNotBecomeGitCarriers(t *testing.T) {
	for _, source := range []string{
		"builtin:fixture/index.ts", "http:fixture/index.ts", "https:fixture/index.ts", "ssh:fixture/index.ts",
		"github:nicobailon/pi-mcp-adapter", "git+https://github.com/nicobailon/pi-mcp-adapter.git",
		"http://localhost/adapter",
	} {
		t.Run(source, func(t *testing.T) {
			if filepath.Separator == '\\' {
				t.Skip("colon is not a native Windows filename")
			}
			workDir, agentRoot := t.TempDir(), t.TempDir()
			local := filepath.Join(agentRoot, source)
			writeEffectiveConfig(t, local, "export {};\n")
			writeEffectiveConfig(t, filepath.Join(filepath.Dir(local), "package.json"), `{"name":"pi-mcp-adapter"}`)
			wire, err := json.Marshal(map[string][]string{"packages": {source}})
			if err != nil {
				t.Fatal(err)
			}
			writeEffectiveConfig(t, filepath.Join(agentRoot, "settings.json"), string(wire))

			contract, _ := profile.PiMCPContractForCodec(aggregate.MCPCodecPiNativeStdio)
			if err := qualifyPiNativeSettings(t.Context(), contract, target.ScopeGlobal, workDir, agentRoot, profile.ObservePiMCPVersion("1.0.2")); err == nil {
				t.Fatal("host-local package spelling bypassed Adapter metadata")
			}
		})
	}
}
