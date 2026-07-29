package authoring

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
	daempaths "github.com/isty2e/daem/internal/paths"
)

func TestExtensionAuthoringRejectsCanonicalTextHazardsBeforeRendering(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		target  string
		scope   string
		wantErr string
	}{
		{name: "marketplace bidi", source: "plugin@safe\u202etxt", target: "claude-code", wantErr: "control characters"},
		{name: "host source bidi", source: "safe\u202etxt", target: "pi", wantErr: "control characters"},
		{name: "host source c1 control", source: "safe\u0085txt", target: "opencode", wantErr: "control characters"},
		{name: "host source invalid utf8", source: string([]byte{'b', 'a', 'd', 0xff}), target: "pi", wantErr: "valid UTF-8"},
		{name: "global host source option", source: "-plugin", target: "antigravity-cli", scope: "global", wantErr: "must not begin with '-'"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ExtensionFromAddRequest(
				AddExtensionRequest{
					ID:      "extension-id",
					Source:  test.source,
					Targets: []string{test.target},
					Scope:   test.scope,
				},
				declaration.ManifestHeader{},
				daempaths.ManifestOriginExplicit,
			)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestMCPAuthoringInferenceDeduplicatesAndRejectsAmbiguousManifestTargets(t *testing.T) {
	server, err := MCPServerFromAddRequest(
		AddMCPServerRequest{Name: "context7", Command: "npx"},
		declaration.ManifestHeader{Targets: []string{"codex", "codex"}},
		daempaths.ManifestOriginExplicit,
	)
	if err != nil {
		t.Fatalf("MCPServerFromAddRequest returned error: %v", err)
	}
	if len(server.Targets) != 1 || server.Targets[0] != "codex" || server.Scope != "project" {
		t.Fatalf("server = %#v, want deduplicated inferred Codex project row", server)
	}

	_, err = MCPServerFromAddRequest(
		AddMCPServerRequest{Name: "context7", Command: "npx"},
		declaration.ManifestHeader{Targets: []string{"codex", "claude-code", "codex"}},
		daempaths.ManifestOriginExplicit,
	)
	const want = "mcp-server target is ambiguous across manifest targets codex, claude-code; pass one --target"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestMCPAuthoringRejectsUnsafeArgumentsDuringFullCandidateDecode(t *testing.T) {
	tests := []struct {
		name     string
		argument string
		wantErr  string
	}{
		{name: "invalid utf8", argument: string([]byte{'b', 'a', 'd', 0xff}), wantErr: "valid UTF-8"},
		{name: "bidi control", argument: "safe\u202etxt", wantErr: "control character"},
		{name: "embedded newline", argument: "line-one\nline-two", wantErr: "control character"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := BuildAddMCPServerChange(
				ManifestDocument{Content: []byte("version = 1\ntargets = [\"codex\"]\n")},
				AddMCPServerRequest{
					Name:    "context7",
					Command: "npx",
					Args:    []string{test.argument},
					Targets: []string{"codex"},
				},
			)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}
