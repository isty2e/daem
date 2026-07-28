package mcpcodec

import (
	"bytes"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

type mcpProjectionMutationCase struct {
	name       string
	placement  aggregate.MCPPlacementID
	initial    []byte
	canonical  func(string, string) ([]byte, error)
	secretText string
}

func TestMCPPlacementOperationsFoldAndVerifyMixedMutations(t *testing.T) {
	for _, tc := range mcpProjectionMutationCases() {
		t.Run(tc.name, func(t *testing.T) {
			operations, ok := mcpPlacementOperationsForID(tc.placement)
			if !ok {
				t.Fatalf("placement operations %q missing", tc.placement)
			}

			existing := append([]byte(nil), tc.initial...)
			for _, serverID := range []string{"replace-me", "remove-me"} {
				canonical := mustMutationCanonical(t, tc, serverID, "npx")
				var err error
				existing, err = operations.MergeCanonicalEntry(existing, serverID, canonical)
				if err != nil {
					t.Fatalf("seed %q: %v", serverID, err)
				}
			}

			replacement := mustMutationCanonical(t, tc, "replace-me", "node")
			created := mustMutationCanonical(t, tc, "created", "uvx")
			mutations := []MCPProjectionMutation{
				mustMCPProjectionUpsert(t, "replace-me", replacement),
				mustMCPProjectionRemoval(t, "remove-me"),
				mustMCPProjectionInsert(t, "created", created),
			}

			folded, err := operations.FoldMutations(existing, mutations)
			if err != nil {
				t.Fatalf("FoldMutations returned error: %v", err)
			}
			if err := operations.VerifyMutations(folded, mutations); err != nil {
				t.Fatalf("VerifyMutations returned error: %v", err)
			}
			if !bytes.Contains(folded, []byte(tc.secretText)) {
				t.Fatalf("folded aggregate lost unmanaged secret canary: %s", folded)
			}
			if present, err := operations.EntryPresent(folded, "remove-me"); err != nil || present {
				t.Fatalf("removed entry present = %t, err = %v", present, err)
			}
			for serverID, canonical := range map[string][]byte{
				"replace-me": replacement,
				"created":    created,
			} {
				comparison, err := operations.CompareCanonicalEntry(folded, serverID, canonical)
				if err != nil || !comparison.Present || !comparison.Equivalent {
					t.Fatalf("comparison %q = %#v, err = %v", serverID, comparison, err)
				}
			}
		})
	}
}

func TestMCPProjectionInsertRequiresReplacementAuthority(t *testing.T) {
	for _, tc := range []mcpProjectionMutationCase{
		mcpProjectionMutationCases()[0],
		mcpProjectionMutationCases()[5],
	} {
		t.Run(tc.name, func(t *testing.T) {
			operations, ok := mcpPlacementOperationsForID(tc.placement)
			if !ok {
				t.Fatalf("placement operations %q missing", tc.placement)
			}
			original := mustMutationCanonical(t, tc, "same-name", "npx")
			existing, err := operations.MergeCanonicalEntry(tc.initial, "same-name", original)
			if err != nil {
				t.Fatalf("seed existing entry: %v", err)
			}
			desired := mustMutationCanonical(t, tc, "same-name", "node")

			_, err = operations.FoldMutations(existing, []MCPProjectionMutation{
				mustMCPProjectionInsert(t, "same-name", desired),
			})
			if err == nil || !strings.Contains(err.Error(), "managed subject ownership evidence") {
				t.Fatalf("insert collision error = %v", err)
			}

			folded, err := operations.FoldMutations(existing, []MCPProjectionMutation{
				mustMCPProjectionUpsert(t, "same-name", desired),
			})
			if err != nil {
				t.Fatalf("authorized upsert returned error: %v", err)
			}
			if err := operations.VerifyMutations(folded, []MCPProjectionMutation{
				mustMCPProjectionUpsert(t, "same-name", desired),
			}); err != nil {
				t.Fatalf("authorized upsert verification: %v", err)
			}
		})
	}
}

func TestMCPProjectionMutationBatchRejectsUnsupportedSameNameEntry(t *testing.T) {
	cases := []struct {
		name      string
		placement aggregate.MCPPlacementID
		existing  []byte
		canonical []byte
	}{
		{
			name:      "JSON",
			placement: aggregate.MCPPlacementClaudeProject,
			existing:  []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"npx","headers":{}}}}`),
			canonical: mustMutationCanonical(t, mcpProjectionMutationCases()[0], "context7", "node"),
		},
		{
			name:      "TOML",
			placement: aggregate.MCPPlacementCodexProject,
			existing:  []byte("[mcp_servers.context7]\ncommand = \"npx\"\nenv = { TOKEN = \"SECRET\" }\n"),
			canonical: mustMutationCanonical(t, mcpProjectionMutationCases()[5], "context7", "node"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			operations, ok := mcpPlacementOperationsForID(tc.placement)
			if !ok {
				t.Fatalf("placement operations %q missing", tc.placement)
			}
			for _, mutation := range []MCPProjectionMutation{
				mustMCPProjectionInsert(t, "context7", tc.canonical),
				mustMCPProjectionUpsert(t, "context7", tc.canonical),
				mustMCPProjectionRemoval(t, "context7"),
			} {
				_, err := operations.FoldMutations(tc.existing, []MCPProjectionMutation{mutation})
				assertMCPProjectionReason(t, err, MCPProjectionReasonUnsupportedManagedField)
			}
		})
	}
}

func TestMCPProjectionMutationBatchRejectsInvalidAndDuplicateValues(t *testing.T) {
	operations, ok := mcpPlacementOperationsForID(aggregate.MCPPlacementClaudeProject)
	if !ok {
		t.Fatal("Claude project placement operations missing")
	}
	canonical := mustMutationCanonical(t, mcpProjectionMutationCases()[0], "context7", "npx")
	mutation := mustMCPProjectionInsert(t, "context7", canonical)

	for _, tc := range []struct {
		name      string
		mutations []MCPProjectionMutation
		want      string
	}{
		{name: "empty", mutations: nil, want: "batch is empty"},
		{name: "zero value", mutations: []MCPProjectionMutation{{}}, want: "stable token"},
		{name: "duplicate", mutations: []MCPProjectionMutation{mutation, mutation}, want: "repeats server id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := operations.FoldMutations(nil, tc.mutations); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("FoldMutations error = %v, want %q", err, tc.want)
			}
			if err := operations.VerifyMutations(nil, tc.mutations); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("VerifyMutations error = %v, want %q", err, tc.want)
			}
		})
	}

	if _, err := NewMCPProjectionInsert("bad/id", canonical); err == nil {
		t.Fatal("NewMCPProjectionInsert accepted invalid server id")
	}
	if _, err := NewMCPProjectionUpsert("context7", []byte(" \n")); err == nil {
		t.Fatal("NewMCPProjectionUpsert accepted empty canonical entry")
	}
	if _, err := NewMCPProjectionRemoval("bad/id"); err == nil {
		t.Fatal("NewMCPProjectionRemoval accepted invalid server id")
	}
}

func TestMCPProjectionMutationOwnsCanonicalBytes(t *testing.T) {
	tc := mcpProjectionMutationCases()[0]
	operations, ok := mcpPlacementOperationsForID(tc.placement)
	if !ok {
		t.Fatal("Claude project placement operations missing")
	}
	canonical := mustMutationCanonical(t, tc, "context7", "npx")
	mutation := mustMCPProjectionInsert(t, "context7", canonical)
	for index := range canonical {
		canonical[index] = '!'
	}

	folded, err := operations.FoldMutations(nil, []MCPProjectionMutation{mutation})
	if err != nil {
		t.Fatalf("FoldMutations after caller mutation returned error: %v", err)
	}
	if err := operations.VerifyMutations(folded, []MCPProjectionMutation{mutation}); err != nil {
		t.Fatalf("VerifyMutations after caller mutation returned error: %v", err)
	}
}

func mcpProjectionMutationCases() []mcpProjectionMutationCase {
	jsonInitial := []byte(`{"unmanaged":{"secret":"JSON_CANARY"}}`)
	return []mcpProjectionMutationCase{
		{
			name: "claude project", placement: aggregate.MCPPlacementClaudeProject,
			initial: jsonInitial, secretText: "JSON_CANARY",
			canonical: func(serverID string, command string) ([]byte, error) {
				projection := validMCPProjection(serverID)
				projection.Command = command
				return CanonicalClaudeProjectMCPServerEntry(projection)
			},
		},
		{
			name: "claude global", placement: aggregate.MCPPlacementClaudeGlobal,
			initial: jsonInitial, secretText: "JSON_CANARY",
			canonical: func(serverID string, command string) ([]byte, error) {
				projection := validClaudeGlobalMCPProjection(serverID)
				projection.Command = command
				return CanonicalClaudeGlobalMCPServerEntry(projection)
			},
		},
		{
			name: "antigravity global", placement: aggregate.MCPPlacementAntigravityGlobal,
			initial: jsonInitial, secretText: "JSON_CANARY",
			canonical: func(serverID string, command string) ([]byte, error) {
				projection := validAntigravityMCPProjection(serverID)
				projection.Command = command
				return CanonicalAntigravityGlobalMCPServerEntry(projection)
			},
		},
		{
			name: "opencode project", placement: aggregate.MCPPlacementOpenCodeProject,
			initial: jsonInitial, secretText: "JSON_CANARY",
			canonical: func(serverID string, command string) ([]byte, error) {
				projection := validOpenCodeMCPProjection(serverID)
				projection.Command = command
				return CanonicalOpenCodeProjectMCPServerEntry(projection)
			},
		},
		{
			name: "opencode global", placement: aggregate.MCPPlacementOpenCodeGlobal,
			initial: jsonInitial, secretText: "JSON_CANARY",
			canonical: func(serverID string, command string) ([]byte, error) {
				projection := validOpenCodeGlobalMCPProjection(serverID)
				projection.Command = command
				return CanonicalOpenCodeGlobalMCPServerEntry(projection)
			},
		},
		{
			name: "codex project", placement: aggregate.MCPPlacementCodexProject,
			initial: []byte("[unmanaged]\nsecret = \"TOML_CANARY\"\n"), secretText: "TOML_CANARY",
			canonical: func(serverID string, command string) ([]byte, error) {
				projection := validCodexMCPProjection(serverID)
				projection.Command = command
				return CanonicalCodexProjectMCPServerEntry(projection)
			},
		},
		{
			name: "codex global", placement: aggregate.MCPPlacementCodexGlobal,
			initial: []byte("[unmanaged]\nsecret = \"TOML_CANARY\"\n"), secretText: "TOML_CANARY",
			canonical: func(serverID string, command string) ([]byte, error) {
				projection := validCodexGlobalMCPProjection(serverID)
				projection.Command = command
				projection.EnvVars = []string{"CODEX_TOKEN"}
				return CanonicalCodexGlobalMCPServerEntry(projection)
			},
		},
	}
}

func mustMutationCanonical(
	t *testing.T,
	tc mcpProjectionMutationCase,
	serverID string,
	command string,
) []byte {
	t.Helper()
	canonical, err := tc.canonical(serverID, command)
	if err != nil {
		t.Fatalf("canonical %q: %v", serverID, err)
	}
	return canonical
}

func mustMCPProjectionInsert(t *testing.T, serverID string, canonical []byte) MCPProjectionMutation {
	t.Helper()
	mutation, err := NewMCPProjectionInsert(serverID, canonical)
	if err != nil {
		t.Fatalf("NewMCPProjectionInsert(%q): %v", serverID, err)
	}
	return mutation
}

func mustMCPProjectionUpsert(t *testing.T, serverID string, canonical []byte) MCPProjectionMutation {
	t.Helper()
	mutation, err := NewMCPProjectionUpsert(serverID, canonical)
	if err != nil {
		t.Fatalf("NewMCPProjectionUpsert(%q): %v", serverID, err)
	}
	return mutation
}

func mustMCPProjectionRemoval(t *testing.T, serverID string) MCPProjectionMutation {
	t.Helper()
	mutation, err := NewMCPProjectionRemoval(serverID)
	if err != nil {
		t.Fatalf("NewMCPProjectionRemoval(%q): %v", serverID, err)
	}
	return mutation
}
