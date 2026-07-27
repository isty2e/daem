package mcpcodec

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestCodexProjectMCPProjectionFactsAndCanonicalEntry(t *testing.T) {
	projection := validCodexMCPProjection("context7")
	projection.Args = nil

	entry, err := CanonicalCodexProjectMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("CanonicalCodexProjectMCPServerEntry returned error: %v", err)
	}
	for _, want := range []string{
		`command = "npx"`,
	} {
		if !strings.Contains(string(entry), want) {
			t.Fatalf("canonical entry = %s, want %q", entry, want)
		}
	}
	for _, forbidden := range []string{`type`, `env`, `cwd`, `url`, `headers`, `oauth`, `tools`} {
		if strings.Contains(string(entry), forbidden) {
			t.Fatalf("canonical entry = %s, did not want %q", entry, forbidden)
		}
	}
	if aggregate.CodexProjectMCPConfigPath != ".codex/config.toml" {
		t.Fatalf("config path = %q", aggregate.CodexProjectMCPConfigPath)
	}
	if got := CodexProjectMCPContentPath("context7"); got != "/mcp_servers/context7" {
		t.Fatalf("content path = %q", got)
	}
}

func TestCodexGlobalMCPProjectionFactsAndCanonicalEntry(t *testing.T) {
	projection := MCPNoEnvServerProjection{
		ServerID:        "context7",
		Command:         "npx",
		AdapterContract: aggregate.CodexGlobalMCPStdioCommandV1,
	}

	entry, err := CanonicalCodexGlobalMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("CanonicalCodexGlobalMCPServerEntry returned error: %v", err)
	}
	if !strings.Contains(string(entry), `command = "npx"`) {
		t.Fatalf("canonical entry = %s, want command", entry)
	}
	for _, forbidden := range []string{`type`, `env`, `cwd`, `url`, `headers`, `oauth`, `tools`} {
		if strings.Contains(string(entry), forbidden) {
			t.Fatalf("canonical entry = %s, did not want %q", entry, forbidden)
		}
	}
	if aggregate.CodexGlobalMCPConfigPath != "~/.codex/config.toml" {
		t.Fatalf("config path = %q", aggregate.CodexGlobalMCPConfigPath)
	}
	if got := CodexGlobalMCPContentPath("context7"); got != "/mcp_servers/context7" {
		t.Fatalf("content path = %q", got)
	}
}

func TestCodexProjectMCPProjectionMergeCompareAndPreserveSiblings(t *testing.T) {
	existing := []byte(`
model = "gpt-5-codex"

[mcp_servers.sibling]
command = "node"
args = ["manual.js"]
`)
	projection := validCodexMCPProjection("context7")
	projection.Args = []string{"-y", "@upstash/context7-mcp"}
	canonical, err := CanonicalCodexProjectMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("CanonicalCodexProjectMCPServerEntry returned error: %v", err)
	}

	merged, err := mergeCodexProjectMCPServerCanonicalEntry(existing, "context7", canonical)
	if err != nil {
		t.Fatalf("mergeCodexProjectMCPServerCanonicalEntry returned error: %v", err)
	}
	for _, want := range []string{
		`model = "gpt-5-codex"`,
		`[mcp_servers.context7]`,
		`command = "npx"`,
		`args = ["-y", "@upstash/context7-mcp"]`,
		`[mcp_servers.sibling]`,
		`command = "node"`,
	} {
		if !strings.Contains(string(merged), want) {
			t.Fatalf("merged = %s, want %q", merged, want)
		}
	}

	comparison, err := compareCodexProjectMCPServerCanonicalEntry(merged, "context7", canonical)
	if err != nil {
		t.Fatalf("compareCodexProjectMCPServerCanonicalEntry returned error: %v", err)
	}
	if !comparison.Present || !comparison.Equivalent {
		t.Fatalf("comparison = %#v, want present/equivalent", comparison)
	}
	if comparison.ContentPath != "/mcp_servers/context7" {
		t.Fatalf("content path = %q", comparison.ContentPath)
	}
}

func TestCodexGlobalMCPProjectionMergeCompareAndPreserveSiblings(t *testing.T) {
	existing := []byte(`
model = "gpt-5-codex"

[mcp_servers.sibling]
command = "node"
args = ["manual.js"]
`)
	projection := MCPNoEnvServerProjection{
		ServerID:        "context7",
		Command:         "npx",
		Args:            []string{"-y", "@upstash/context7-mcp"},
		AdapterContract: aggregate.CodexGlobalMCPStdioCommandV1,
	}
	canonical, err := CanonicalCodexGlobalMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("CanonicalCodexGlobalMCPServerEntry returned error: %v", err)
	}

	merged, err := mergeCodexGlobalMCPServerCanonicalEntry(existing, "context7", canonical)
	if err != nil {
		t.Fatalf("mergeCodexGlobalMCPServerCanonicalEntry returned error: %v", err)
	}
	for _, want := range []string{
		`model = "gpt-5-codex"`,
		`[mcp_servers.context7]`,
		`command = "npx"`,
		`args = ["-y", "@upstash/context7-mcp"]`,
		`[mcp_servers.sibling]`,
		`command = "node"`,
	} {
		if !strings.Contains(string(merged), want) {
			t.Fatalf("merged = %s, want %q", merged, want)
		}
	}

	comparison, err := compareCodexGlobalMCPServerCanonicalEntry(merged, "context7", canonical)
	if err != nil {
		t.Fatalf("compareCodexGlobalMCPServerCanonicalEntry returned error: %v", err)
	}
	if !comparison.Present || !comparison.Equivalent {
		t.Fatalf("comparison = %#v, want present/equivalent", comparison)
	}
	if comparison.ContentPath != "/mcp_servers/context7" {
		t.Fatalf("content path = %q", comparison.ContentPath)
	}
}

func TestCodexGlobalMCPProjectionCanonicalizesAbsentArgs(t *testing.T) {
	existing := []byte(`
[mcp_servers.context7]
command = "npx"
`)
	comparison, err := compareCodexGlobalMCPServerProjection(existing, validCodexGlobalMCPProjection("context7"))
	if err != nil {
		t.Fatalf("compareCodexGlobalMCPServerProjection returned error: %v", err)
	}
	if !comparison.Present || !comparison.Equivalent {
		t.Fatalf("comparison = %#v, want present/equivalent with absent args canonicalized", comparison)
	}
}

func TestCodexGlobalMCPProjectionRemovePreservesSiblingsAndTopLevelFields(t *testing.T) {
	existing := []byte(`
model = "gpt-5-codex"

[mcp_servers.context7]
command = "npx"
args = ["-y", "@upstash/context7-mcp"]

[mcp_servers.sibling]
command = "node"
env = { API_TOKEN = "SECRET_CANARY" }
`)

	removed, err := removeCodexGlobalMCPServerProjection(existing, "context7")
	if err != nil {
		t.Fatalf("removeCodexGlobalMCPServerProjection returned error: %v", err)
	}
	if strings.Contains(string(removed), `[mcp_servers.context7]`) {
		t.Fatalf("removed = %s, did not want managed server id", removed)
	}
	if !containsAll(string(removed), `model = "gpt-5-codex"`, `[mcp_servers.sibling]`, `SECRET_CANARY`) {
		t.Fatalf("removed = %s, want top-level and unsupported sibling values preserved", removed)
	}
	comparison, err := compareCodexGlobalMCPServerProjection(removed, validCodexGlobalMCPProjection("context7"))
	if err != nil {
		t.Fatalf("compareCodexGlobalMCPServerProjection returned error: %v", err)
	}
	if comparison.Present {
		t.Fatalf("comparison = %#v, want absent managed entry", comparison)
	}
}

func TestCodexGlobalMCPProjectionRejectsUnsupportedSameNameShapes(t *testing.T) {
	projection := validCodexGlobalMCPProjection("context7")
	cases := []struct {
		name  string
		input []byte
		want  MCPProjectionReasonCode
	}{
		{name: "malformed toml", input: []byte(`[mcp_servers.context7`), want: MCPProjectionReasonConfigMalformed},
		{name: "empty config", input: []byte(`   `), want: MCPProjectionReasonConfigMalformed},
		{name: "mcp_servers non table", input: []byte(`mcp_servers = []`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "managed entry non table", input: []byte(`[mcp_servers]
context7 = []`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "missing command", input: []byte(`[mcp_servers.context7]
args = []`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "command not string", input: []byte(`[mcp_servers.context7]
command = ["npx"]`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "args not array", input: []byte(`[mcp_servers.context7]
command = "npx"
args = "-y"`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "arg non string", input: []byte(`[mcp_servers.context7]
command = "npx"
args = ["-y", 7]`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "unsupported env field", input: []byte(`[mcp_servers.context7]
command = "npx"
env = { API_TOKEN = "SECRET_CANARY" }`), want: MCPProjectionReasonUnsupportedManagedField},
		{name: "unsupported remote field", input: []byte(`[mcp_servers.context7]
command = "npx"
url = "https://example.invalid/mcp"`), want: MCPProjectionReasonUnsupportedManagedField},
		{name: "shell command existing", input: []byte(`[mcp_servers.context7]
command = "npx --yes"`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "duplicate dotted and table key", input: []byte(`mcp_servers.context7.command = "npx"
[mcp_servers.context7]
command = "node"
args = []`), want: MCPProjectionReasonConfigMalformed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compareCodexGlobalMCPServerProjection(tc.input, projection)
			assertMCPProjectionReason(t, err, tc.want)
			if err != nil && strings.Contains(err.Error(), "SECRET_CANARY") {
				t.Fatalf("error leaked secret canary: %q", err)
			}
		})
	}
}

func TestCodexGlobalMCPProjectionRejectsInvalidDesiredProjection(t *testing.T) {
	cases := []struct {
		name       string
		projection MCPNoEnvServerProjection
		want       MCPProjectionReasonCode
	}{
		{
			name: "stale adapter",
			projection: MCPNoEnvServerProjection{
				ServerID:        "context7",
				Command:         "npx",
				AdapterContract: "codex-global-mcp-stdio-command-v0",
			},
			want: MCPProjectionReasonStaleAdapterContract,
		},
		{
			name: "absolute command",
			projection: MCPNoEnvServerProjection{
				ServerID:        "context7",
				Command:         "/usr/bin/node",
				AdapterContract: aggregate.CodexGlobalMCPStdioCommandV1,
			},
			want: MCPProjectionReasonProjectionEquivalenceUndefined,
		},
		{
			name: "invalid server id",
			projection: MCPNoEnvServerProjection{
				ServerID:        "bad/server",
				Command:         "npx",
				AdapterContract: aggregate.CodexGlobalMCPStdioCommandV1,
			},
			want: MCPProjectionReasonProjectionEquivalenceUndefined,
		},
		{
			name: "shell command",
			projection: MCPNoEnvServerProjection{
				ServerID:        "context7",
				Command:         "npx --yes",
				AdapterContract: aggregate.CodexGlobalMCPStdioCommandV1,
			},
			want: MCPProjectionReasonProjectionEquivalenceUndefined,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CanonicalCodexGlobalMCPServerEntry(tc.projection)
			assertMCPProjectionReason(t, err, tc.want)
		})
	}
}

func TestCodexGlobalMCPProjectionMergeBlocksUnsupportedSameNameEntry(t *testing.T) {
	existing := []byte(`
[mcp_servers.context7]
command = "npx"
env = { API_TOKEN = "SECRET_CANARY" }

[mcp_servers.sibling]
command = "node"
env = { API_TOKEN = "SECRET_CANARY" }
`)
	canonical, err := CanonicalCodexGlobalMCPServerEntry(validCodexGlobalMCPProjection("context7"))
	if err != nil {
		t.Fatalf("CanonicalCodexGlobalMCPServerEntry returned error: %v", err)
	}

	_, err = mergeCodexGlobalMCPServerCanonicalEntry(existing, "context7", canonical)
	assertMCPProjectionReason(t, err, MCPProjectionReasonUnsupportedManagedField)
	if err != nil && strings.Contains(err.Error(), "SECRET_CANARY") {
		t.Fatalf("error leaked secret canary: %q", err)
	}

	merged, err := mergeCodexGlobalMCPServerCanonicalEntry(existing, "new-server", canonical)
	if err != nil {
		t.Fatalf("mergeCodexGlobalMCPServerCanonicalEntry for new server returned error: %v", err)
	}
	if !containsAll(string(merged), `[mcp_servers.context7]`, `[mcp_servers.new-server]`, `SECRET_CANARY`) {
		t.Fatalf("merged = %s, want unsupported siblings preserved when unmanaged", merged)
	}
}

func TestCodexGlobalMCPProjectionExtractsSupportedRowsAndReportsRejections(t *testing.T) {
	existing := []byte(`
[mcp_servers.context7]
command = "npx"
args = ["-y", "@upstash/context7-mcp"]

[mcp_servers.remote]
command = "npx"
headers = { Authorization = "Bearer SECRET_CANARY" }
`)

	projections, rejections, err := ExtractCodexGlobalMCPServerProjections(existing)
	if err != nil {
		t.Fatalf("ExtractCodexGlobalMCPServerProjections returned error: %v", err)
	}
	if len(projections) != 1 ||
		projections[0].ServerID != "context7" ||
		projections[0].Command != "npx" ||
		projections[0].AdapterContract != aggregate.CodexGlobalMCPStdioCommandV1 {
		t.Fatalf("projections = %#v, want one context7 Codex global projection", projections)
	}
	if len(rejections) != 1 ||
		rejections[0].ContentPath != "/mcp_servers/remote" ||
		rejections[0].Reason != MCPProjectionReasonUnsupportedManagedField {
		t.Fatalf("rejections = %#v, want remote unsupported managed field", rejections)
	}
}

func TestCodexProjectMCPProjectionCanonicalizesAbsentArgs(t *testing.T) {
	existing := []byte(`
[mcp_servers.context7]
command = "npx"
`)
	comparison, err := compareCodexProjectMCPServerProjection(existing, validCodexMCPProjection("context7"))
	if err != nil {
		t.Fatalf("compareCodexProjectMCPServerProjection returned error: %v", err)
	}
	if !comparison.Present || !comparison.Equivalent {
		t.Fatalf("comparison = %#v, want present/equivalent with absent args canonicalized", comparison)
	}
}

func TestCodexProjectMCPProjectionDecodesInternalStringArgsMap(t *testing.T) {
	entry, err := decodeCodexProjectMCPServerEntryValue(
		map[string]any{"command": "npx", "args": []string{"-y", "@upstash/context7-mcp"}},
		"context7",
	)
	if err != nil {
		t.Fatalf("decodeCodexProjectMCPServerEntryValue returned error: %v", err)
	}
	if len(entry.Args) != 2 || entry.Args[0] != "-y" || entry.Args[1] != "@upstash/context7-mcp" {
		t.Fatalf("entry args = %#v", entry.Args)
	}
}

func TestCodexProjectMCPProjectionRemovePreservesSiblingsAndTopLevelFields(t *testing.T) {
	existing := []byte(`
model = "gpt-5-codex"

[mcp_servers.context7]
command = "npx"
args = ["-y", "@upstash/context7-mcp"]

[mcp_servers.sibling]
command = "node"
args = ["manual.js"]
`)

	removed, err := removeCodexProjectMCPServerProjection(existing, "context7")
	if err != nil {
		t.Fatalf("removeCodexProjectMCPServerProjection returned error: %v", err)
	}
	if strings.Contains(string(removed), `[mcp_servers.context7]`) {
		t.Fatalf("removed = %s, did not want managed server id", removed)
	}
	if !containsAll(string(removed), `model = "gpt-5-codex"`, `[mcp_servers.sibling]`, `command = "node"`) {
		t.Fatalf("removed = %s, want top-level and sibling values preserved", removed)
	}
}

func TestCodexProjectMCPProjectionRejectsUnsupportedSameNameShapes(t *testing.T) {
	projection := validCodexMCPProjection("context7")
	cases := []struct {
		name  string
		input []byte
		want  MCPProjectionReasonCode
	}{
		{name: "malformed toml", input: []byte(`[mcp_servers.context7`), want: MCPProjectionReasonConfigMalformed},
		{name: "empty config", input: []byte(`   `), want: MCPProjectionReasonConfigMalformed},
		{name: "mcp_servers non table", input: []byte(`mcp_servers = []`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "managed entry non table", input: []byte(`[mcp_servers]
context7 = []`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "missing command", input: []byte(`[mcp_servers.context7]
args = []`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "command not string", input: []byte(`[mcp_servers.context7]
command = ["npx"]`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "args not array", input: []byte(`[mcp_servers.context7]
command = "npx"
args = "-y"`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "arg non string", input: []byte(`[mcp_servers.context7]
command = "npx"
args = ["-y", 7]`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "unsupported env field", input: []byte(`[mcp_servers.context7]
command = "npx"
env = { API_TOKEN = "SECRET_CANARY" }`), want: MCPProjectionReasonUnsupportedManagedField},
		{name: "unsupported cwd field", input: []byte(`[mcp_servers.context7]
command = "npx"
cwd = "/tmp"`), want: MCPProjectionReasonUnsupportedManagedField},
		{name: "shell command existing", input: []byte(`[mcp_servers.context7]
command = "npx --yes"`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "duplicate command key", input: []byte(`[mcp_servers.context7]
command = "npx"
command = "node"`), want: MCPProjectionReasonConfigMalformed},
		{name: "duplicate dotted and table key", input: []byte(`mcp_servers.context7.command = "npx"
[mcp_servers.context7]
command = "node"
args = []`), want: MCPProjectionReasonConfigMalformed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compareCodexProjectMCPServerProjection(tc.input, projection)
			assertMCPProjectionReason(t, err, tc.want)
			if err != nil && strings.Contains(err.Error(), "SECRET_CANARY") {
				t.Fatalf("error leaked secret canary: %q", err)
			}
		})
	}
}

func TestCodexProjectMCPProjectionMergeBlocksUnsupportedSameNameEntry(t *testing.T) {
	existing := []byte(`
[mcp_servers.context7]
command = "npx"
env = { API_TOKEN = "SECRET_CANARY" }

[mcp_servers.sibling]
command = "node"
env = { API_TOKEN = "SECRET_CANARY" }
`)
	canonical, err := CanonicalCodexProjectMCPServerEntry(validCodexMCPProjection("context7"))
	if err != nil {
		t.Fatalf("CanonicalCodexProjectMCPServerEntry returned error: %v", err)
	}

	_, err = mergeCodexProjectMCPServerCanonicalEntry(existing, "context7", canonical)
	assertMCPProjectionReason(t, err, MCPProjectionReasonUnsupportedManagedField)
	if err != nil && strings.Contains(err.Error(), "SECRET_CANARY") {
		t.Fatalf("error leaked secret canary: %q", err)
	}

	merged, err := mergeCodexProjectMCPServerCanonicalEntry(existing, "new-server", canonical)
	if err != nil {
		t.Fatalf("mergeCodexProjectMCPServerCanonicalEntry for new server returned error: %v", err)
	}
	if !containsAll(string(merged), `[mcp_servers.context7]`, `[mcp_servers.new-server]`, `SECRET_CANARY`) {
		t.Fatalf("merged = %s, want unsupported siblings preserved when unmanaged", merged)
	}
}

func TestCodexProjectMCPProjectionExtractsSupportedRowsAndReportsRejections(t *testing.T) {
	existing := []byte(`
[mcp_servers.context7]
command = "npx"
args = ["-y", "@upstash/context7-mcp"]

[mcp_servers.remote]
command = "npx"
headers = { Authorization = "Bearer SECRET_CANARY" }
`)

	projections, rejections, err := ExtractCodexProjectMCPServerProjections(existing)
	if err != nil {
		t.Fatalf("ExtractCodexProjectMCPServerProjections returned error: %v", err)
	}
	if len(projections) != 1 ||
		projections[0].ServerID != "context7" ||
		projections[0].Command != "npx" ||
		projections[0].AdapterContract != aggregate.CodexProjectMCPStdioCommandV1 {
		t.Fatalf("projections = %#v, want one context7 Codex projection", projections)
	}
	if len(rejections) != 1 ||
		rejections[0].ContentPath != "/mcp_servers/remote" ||
		rejections[0].Reason != MCPProjectionReasonUnsupportedManagedField {
		t.Fatalf("rejections = %#v, want remote unsupported managed field", rejections)
	}
}
