package mcpcodec

import (
	"fmt"
	"strings"
	"testing"
)

type mcpJSONAggregateCase struct {
	name             string
	parentKey        string
	canonical        func(string) ([]byte, error)
	merge            func([]byte, string, []byte) ([]byte, error)
	remove           func([]byte, string) ([]byte, error)
	restoreRemove    func([]byte, string, bool) ([]byte, bool, error)
	extractBytes     func([]byte, string) ([]byte, bool, error)
	entryPresent     func([]byte, string) (bool, error)
	parentPresent    func([]byte) (bool, error)
	unsupportedEntry string
}

func TestMCPJSONAggregateAlgorithmsPreserveSiblingsAndRestoreParentSemantics(t *testing.T) {
	for _, tc := range mcpJSONAggregateCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			canonical := mustMCPJSONCanonicalEntry(t, tc, "context7")
			existing := fmt.Appendf(nil, `{
  "comment": {"keep": true},
  "%s": {
    "context7": %s,
    "sibling": {"remote": true}
  }
}`, tc.parentKey, canonical)

			removed, err := tc.remove(existing, "context7")
			if err != nil {
				t.Fatalf("remove returned error: %v", err)
			}
			if strings.Contains(string(removed), `"context7":`) {
				t.Fatalf("removed = %s, did not want managed server id", removed)
			}
			if !containsAll(string(removed), `"comment": {`, `"keep": true`, `"sibling": {`, `"remote": true`) {
				t.Fatalf("removed = %s, want top-level and sibling values preserved", removed)
			}
			if present, err := tc.entryPresent(removed, "sibling"); err != nil || !present {
				t.Fatalf("entryPresent(sibling) = %t, %v; want true, nil", present, err)
			}
			if present, err := tc.parentPresent(removed); err != nil || !present {
				t.Fatalf("parentPresent(removed) = %t, %v; want true, nil", present, err)
			}

			onlyManaged := fmt.Appendf(nil, `{"%s":{"context7":%s}}`, tc.parentKey, canonical)
			restored, keepFile, err := tc.restoreRemove(onlyManaged, "context7", false)
			if err != nil {
				t.Fatalf("restoreRemove(parent absent before) returned error: %v", err)
			}
			if keepFile || restored != nil {
				t.Fatalf("restored = %s, keepFile = %t; want removed file", restored, keepFile)
			}

			restored, keepFile, err = tc.restoreRemove(onlyManaged, "context7", true)
			if err != nil {
				t.Fatalf("restoreRemove(parent present before) returned error: %v", err)
			}
			if !keepFile || !containsAll(string(restored), fmt.Sprintf(`"%s": {}`, tc.parentKey)) {
				t.Fatalf("restored = %s, keepFile = %t; want retained empty parent", restored, keepFile)
			}
		})
	}
}

func TestMCPJSONAggregateAlgorithmsExtractCanonicalBytesAfterMerge(t *testing.T) {
	for _, tc := range mcpJSONAggregateCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			canonical := mustMCPJSONCanonicalEntry(t, tc, "context7")
			merged, err := tc.merge([]byte(`{"comment":"keep"}`), "context7", canonical)
			if err != nil {
				t.Fatalf("merge returned error: %v", err)
			}
			if !containsAll(string(merged), `"comment": "keep"`, fmt.Sprintf(`"%s": {`, tc.parentKey), `"context7": {`) {
				t.Fatalf("merged = %s, want top-level field plus managed server", merged)
			}

			extracted, present, err := tc.extractBytes(merged, "context7")
			if err != nil {
				t.Fatalf("extractBytes returned error: %v", err)
			}
			if !present {
				t.Fatalf("extractBytes present = false, want true")
			}
			if string(extracted) != string(canonical) {
				t.Fatalf("extracted = %s, want canonical %s", extracted, canonical)
			}
		})
	}
}

func TestMCPJSONAggregateAlgorithmsBlockUnsupportedSameNameEntry(t *testing.T) {
	for _, tc := range mcpJSONAggregateCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			canonical := mustMCPJSONCanonicalEntry(t, tc, "context7")
			existing := fmt.Appendf(nil, `{
  "%s": {
    "context7": %s,
    "sibling": {"remote": true}
  }
}`, tc.parentKey, tc.unsupportedEntry)

			_, err := tc.merge(existing, "context7", canonical)
			assertMCPProjectionReason(t, err, MCPProjectionReasonUnsupportedManagedField)

			_, err = tc.remove(existing, "context7")
			assertMCPProjectionReason(t, err, MCPProjectionReasonUnsupportedManagedField)

			merged, err := tc.merge(existing, "new-server", mustMCPJSONCanonicalEntry(t, tc, "new-server"))
			if err != nil {
				t.Fatalf("merge different server returned error: %v", err)
			}
			if !containsAll(string(merged), `"context7": {`, `"sibling": {`, `"new-server": {`) {
				t.Fatalf("merged = %s, want unmanaged unsupported and sibling entries preserved", merged)
			}
		})
	}
}

func TestMCPJSONAggregateAlgorithmsRejectMalformedAndDuplicateAggregateKeys(t *testing.T) {
	for _, tc := range mcpJSONAggregateCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			canonical := mustMCPJSONCanonicalEntry(t, tc, "context7")
			cases := []struct {
				name  string
				input []byte
				want  MCPProjectionReasonCode
			}{
				{name: "empty config", input: []byte("  "), want: MCPProjectionReasonConfigMalformed},
				{name: "parent non object", input: fmt.Appendf(nil, `{"%s":[]}`, tc.parentKey), want: MCPProjectionReasonProjectionEquivalenceUndefined},
				{name: "duplicate parent key", input: fmt.Appendf(nil, `{"%s":{},"%s":{}}`, tc.parentKey, tc.parentKey), want: MCPProjectionReasonProjectionEquivalenceUndefined},
				{name: "duplicate server key", input: fmt.Appendf(nil, `{"%s":{"context7":%s,"context7":%s}}`, tc.parentKey, canonical, canonical), want: MCPProjectionReasonProjectionEquivalenceUndefined},
			}
			for _, malformed := range cases {
				t.Run(malformed.name, func(t *testing.T) {
					_, err := tc.parentPresent(malformed.input)
					assertMCPProjectionReason(t, err, malformed.want)
				})
			}
		})
	}
}

func TestMCPJSONAggregateAlgorithmsRejectInvalidServerIDBeforeParsingHostConfig(t *testing.T) {
	for _, tc := range mcpJSONAggregateCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			malformedHostConfig := fmt.Appendf(nil, `{"%s":`, tc.parentKey)
			canonical := mustMCPJSONCanonicalEntry(t, tc, "context7")

			_, err := tc.merge(malformedHostConfig, "bad/server", canonical)
			assertMCPProjectionReason(t, err, MCPProjectionReasonProjectionEquivalenceUndefined)

			_, err = tc.remove(malformedHostConfig, "bad/server")
			assertMCPProjectionReason(t, err, MCPProjectionReasonProjectionEquivalenceUndefined)

			_, _, err = tc.restoreRemove(malformedHostConfig, "bad/server", true)
			assertMCPProjectionReason(t, err, MCPProjectionReasonProjectionEquivalenceUndefined)

			_, _, err = tc.extractBytes(malformedHostConfig, "bad/server")
			assertMCPProjectionReason(t, err, MCPProjectionReasonProjectionEquivalenceUndefined)

			_, err = tc.entryPresent(malformedHostConfig, "bad/server")
			assertMCPProjectionReason(t, err, MCPProjectionReasonProjectionEquivalenceUndefined)
		})
	}
}

func TestMCPJSONAggregateEntryPresentDoesNotInterpretUnsupportedSameNameEntry(t *testing.T) {
	for _, tc := range mcpJSONAggregateCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			existing := fmt.Appendf(nil, `{"%s":{"context7":%s}}`, tc.parentKey, tc.unsupportedEntry)

			present, err := tc.entryPresent(existing, "context7")
			if err != nil || !present {
				t.Fatalf("entryPresent = %t, %v; want true without interpreting unsupported same-name entry", present, err)
			}

			_, _, err = tc.extractBytes(existing, "context7")
			assertMCPProjectionReason(t, err, MCPProjectionReasonUnsupportedManagedField)
		})
	}
}

func mcpJSONAggregateCases(t *testing.T) []mcpJSONAggregateCase {
	t.Helper()
	return []mcpJSONAggregateCase{
		{
			name:      "claude project",
			parentKey: mcpManagedServersField,
			canonical: func(serverID string) ([]byte, error) {
				return CanonicalClaudeProjectMCPServerEntry(validMCPProjection(serverID))
			},
			merge:            mergeClaudeProjectMCPServerCanonicalEntry,
			remove:           removeClaudeProjectMCPServerProjection,
			restoreRemove:    restoreRemoveClaudeProjectMCPServerProjection,
			extractBytes:     extractClaudeProjectMCPServerProjectionBytes,
			entryPresent:     claudeProjectMCPServerEntryPresent,
			parentPresent:    claudeProjectMCPServersParentPresent,
			unsupportedEntry: `{"type":"stdio","command":"npx","headers":{}}`,
		},
		{
			name:      "claude global",
			parentKey: mcpManagedServersField,
			canonical: func(serverID string) ([]byte, error) {
				return CanonicalClaudeGlobalMCPServerEntry(validClaudeGlobalMCPProjection(serverID))
			},
			merge:            mergeClaudeGlobalMCPServerCanonicalEntry,
			remove:           removeClaudeGlobalMCPServerProjection,
			restoreRemove:    restoreRemoveClaudeGlobalMCPServerProjection,
			extractBytes:     extractClaudeGlobalMCPServerProjectionBytes,
			entryPresent:     claudeGlobalMCPServerEntryPresent,
			parentPresent:    claudeGlobalMCPServersParentPresent,
			unsupportedEntry: `{"type":"stdio","command":"npx","headers":{}}`,
		},
		{
			name:      "antigravity global",
			parentKey: mcpManagedServersField,
			canonical: func(serverID string) ([]byte, error) {
				return CanonicalAntigravityGlobalMCPServerEntry(validAntigravityMCPProjection(serverID))
			},
			merge:            mergeAntigravityGlobalMCPServerCanonicalEntry,
			remove:           removeAntigravityGlobalMCPServerProjection,
			restoreRemove:    restoreRemoveAntigravityGlobalMCPServerProjection,
			extractBytes:     extractAntigravityGlobalMCPServerProjectionBytes,
			entryPresent:     antigravityGlobalMCPServerEntryPresent,
			parentPresent:    antigravityGlobalMCPServersParentPresent,
			unsupportedEntry: `{"command":"npx","headers":{}}`,
		},
		{
			name:      "opencode project",
			parentKey: openCodeProjectMCPManagedField,
			canonical: func(serverID string) ([]byte, error) {
				return CanonicalOpenCodeProjectMCPServerEntry(validOpenCodeMCPProjection(serverID))
			},
			merge:            mergeOpenCodeProjectMCPServerCanonicalEntry,
			remove:           removeOpenCodeProjectMCPServerProjection,
			restoreRemove:    restoreRemoveOpenCodeProjectMCPServerProjection,
			extractBytes:     extractOpenCodeProjectMCPServerProjectionBytes,
			entryPresent:     openCodeProjectMCPServerEntryPresent,
			parentPresent:    openCodeProjectMCPServersParentPresent,
			unsupportedEntry: `{"type":"local","command":["npx"],"environment":{}}`,
		},
		{
			name:      "opencode global",
			parentKey: openCodeProjectMCPManagedField,
			canonical: func(serverID string) ([]byte, error) {
				return CanonicalOpenCodeGlobalMCPServerEntry(validOpenCodeGlobalMCPProjection(serverID))
			},
			merge:            mergeOpenCodeGlobalMCPServerCanonicalEntry,
			remove:           removeOpenCodeGlobalMCPServerProjection,
			restoreRemove:    restoreRemoveOpenCodeGlobalMCPServerProjection,
			extractBytes:     extractOpenCodeGlobalMCPServerProjectionBytes,
			entryPresent:     openCodeGlobalMCPServerEntryPresent,
			parentPresent:    openCodeGlobalMCPServersParentPresent,
			unsupportedEntry: `{"type":"local","command":["npx"],"headers":{}}`,
		},
	}
}

func mustMCPJSONCanonicalEntry(t *testing.T, tc mcpJSONAggregateCase, serverID string) []byte {
	t.Helper()
	canonical, err := tc.canonical(serverID)
	if err != nil {
		t.Fatalf("%s canonical(%q) returned error: %v", tc.name, serverID, err)
	}
	return canonical
}
