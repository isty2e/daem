package mcpcodec

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestOpenCodeProjectMCPProjectionFactsAndCanonicalEntry(t *testing.T) {
	projection := validOpenCodeMCPProjection("context7")
	projection.Args = nil

	entry, err := CanonicalOpenCodeProjectMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("CanonicalOpenCodeProjectMCPServerEntry returned error: %v", err)
	}
	for _, want := range []string{
		`"type": "local"`,
		`"command": [`,
		`"npx"`,
	} {
		if !strings.Contains(string(entry), want) {
			t.Fatalf("canonical entry = %s, want %q", entry, want)
		}
	}
	for _, forbidden := range []string{`"args"`, `"env"`, `"cwd"`, `"enabled"`, `"timeout"`, `"url"`, `"headers"`, `"oauth"`} {
		if strings.Contains(string(entry), forbidden) {
			t.Fatalf("canonical entry = %s, did not want %q", entry, forbidden)
		}
	}
	if aggregate.OpenCodeProjectMCPConfigPath != "opencode.json" {
		t.Fatalf("config path = %q", aggregate.OpenCodeProjectMCPConfigPath)
	}
	if got := OpenCodeProjectMCPContentPath("context7"); got != "/mcp/context7" {
		t.Fatalf("content path = %q", got)
	}
}

func TestOpenCodeGlobalMCPProjectionFactsAndCanonicalEntry(t *testing.T) {
	projection := validOpenCodeGlobalMCPProjection("context7")
	projection.Args = nil

	entry, err := CanonicalOpenCodeGlobalMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("CanonicalOpenCodeGlobalMCPServerEntry returned error: %v", err)
	}
	for _, want := range []string{
		`"type": "local"`,
		`"command": [`,
		`"npx"`,
	} {
		if !strings.Contains(string(entry), want) {
			t.Fatalf("canonical entry = %s, want %q", entry, want)
		}
	}
	for _, forbidden := range []string{`"args"`, `"env"`, `"environment"`, `"cwd"`, `"enabled"`, `"timeout"`, `"url"`, `"headers"`, `"oauth"`} {
		if strings.Contains(string(entry), forbidden) {
			t.Fatalf("canonical entry = %s, did not want %q", entry, forbidden)
		}
	}
	if aggregate.OpenCodeGlobalMCPConfigPath != "~/.config/opencode/opencode.json" {
		t.Fatalf("config path = %q", aggregate.OpenCodeGlobalMCPConfigPath)
	}
	if got := OpenCodeGlobalMCPContentPath("context7"); got != "/mcp/context7" {
		t.Fatalf("content path = %q", got)
	}
}

func TestOpenCodeProjectMCPProjectionCanonicalCommandIncludesArgs(t *testing.T) {
	projection := validOpenCodeMCPProjection("context7")
	projection.Args = []string{"-y", "@upstash/context7-mcp"}

	entry, err := CanonicalOpenCodeProjectMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("CanonicalOpenCodeProjectMCPServerEntry returned error: %v", err)
	}
	for _, want := range []string{
		`"command": [`,
		`"npx"`,
		`"-y"`,
		`"@upstash/context7-mcp"`,
	} {
		if !strings.Contains(string(entry), want) {
			t.Fatalf("canonical entry = %s, want %q", entry, want)
		}
	}
}

func TestOpenCodeProjectMCPProjectionMergeCompareAndPreserveSiblings(t *testing.T) {
	existing := []byte(`{
	  "theme": "system",
	  "mcp": {
	    "sibling": {"type":"remote","url":"https://example.invalid/mcp","headers":{"Authorization":"Bearer secret"}}
	  }
	}`)
	projection := validOpenCodeMCPProjection("context7")
	projection.Args = []string{"-y", "@upstash/context7-mcp"}
	canonical, err := CanonicalOpenCodeProjectMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("CanonicalOpenCodeProjectMCPServerEntry returned error: %v", err)
	}

	merged, err := mergeOpenCodeProjectMCPServerCanonicalEntry(existing, "context7", canonical)
	if err != nil {
		t.Fatalf("mergeOpenCodeProjectMCPServerCanonicalEntry returned error: %v", err)
	}
	for _, want := range []string{
		`"theme": "system"`,
		`"sibling": {`,
		`"url": "https://example.invalid/mcp"`,
		`"context7": {`,
		`"type": "local"`,
		`"command": [`,
	} {
		if !strings.Contains(string(merged), want) {
			t.Fatalf("merged = %s, want %q", merged, want)
		}
	}

	comparison, err := compareOpenCodeProjectMCPServerCanonicalEntry(merged, "context7", canonical)
	if err != nil {
		t.Fatalf("compareOpenCodeProjectMCPServerCanonicalEntry returned error: %v", err)
	}
	if !comparison.Present || !comparison.Equivalent {
		t.Fatalf("comparison = %#v, want present/equivalent", comparison)
	}
	if comparison.ContentPath != "/mcp/context7" {
		t.Fatalf("content path = %q", comparison.ContentPath)
	}
}

func TestOpenCodeGlobalMCPProjectionMergeCompareAndPreserveSiblings(t *testing.T) {
	existing := []byte(`{
	  "theme": "system",
	  "mcp": {
	    "sibling": {"type":"remote","url":"https://example.invalid/mcp","headers":{"Authorization":"Bearer SECRET_CANARY"}}
	  }
	}`)
	projection := validOpenCodeGlobalMCPProjection("context7")
	projection.Args = []string{"-y", "@upstash/context7-mcp"}
	canonical, err := CanonicalOpenCodeGlobalMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("CanonicalOpenCodeGlobalMCPServerEntry returned error: %v", err)
	}

	merged, err := mergeOpenCodeGlobalMCPServerCanonicalEntry(existing, "context7", canonical)
	if err != nil {
		t.Fatalf("mergeOpenCodeGlobalMCPServerCanonicalEntry returned error: %v", err)
	}
	for _, want := range []string{
		`"theme": "system"`,
		`"sibling": {`,
		`"url": "https://example.invalid/mcp"`,
		`"context7": {`,
		`"type": "local"`,
		`"command": [`,
	} {
		if !strings.Contains(string(merged), want) {
			t.Fatalf("merged = %s, want %q", merged, want)
		}
	}

	comparison, err := compareOpenCodeGlobalMCPServerCanonicalEntry(merged, "context7", canonical)
	if err != nil {
		t.Fatalf("compareOpenCodeGlobalMCPServerCanonicalEntry returned error: %v", err)
	}
	if !comparison.Present || !comparison.Equivalent {
		t.Fatalf("comparison = %#v, want present/equivalent", comparison)
	}
	if comparison.ContentPath != "/mcp/context7" {
		t.Fatalf("content path = %q", comparison.ContentPath)
	}
}

func TestOpenCodeProjectMCPProjectionCreatesMCPParentAndRemovesOnlyManagedEntry(t *testing.T) {
	canonical, err := CanonicalOpenCodeProjectMCPServerEntry(validOpenCodeMCPProjection("context7"))
	if err != nil {
		t.Fatalf("CanonicalOpenCodeProjectMCPServerEntry returned error: %v", err)
	}
	merged, err := mergeOpenCodeProjectMCPServerCanonicalEntry([]byte(`{"model":"keep"}`), "context7", canonical)
	if err != nil {
		t.Fatalf("mergeOpenCodeProjectMCPServerCanonicalEntry returned error: %v", err)
	}
	if !containsAll(string(merged), `"model": "keep"`, `"mcp": {`, `"context7": {`) {
		t.Fatalf("merged = %s, want top-level fields plus mcp/context7", merged)
	}

	withSibling := []byte(`{
	  "mcp": {
	    "context7": {"type":"local","command":["npx"]},
	    "sibling": {"type":"remote","url":"https://example.invalid/mcp","headers":{"Authorization":"Bearer SECRET_CANARY"}}
	  },
	  "model": "keep"
	}`)
	removed, err := removeOpenCodeProjectMCPServerProjection(withSibling, "context7")
	if err != nil {
		t.Fatalf("removeOpenCodeProjectMCPServerProjection returned error: %v", err)
	}
	if strings.Contains(string(removed), `"context7":`) {
		t.Fatalf("removed = %s, did not want managed server id", removed)
	}
	if !containsAll(string(removed), `"sibling": {`, `"url": "https://example.invalid/mcp"`, `"model": "keep"`) {
		t.Fatalf("removed = %s, want sibling and top-level fields preserved", removed)
	}
}

func TestOpenCodeProjectMCPProjectionRejectsUnsupportedSameNameShapes(t *testing.T) {
	projection := validOpenCodeMCPProjection("context7")
	cases := []struct {
		name  string
		input []byte
		want  MCPProjectionReasonCode
	}{
		{name: "malformed json", input: []byte(`{"mcp":`), want: MCPProjectionReasonConfigMalformed},
		{name: "mcp non object", input: []byte(`{"mcp":[]}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "managed entry non object", input: []byte(`{"mcp":{"context7":[]}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "missing type", input: []byte(`{"mcp":{"context7":{"command":["npx"]}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "remote type", input: []byte(`{"mcp":{"context7":{"type":"remote","command":["npx"]}}}`), want: MCPProjectionReasonUnsupportedTransport},
		{name: "missing command", input: []byte(`{"mcp":{"context7":{"type":"local"}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "command string", input: []byte(`{"mcp":{"context7":{"type":"local","command":"npx"}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "command empty array", input: []byte(`{"mcp":{"context7":{"type":"local","command":[]}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "command arg non string", input: []byte(`{"mcp":{"context7":{"type":"local","command":["npx",7]}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "command first not canonical", input: []byte(`{"mcp":{"context7":{"type":"local","command":["/usr/bin/../bin/node"]}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "unsupported env field", input: []byte(`{"mcp":{"context7":{"type":"local","command":["npx"],"environment":{"TOKEN":"SECRET_CANARY"}}}}`), want: MCPProjectionReasonUnsupportedManagedField},
		{name: "unsupported cwd field", input: []byte(`{"mcp":{"context7":{"type":"local","command":["npx"],"cwd":"/tmp"}}}`), want: MCPProjectionReasonUnsupportedManagedField},
		{name: "unsupported timeout field", input: []byte(`{"mcp":{"context7":{"type":"local","command":["npx"],"timeout":30}}}`), want: MCPProjectionReasonUnsupportedManagedField},
		{name: "duplicate key", input: []byte(`{"mcp":{"context7":{"type":"local","command":["npx"],"command":["node"]}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compareOpenCodeProjectMCPServerProjection(tc.input, projection)
			assertMCPProjectionReason(t, err, tc.want)
			if err != nil && strings.Contains(err.Error(), "SECRET_CANARY") {
				t.Fatalf("error leaked secret canary: %q", err)
			}
		})
	}
}

func TestOpenCodeGlobalMCPProjectionRejectsUnsupportedSameNameShapes(t *testing.T) {
	projection := validOpenCodeGlobalMCPProjection("context7")
	cases := []struct {
		name  string
		input []byte
		want  MCPProjectionReasonCode
	}{
		{name: "remote type", input: []byte(`{"mcp":{"context7":{"type":"remote","command":["npx"]}}}`), want: MCPProjectionReasonUnsupportedTransport},
		{name: "unsupported environment field", input: []byte(`{"mcp":{"context7":{"type":"local","command":["npx"],"environment":{"TOKEN":"SECRET_CANARY"}}}}`), want: MCPProjectionReasonUnsupportedManagedField},
		{name: "duplicate command key", input: []byte(`{"mcp":{"context7":{"type":"local","command":["npx"],"command":["node"]}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "command string", input: []byte(`{"mcp":{"context7":{"type":"local","command":"npx"}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compareOpenCodeGlobalMCPServerProjection(tc.input, projection)
			assertMCPProjectionReason(t, err, tc.want)
			if err != nil && strings.Contains(err.Error(), "SECRET_CANARY") {
				t.Fatalf("error leaked secret canary: %q", err)
			}
		})
	}
}

func TestOpenCodeProjectMCPProjectionMergeBlocksUnsupportedSameNameEntry(t *testing.T) {
	existing := []byte(`{
	  "mcp": {
	    "context7": {"type":"local","command":["npx"],"environment":{"TOKEN":"SECRET_CANARY"}},
	    "sibling": {"type":"remote","url":"https://example.invalid/mcp","headers":{"Authorization":"Bearer SECRET_CANARY"}}
	  }
	}`)
	canonical, err := CanonicalOpenCodeProjectMCPServerEntry(validOpenCodeMCPProjection("context7"))
	if err != nil {
		t.Fatalf("CanonicalOpenCodeProjectMCPServerEntry returned error: %v", err)
	}

	_, err = mergeOpenCodeProjectMCPServerCanonicalEntry(existing, "context7", canonical)
	assertMCPProjectionReason(t, err, MCPProjectionReasonUnsupportedManagedField)
	if err != nil && strings.Contains(err.Error(), "SECRET_CANARY") {
		t.Fatalf("error leaked secret canary: %q", err)
	}

	merged, err := mergeOpenCodeProjectMCPServerCanonicalEntry(existing, "new-server", canonical)
	if err != nil {
		t.Fatalf("mergeOpenCodeProjectMCPServerCanonicalEntry for new server returned error: %v", err)
	}
	if !containsAll(string(merged), `"context7": {`, `"new-server": {`, `"SECRET_CANARY"`) {
		t.Fatalf("merged = %s, want unsupported sibling preserved when different server is managed", merged)
	}
}

func TestOpenCodeProjectMCPProjectionRejectsInvalidDesiredProjection(t *testing.T) {
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
				AdapterContract: "opencode-project-mcp-local-command-v0",
			},
			want: MCPProjectionReasonStaleAdapterContract,
		},
		{
			name: "absolute command",
			projection: MCPNoEnvServerProjection{
				ServerID:        "context7",
				Command:         "/usr/bin/../bin/node",
				AdapterContract: aggregate.OpenCodeProjectMCPLocalCommandV1,
			},
			want: MCPProjectionReasonProjectionEquivalenceUndefined,
		},
		{
			name: "invalid server id",
			projection: MCPNoEnvServerProjection{
				ServerID:        "bad/server",
				Command:         "npx",
				AdapterContract: aggregate.OpenCodeProjectMCPLocalCommandV1,
			},
			want: MCPProjectionReasonProjectionEquivalenceUndefined,
		},
		{
			name: "shell command",
			projection: MCPNoEnvServerProjection{
				ServerID:        "context7",
				Command:         "npx --yes",
				AdapterContract: aggregate.OpenCodeProjectMCPLocalCommandV1,
			},
			want: MCPProjectionReasonProjectionEquivalenceUndefined,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CanonicalOpenCodeProjectMCPServerEntry(tc.projection)
			assertMCPProjectionReason(t, err, tc.want)
		})
	}
}
