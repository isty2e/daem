package declaration_test

import (
	"fmt"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
)

func TestPiMCPBackendDeclarationAdmission(t *testing.T) {
	for _, test := range []struct {
		target string
		field  string
		want   desiredmcp.Backend
	}{
		{"pi", "", desiredmcp.BackendAdapter},
		{"pi", "backend = \"adapter\"\n", desiredmcp.BackendAdapter},
		{"pi", "backend = \"native\"\n", desiredmcp.BackendNative},
		{"pi", "backend = \"unknown\"\n", ""},
		{"codex", "", desiredmcp.BackendNative},
		{"codex", "backend = \"native\"\n", ""},
		{"claude-code", "backend = \"adapter\"\n", ""},
	} {
		t.Run(test.target+"/"+test.field, func(t *testing.T) {
			content := fmt.Sprintf("version = 1\ntargets = [\"%s\"]\n\n[[mcp_server]]\nname = \"context7\"\n%s transport = \"stdio\"\ncommand = \"node\"\n", test.target, test.field)
			manifest, err := declarationmanifest.Decode([]byte(content))
			if test.want == "" {
				if err == nil {
					t.Fatal("unsupported backend declaration was admitted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := manifest.MCPServers()[0].Bindings()[0].Backend(); got != test.want {
				t.Fatalf("backend = %q, want %q", got, test.want)
			}
		})
	}
}
