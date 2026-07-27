package mcpcodec

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestImplementedMCPPlacementOperationsCoverImplementedPlacements(t *testing.T) {
	placements := aggregate.ImplementedMCPPlacements()
	operations := implementedMCPPlacementOperationCatalog
	if len(operations) != len(placements) {
		t.Fatalf("operation rows = %d, placements = %d", len(operations), len(placements))
	}

	byID := make(map[aggregate.MCPPlacementID]aggregate.MCPPlacement, len(placements))
	for _, placement := range placements {
		byID[placement.ID()] = placement
	}
	seen := make(map[aggregate.MCPPlacementID]struct{}, len(operations))
	for _, operation := range operations {
		if err := operation.validate(); err != nil {
			t.Fatalf("operation %q did not validate: %v", operation.Placement().ID(), err)
		}
		placement := operation.Placement()
		if _, ok := byID[placement.ID()]; !ok {
			t.Fatalf("operation %q has no placement row", placement.ID())
		}
		if _, ok := seen[placement.ID()]; ok {
			t.Fatalf("duplicate operation row for %q", placement.ID())
		}
		seen[placement.ID()] = struct{}{}

		contentPath, err := operation.ContentPath("context7")
		if err != nil {
			t.Fatalf("%s ContentPath returned error: %v", placement.ID(), err)
		}
		serverID, ok := operation.ServerIDFromContentPath(contentPath)
		if !ok || serverID != "context7" {
			t.Fatalf("%s ServerIDFromContentPath(%q) = %q, %v", placement.ID(), contentPath, serverID, ok)
		}
		if serverID, ok := operation.ServerIDFromContentPath(aggregate.ContentPath(placement.ContentPathPrefix())); ok || serverID != "" {
			t.Fatalf("%s parent path resolved to %q, %v", placement.ID(), serverID, ok)
		}
		if serverID, ok := operation.ServerIDFromContentPath(contentPath + "/nested"); ok || serverID != "" {
			t.Fatalf("%s nested path resolved to %q, %v", placement.ID(), serverID, ok)
		}
		for _, malformed := range []aggregate.ContentPath{
			aggregate.ContentPath(string(contentPath) + "/"),
			aggregate.ContentPath(string(contentPath) + "/.."),
			aggregate.ContentPath(string(contentPath) + "\\nested"),
			aggregate.ContentPath(string(contentPath) + "\x00"),
		} {
			if serverID, ok := operation.ServerIDFromContentPath(malformed); ok || serverID != "" {
				t.Fatalf("%s malformed path %q resolved to %q, %v", placement.ID(), malformed, serverID, ok)
			}
		}
		byCodecContract, ok := ImplementedMCPPlacementOperationsForCodecContract(placement.CodecContractID())
		if !ok || byCodecContract.Placement().ID() != placement.ID() {
			t.Fatalf("lookup by codec contract %q = %q, %v; want %q", placement.CodecContractID(), byCodecContract.Placement().ID(), ok, placement.ID())
		}
	}
	if operations, ok := ImplementedMCPPlacementOperationsForCodecContract(aggregate.CodecContractID("unknown-codec")); ok || operations.Placement().ID() != "" {
		t.Fatalf("unknown codec contract lookup = %q, %v; want missing", operations.Placement().ID(), ok)
	}
}

func TestJSONMCPConfigSpecsUsePlacementAggregateSpecRows(t *testing.T) {
	cases := []struct {
		name      string
		placement aggregate.MCPPlacementID
		spec      mcpConfigSpec
	}{
		{
			name: "claude project", placement: aggregate.MCPPlacementClaudeProject,
			spec: claudeProjectMCPConfigSpec(),
		},
		{
			name: "claude global", placement: aggregate.MCPPlacementClaudeGlobal,
			spec: claudeGlobalMCPConfigSpec(),
		},
		{
			name: "antigravity global", placement: aggregate.MCPPlacementAntigravityGlobal,
			spec: antigravityGlobalMCPConfigSpec(),
		},
		{
			name: "opencode project", placement: aggregate.MCPPlacementOpenCodeProject,
			spec: openCodeProjectMCPConfigSpec(),
		},
		{
			name: "opencode global", placement: aggregate.MCPPlacementOpenCodeGlobal,
			spec: openCodeGlobalMCPConfigSpec(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			placement, ok := aggregate.MCPPlacementForID(tc.placement)
			if !ok {
				t.Fatalf("placement %q missing", tc.placement)
			}
			if tc.spec.configPath != placement.ConfigPath().String() ||
				tc.spec.serversPath != string(placement.ContentPathPrefix()) {
				t.Fatalf("codec spec = %#v, placement = %#v", tc.spec, placement.AggregateSpec())
			}
		})
	}
}

func TestMCPPlacementOperationsRejectIncompleteRows(t *testing.T) {
	placement, ok := aggregate.MCPPlacementForID(aggregate.MCPPlacementCodexProject)
	if !ok {
		t.Fatal("Codex project placement missing")
	}
	valid := mcpPlacementOperationsInput{
		placement:             placement,
		foldMutations:         foldCodexProjectMCPProjectionMutations,
		restoreMutations:      restoreCodexProjectMCPProjectionMutations,
		verifyMutations:       verifyCodexProjectMCPProjectionMutations,
		observeCanonical:      observeCodexProjectMCPProjections,
		mergeCanonicalEntry:   mergeCodexProjectMCPServerCanonicalEntry,
		removeProjection:      removeCodexProjectMCPServerProjection,
		restoreRemove:         restoreRemoveCodexProjectMCPServerProjection,
		extractCanonicalEntry: extractCodexProjectMCPServerProjectionBytes,
		compareCanonicalEntry: compareCodexProjectMCPServerCanonicalEntry,
		entryPresent:          codexProjectMCPServerEntryPresent,
		parentPresent:         codexProjectMCPServersParentPresent,
	}
	cases := []struct {
		name string
		edit func(*mcpPlacementOperationsInput)
		want string
	}{
		{name: "missing fold mutations", edit: func(input *mcpPlacementOperationsInput) { input.foldMutations = nil }, want: "fold mutations"},
		{name: "missing restore mutations", edit: func(input *mcpPlacementOperationsInput) { input.restoreMutations = nil }, want: "restore mutations"},
		{name: "missing verify mutations", edit: func(input *mcpPlacementOperationsInput) { input.verifyMutations = nil }, want: "verify mutations"},
		{name: "missing canonical observation", edit: func(input *mcpPlacementOperationsInput) { input.observeCanonical = nil }, want: "canonical observation"},
		{name: "missing merge", edit: func(input *mcpPlacementOperationsInput) { input.mergeCanonicalEntry = nil }, want: "merge canonical entry"},
		{name: "missing remove", edit: func(input *mcpPlacementOperationsInput) { input.removeProjection = nil }, want: "remove projection"},
		{name: "missing restore", edit: func(input *mcpPlacementOperationsInput) { input.restoreRemove = nil }, want: "restore-remove"},
		{name: "missing extract", edit: func(input *mcpPlacementOperationsInput) { input.extractCanonicalEntry = nil }, want: "extract canonical entry"},
		{name: "missing compare", edit: func(input *mcpPlacementOperationsInput) { input.compareCanonicalEntry = nil }, want: "compare canonical entry"},
		{name: "missing entry present", edit: func(input *mcpPlacementOperationsInput) { input.entryPresent = nil }, want: "entry-present"},
		{name: "missing parent present", edit: func(input *mcpPlacementOperationsInput) { input.parentPresent = nil }, want: "parent-present"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := valid
			tc.edit(&input)
			_, err := newMCPPlacementOperations(input)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestValidateMCPPlacementOperationCatalogRejectsMissingDuplicateAndStrayRows(t *testing.T) {
	operations := append([]MCPPlacementOperations(nil), implementedMCPPlacementOperationCatalog...)
	placements := aggregate.ImplementedMCPPlacements()
	if len(operations) < 2 || len(placements) < 2 {
		t.Fatal("test requires multiple operation and placement rows")
	}

	cases := []struct {
		name       string
		operations []MCPPlacementOperations
		placements []aggregate.MCPPlacement
		want       string
	}{
		{
			name:       "missing operation",
			operations: operations[1:],
			placements: placements,
			want:       "has no operation row",
		},
		{
			name:       "duplicate operation",
			operations: append(append([]MCPPlacementOperations(nil), operations...), operations[0]),
			placements: placements,
			want:       "share placement id",
		},
		{
			name:       "stray operation",
			operations: []MCPPlacementOperations{mustTestMCPPlacementOperations(t, "stray")},
			placements: nil,
			want:       "have no implemented placement row",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateMCPPlacementOperationCatalog(tc.operations, tc.placements)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestMCPPlacementOperationsJSONPlacementRoundTrip(t *testing.T) {
	operations, ok := mcpPlacementOperationsForID(aggregate.MCPPlacementClaudeProject)
	if !ok {
		t.Fatal("Claude project operation row missing")
	}
	projection := validMCPProjection("context7")
	projection.Args = []string{"-y", "@upstash/context7-mcp"}
	canonical, err := CanonicalClaudeProjectMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("CanonicalClaudeProjectMCPServerEntry returned error: %v", err)
	}
	existing := []byte(`{"mcpServers":{"sibling":{"type":"stdio","command":"node","args":["manual.js"],"env":{"TOKEN":"SECRET"}}}}`)

	merged, err := operations.MergeCanonicalEntry(existing, "context7", canonical)
	if err != nil {
		t.Fatalf("MergeCanonicalEntry returned error: %v", err)
	}
	if present, err := operations.EntryPresent(merged, "context7"); err != nil || !present {
		t.Fatalf("EntryPresent = %v, %v; want present", present, err)
	}
	if parentPresent, err := operations.ParentPresent(merged); err != nil || !parentPresent {
		t.Fatalf("ParentPresent = %v, %v; want present", parentPresent, err)
	}
	extracted, present, err := operations.ExtractCanonicalEntry(merged, "context7")
	if err != nil || !present {
		t.Fatalf("ExtractCanonicalEntry present = %v, err = %v", present, err)
	}
	comparison, err := operations.CompareCanonicalEntry(merged, "context7", canonical)
	if err != nil {
		t.Fatalf("CompareCanonicalEntry returned error: %v", err)
	}
	if !comparison.Present || !comparison.Equivalent || comparison.ContentPath != "/mcpServers/context7" {
		t.Fatalf("comparison = %#v, want present equivalent content path", comparison)
	}
	roundTripComparison, err := operations.CompareCanonicalEntry(merged, "context7", extracted)
	if err != nil {
		t.Fatalf("CompareCanonicalEntry on extracted content returned error: %v", err)
	}
	if !roundTripComparison.Present || !roundTripComparison.Equivalent {
		t.Fatalf("round-trip comparison = %#v, want equivalent", roundTripComparison)
	}

	removed, err := operations.RemoveProjection(merged, "context7")
	if err != nil {
		t.Fatalf("RemoveProjection returned error: %v", err)
	}
	if present, err := operations.EntryPresent(removed, "context7"); err != nil || present {
		t.Fatalf("EntryPresent after remove = %v, %v; want absent", present, err)
	}
	if present, err := operations.EntryPresent(removed, "sibling"); err != nil || !present {
		t.Fatalf("EntryPresent sibling after remove = %v, %v; want present", present, err)
	}
}

func TestMCPPlacementOperationsPropagateInvalidServerIDAcrossOperations(t *testing.T) {
	operations, ok := mcpPlacementOperationsForID(aggregate.MCPPlacementClaudeProject)
	if !ok {
		t.Fatal("Claude project operation row missing")
	}
	canonical, err := CanonicalClaudeProjectMCPServerEntry(validMCPProjection("context7"))
	if err != nil {
		t.Fatalf("CanonicalClaudeProjectMCPServerEntry returned error: %v", err)
	}
	badServerID := "bad/id"
	if _, err := operations.ContentPath(badServerID); err == nil {
		t.Fatal("ContentPath returned nil error for invalid server id")
	}
	if serverID, ok := operations.ServerIDFromContentPath(aggregate.ContentPath("/mcpServers/" + badServerID)); ok || serverID != "" {
		t.Fatalf("ServerIDFromContentPath invalid id = %q, %v", serverID, ok)
	}
	if _, err := operations.MergeCanonicalEntry(nil, badServerID, canonical); err == nil {
		t.Fatal("MergeCanonicalEntry returned nil error for invalid server id")
	}
	if _, err := operations.RemoveProjection(nil, badServerID); err == nil {
		t.Fatal("RemoveProjection returned nil error for invalid server id")
	}
	if _, _, err := operations.ExtractCanonicalEntry(nil, badServerID); err == nil {
		t.Fatal("ExtractCanonicalEntry returned nil error for invalid server id")
	}
	if _, err := operations.CompareCanonicalEntry(nil, badServerID, canonical); err == nil {
		t.Fatal("CompareCanonicalEntry returned nil error for invalid server id")
	}
	if _, err := operations.EntryPresent(nil, badServerID); err == nil {
		t.Fatal("EntryPresent returned nil error for invalid server id")
	}
}

func TestMCPPlacementOperationsCodexTOMLPlacementRoundTrip(t *testing.T) {
	operations, ok := mcpPlacementOperationsForID(aggregate.MCPPlacementCodexGlobal)
	if !ok {
		t.Fatal("Codex global operation row missing")
	}
	projection := validCodexGlobalMCPProjection("context7")
	projection.Args = []string{"-y", "@upstash/context7-mcp"}
	canonical, err := CanonicalCodexGlobalMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("CanonicalCodexGlobalMCPServerEntry returned error: %v", err)
	}
	existing := []byte(`
model = "gpt-5-codex"

[mcp_servers.sibling]
command = "node"
args = ["manual.js"]
`)

	merged, err := operations.MergeCanonicalEntry(existing, "context7", canonical)
	if err != nil {
		t.Fatalf("MergeCanonicalEntry returned error: %v", err)
	}
	for _, want := range []string{`model = "gpt-5-codex"`, `[mcp_servers.sibling]`, `[mcp_servers.context7]`} {
		if !strings.Contains(string(merged), want) {
			t.Fatalf("merged = %s, want %q", merged, want)
		}
	}
	extracted, present, err := operations.ExtractCanonicalEntry(merged, "context7")
	if err != nil || !present {
		t.Fatalf("ExtractCanonicalEntry present = %v, err = %v", present, err)
	}
	comparison, err := operations.CompareCanonicalEntry(merged, "context7", canonical)
	if err != nil {
		t.Fatalf("CompareCanonicalEntry returned error: %v", err)
	}
	if !comparison.Present || !comparison.Equivalent || comparison.ContentPath != "/mcp_servers/context7" {
		t.Fatalf("comparison = %#v, want present equivalent content path", comparison)
	}
	if roundTripComparison, err := operations.CompareCanonicalEntry(merged, "context7", extracted); err != nil ||
		!roundTripComparison.Present ||
		!roundTripComparison.Equivalent {
		t.Fatalf("round-trip comparison = %#v, err = %v; want equivalent", roundTripComparison, err)
	}

	removed, keepFile, err := operations.RestoreRemoveProjection(merged, "context7", true)
	if err != nil {
		t.Fatalf("RestoreRemoveProjection returned error: %v", err)
	}
	if !keepFile || strings.Contains(string(removed), `[mcp_servers.context7]`) {
		t.Fatalf("removed = %s, keepFile = %v; want sibling-only file", removed, keepFile)
	}
	if present, err := operations.EntryPresent(removed, "sibling"); err != nil || !present {
		t.Fatalf("EntryPresent sibling after remove = %v, %v; want present", present, err)
	}
}

func TestMCPPlacementOperationsRestoreRemoveDropsEmptyParentWhenAbsentBefore(t *testing.T) {
	cases := []struct {
		name      string
		placement aggregate.MCPPlacementID
		canonical []byte
	}{
		{
			name:      "claude project json",
			placement: aggregate.MCPPlacementClaudeProject,
			canonical: mustCanonicalClaudeProjectMCPServerEntry(t, validMCPProjection("context7")),
		},
		{
			name:      "codex project toml",
			placement: aggregate.MCPPlacementCodexProject,
			canonical: mustCanonicalCodexProjectMCPServerEntry(t, validCodexMCPProjection("context7")),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			operations, ok := mcpPlacementOperationsForID(tc.placement)
			if !ok {
				t.Fatalf("%s operation row missing", tc.placement)
			}
			merged, err := operations.MergeCanonicalEntry(nil, "context7", tc.canonical)
			if err != nil {
				t.Fatalf("MergeCanonicalEntry returned error: %v", err)
			}
			removed, keepFile, err := operations.RestoreRemoveProjection(merged, "context7", false)
			if err != nil {
				t.Fatalf("RestoreRemoveProjection returned error: %v", err)
			}
			if keepFile || removed != nil {
				t.Fatalf("removed = %q, keepFile = %v; want dropped empty aggregate", removed, keepFile)
			}
		})
	}
}

func TestMCPPlacementOperationsDistinguishAbsentFromMalformedPresentEntry(t *testing.T) {
	operations, ok := mcpPlacementOperationsForID(aggregate.MCPPlacementCodexProject)
	if !ok {
		t.Fatal("Codex project operation row missing")
	}
	canonical, err := CanonicalCodexProjectMCPServerEntry(validCodexMCPProjection("context7"))
	if err != nil {
		t.Fatalf("CanonicalCodexProjectMCPServerEntry returned error: %v", err)
	}

	comparison, err := operations.CompareCanonicalEntry(nil, "context7", canonical)
	if err != nil {
		t.Fatalf("CompareCanonicalEntry absent returned error: %v", err)
	}
	if comparison.Present || comparison.Equivalent {
		t.Fatalf("absent comparison = %#v, want absent/non-equivalent", comparison)
	}

	malformedPresent := []byte(`[mcp_servers.context7]
env = { API_TOKEN = "SECRET" }
`)
	if _, err := operations.CompareCanonicalEntry(malformedPresent, "context7", canonical); err == nil {
		t.Fatal("CompareCanonicalEntry malformed present returned nil error")
	}
	if present, err := operations.EntryPresent(malformedPresent, "context7"); err != nil || !present {
		t.Fatalf("EntryPresent malformed present = %v, %v; want key present", present, err)
	}
}

func mustTestMCPPlacementOperations(t *testing.T, id aggregate.MCPPlacementID) MCPPlacementOperations {
	t.Helper()
	placement, err := aggregate.NewMCPPlacement(aggregate.MCPPlacementInput{
		ID:                id,
		Target:            target.TargetCodex,
		Scope:             target.ScopeProject,
		ConfigLayer:       "codex-test-config",
		ConfigPath:        ".test/config",
		MergeUnit:         aggregate.MCPMergeUnitServerEntry,
		ContentPathPrefix: "/mcp",
		SiblingRetention:  aggregate.MCPSiblingRetentionPreserveUnmanaged,
		CodecContractID:   aggregate.CodecContractID(string(id) + "-codec-v1"),
		ComparedFields:    []string{"command", "target"},
		Absence:           aggregate.MCPAbsenceRemoveBinding,
	})
	if err != nil {
		t.Fatalf("NewMCPPlacement returned error: %v", err)
	}
	operations, err := newMCPPlacementOperations(mcpPlacementOperationsInput{
		placement:             placement,
		foldMutations:         foldCodexProjectMCPProjectionMutations,
		restoreMutations:      restoreCodexProjectMCPProjectionMutations,
		verifyMutations:       verifyCodexProjectMCPProjectionMutations,
		observeCanonical:      observeCodexProjectMCPProjections,
		mergeCanonicalEntry:   mergeCodexProjectMCPServerCanonicalEntry,
		removeProjection:      removeCodexProjectMCPServerProjection,
		restoreRemove:         restoreRemoveCodexProjectMCPServerProjection,
		extractCanonicalEntry: extractCodexProjectMCPServerProjectionBytes,
		compareCanonicalEntry: compareCodexProjectMCPServerCanonicalEntry,
		entryPresent:          codexProjectMCPServerEntryPresent,
		parentPresent:         codexProjectMCPServersParentPresent,
	})
	if err != nil {
		t.Fatalf("newMCPPlacementOperations returned error: %v", err)
	}
	return operations
}

func mustCanonicalClaudeProjectMCPServerEntry(t *testing.T, projection ClaudeProjectMCPServerProjection) []byte {
	t.Helper()
	canonical, err := CanonicalClaudeProjectMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("CanonicalClaudeProjectMCPServerEntry returned error: %v", err)
	}
	return canonical
}

func mustCanonicalCodexProjectMCPServerEntry(t *testing.T, projection MCPNoEnvServerProjection) []byte {
	t.Helper()
	canonical, err := CanonicalCodexProjectMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("CanonicalCodexProjectMCPServerEntry returned error: %v", err)
	}
	return canonical
}
