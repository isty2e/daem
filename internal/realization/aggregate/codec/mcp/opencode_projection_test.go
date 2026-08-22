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
	projection.Environment = map[string]string{
		"CHILD_TOKEN": "{env:SOURCE_TOKEN}",
	}

	entry, err := CanonicalOpenCodeGlobalMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("CanonicalOpenCodeGlobalMCPServerEntry returned error: %v", err)
	}
	for _, want := range []string{
		`"type": "local"`,
		`"command": [`,
		`"npx"`,
		`"environment": {`,
		`"CHILD_TOKEN": "{env:SOURCE_TOKEN}"`,
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
	if aggregate.OpenCodeGlobalMCPConfigPath != "~/.config/opencode/opencode.json" {
		t.Fatalf("config path = %q", aggregate.OpenCodeGlobalMCPConfigPath)
	}
	if got := OpenCodeGlobalMCPContentPath("context7"); got != "/mcp/context7" {
		t.Fatalf("content path = %q", got)
	}
}

func TestOpenCodeGlobalMCPProjectionCanonicalEntryOmitsEmptyEnvironment(t *testing.T) {
	entry, err := CanonicalOpenCodeGlobalMCPServerEntry(validOpenCodeGlobalMCPProjection("context7"))
	if err != nil {
		t.Fatalf("CanonicalOpenCodeGlobalMCPServerEntry returned error: %v", err)
	}
	if strings.Contains(string(entry), `"environment"`) {
		t.Fatalf("canonical entry = %s, did not want empty environment", entry)
	}
}

func TestOpenCodeGlobalMCPProjectionTreatsMissingAndEmptyEnvironmentAsEquivalent(t *testing.T) {
	projection := validOpenCodeGlobalMCPProjection("context7")
	for _, existing := range [][]byte{
		[]byte(`{"mcp":{"context7":{"type":"local","command":["npx"]}}}`),
		[]byte(`{"mcp":{"context7":{"type":"local","command":["npx"],"environment":{}}}}`),
	} {
		comparison, err := compareOpenCodeGlobalMCPServerProjection(existing, projection)
		if err != nil {
			t.Fatalf("compareOpenCodeGlobalMCPServerProjection returned error: %v", err)
		}
		if !comparison.Present || !comparison.Equivalent {
			t.Fatalf("comparison = %#v, want present/equivalent", comparison)
		}
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
	projection.Environment = map[string]string{"CHILD_TOKEN": "{env:SOURCE_TOKEN}"}
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
		`"CHILD_TOKEN": "{env:SOURCE_TOKEN}"`,
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

	drifted := []byte(strings.ReplaceAll(string(merged), "{env:SOURCE_TOKEN}", "{env:OTHER_TOKEN}"))
	comparison, err = compareOpenCodeGlobalMCPServerCanonicalEntry(drifted, "context7", canonical)
	if err != nil {
		t.Fatalf("compare drifted OpenCode global entry returned error: %v", err)
	}
	if !comparison.Present || comparison.Equivalent {
		t.Fatalf("drifted comparison = %#v, want present/non-equivalent", comparison)
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
		{name: "duplicate key", input: []byte(`{"mcp":{"context7":{"type":"local","command":["npx"],"command":["node"]}}}`), want: MCPProjectionReasonDuplicateKey},
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
		{name: "literal environment value", input: []byte(`{"mcp":{"context7":{"type":"local","command":["npx"],"environment":{"TOKEN":"SECRET_CANARY"}}}}`), want: MCPProjectionReasonSecretLiteralForbidden},
		{name: "shell environment reference", input: []byte(`{"mcp":{"context7":{"type":"local","command":["npx"],"environment":{"TOKEN":"${TOKEN}"}}}}`), want: MCPProjectionReasonSecretLiteralForbidden},
		{name: "dollar environment reference", input: []byte(`{"mcp":{"context7":{"type":"local","command":["npx"],"environment":{"TOKEN":"$TOKEN"}}}}`), want: MCPProjectionReasonSecretLiteralForbidden},
		{name: "file interpolation", input: []byte(`{"mcp":{"context7":{"type":"local","command":["npx"],"environment":{"TOKEN":"{file:/tmp/token}"}}}}`), want: MCPProjectionReasonSecretLiteralForbidden},
		{name: "compound reference", input: []byte(`{"mcp":{"context7":{"type":"local","command":["npx"],"environment":{"TOKEN":"prefix-{env:TOKEN}"}}}}`), want: MCPProjectionReasonSecretLiteralForbidden},
		{name: "empty source name", input: []byte(`{"mcp":{"context7":{"type":"local","command":["npx"],"environment":{"TOKEN":"{env:}"}}}}`), want: MCPProjectionReasonSecretLiteralForbidden},
		{name: "invalid source name", input: []byte(`{"mcp":{"context7":{"type":"local","command":["npx"],"environment":{"TOKEN":"{env:BAD-NAME}"}}}}`), want: MCPProjectionReasonSecretLiteralForbidden},
		{name: "environment array", input: []byte(`{"mcp":{"context7":{"type":"local","command":["npx"],"environment":[]}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "environment null", input: []byte(`{"mcp":{"context7":{"type":"local","command":["npx"],"environment":null}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "unknown field", input: []byte(`{"mcp":{"context7":{"type":"local","command":["npx"],"headers":{}}}}`), want: MCPProjectionReasonUnsupportedManagedField},
		{name: "duplicate command key", input: []byte(`{"mcp":{"context7":{"type":"local","command":["npx"],"command":["node"]}}}`), want: MCPProjectionReasonDuplicateKey},
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

func TestOpenCodeGlobalMCPProjectionRejectsInvalidDesiredEnvironment(t *testing.T) {
	cases := []struct {
		name        string
		environment map[string]string
	}{
		{name: "invalid child name", environment: map[string]string{"BAD-NAME": "{env:SOURCE_TOKEN}"}},
		{name: "literal value", environment: map[string]string{"CHILD_TOKEN": "SECRET_CANARY"}},
		{name: "shell reference", environment: map[string]string{"CHILD_TOKEN": "${SOURCE_TOKEN}"}},
		{name: "compound reference", environment: map[string]string{"CHILD_TOKEN": "{env:SOURCE_TOKEN}/suffix"}},
		{name: "invalid source name", environment: map[string]string{"CHILD_TOKEN": "{env:BAD-NAME}"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projection := validOpenCodeGlobalMCPProjection("context7")
			projection.Environment = tc.environment
			_, err := CanonicalOpenCodeGlobalMCPServerEntry(projection)
			if err == nil {
				t.Fatal("error = nil, want invalid environment rejection")
			}
			if strings.Contains(err.Error(), "SECRET_CANARY") {
				t.Fatalf("error leaked secret canary: %q", err)
			}
		})
	}
}

func TestOpenCodeGlobalMCPProjectionOwnsArgsAndEnvironment(t *testing.T) {
	projection := validOpenCodeGlobalMCPProjection("context7")
	projection.Args = []string{"-y"}
	projection.Environment = map[string]string{"CHILD_TOKEN": "{env:SOURCE_TOKEN}"}

	entry, err := canonicalOpenCodeGlobalMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("canonicalOpenCodeGlobalMCPServerEntry returned error: %v", err)
	}
	projection.Args[0] = "caller-mutated"
	projection.Environment["CHILD_TOKEN"] = "{env:OTHER_TOKEN}"

	if got := entry.Command; len(got) != 2 || got[1] != "-y" {
		t.Fatalf("entry command after caller mutation = %#v", got)
	}
	if got := entry.Environment["CHILD_TOKEN"]; got != "{env:SOURCE_TOKEN}" {
		t.Fatalf("entry environment after caller mutation = %q", got)
	}
}

func TestExtractOpenCodeGlobalMCPServerProjectionsOwnsEnvironment(t *testing.T) {
	content := []byte(`{"mcp":{"context7":{"type":"local","command":["npx","-y"],"environment":{"CHILD_TOKEN":"{env:SOURCE_TOKEN}"}}}}`)
	first, rejections, err := ExtractOpenCodeGlobalMCPServerProjections(t.Context(), content)
	if err != nil || len(rejections) != 0 || len(first) != 1 {
		t.Fatalf("first extraction = %#v, rejections = %#v, error = %v", first, rejections, err)
	}
	first[0].Args[0] = "caller-mutated"
	first[0].Environment["CHILD_TOKEN"] = "{env:OTHER_TOKEN}"

	second, rejections, err := ExtractOpenCodeGlobalMCPServerProjections(t.Context(), content)
	if err != nil || len(rejections) != 0 || len(second) != 1 {
		t.Fatalf("second extraction = %#v, rejections = %#v, error = %v", second, rejections, err)
	}
	if got := second[0].Args; len(got) != 1 || got[0] != "-y" {
		t.Fatalf("second extraction args = %#v", got)
	}
	if got := second[0].Environment["CHILD_TOKEN"]; got != "{env:SOURCE_TOKEN}" {
		t.Fatalf("second extraction environment = %q", got)
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
