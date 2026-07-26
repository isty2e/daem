package mcpcodec

import (
	"slices"
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
			if tc.spec.configPath != placement.ConfigPath() ||
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
		{name: "delegate requirement without probe", edit: func(input *mcpPlacementOperationsInput) { input.probeRequiresDelegate = true }, want: "without a runtime-probe launch"},
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

func TestMCPPlacementRuntimeProbeCapabilitiesAreExact(t *testing.T) {
	supported := make(map[aggregate.MCPPlacementID]bool)
	for _, operations := range implementedMCPPlacementOperationCatalog {
		if operations.SupportsRuntimeProbe() {
			supported[operations.Placement().ID()] = operations.RuntimeProbeRequiresDelegatePlan()
		}
	}
	want := map[aggregate.MCPPlacementID]bool{
		aggregate.MCPPlacementClaudeProject:   true,
		aggregate.MCPPlacementOpenCodeProject: false,
	}
	if len(supported) != len(want) {
		t.Fatalf("runtime-probe placements = %#v, want %#v", supported, want)
	}
	for id, requiresDelegate := range want {
		if supported[id] != requiresDelegate {
			t.Fatalf(
				"runtime-probe placement %q requires delegate = %v, want %v",
				id,
				supported[id],
				requiresDelegate,
			)
		}
	}
	placements := RuntimeProbePlacements()
	gotIDs := make([]aggregate.MCPPlacementID, 0, len(placements))
	for _, placement := range placements {
		gotIDs = append(gotIDs, placement.ID())
	}
	wantIDs := []aggregate.MCPPlacementID{
		aggregate.MCPPlacementClaudeProject,
		aggregate.MCPPlacementOpenCodeProject,
	}
	if !slices.Equal(gotIDs, wantIDs) {
		t.Fatalf("runtime-probe placement order = %v, want %v", gotIDs, wantIDs)
	}
	placements[0] = aggregate.MCPPlacement{}
	if got := RuntimeProbePlacements()[0].ID(); got != wantIDs[0] {
		t.Fatalf("runtime-probe placements leaked caller mutation: first id = %q, want %q", got, wantIDs[0])
	}
}

func TestMCPPlacementRuntimeProbeLaunchLowersCanonicalEntriesDefensively(t *testing.T) {
	claudeOperations, ok := ImplementedMCPPlacementOperationsForID(aggregate.MCPPlacementClaudeProject)
	if !ok {
		t.Fatal("Claude project operation row missing")
	}
	claudeProjection := validMCPProjection("context7")
	claudeProjection.Command = "node"
	claudeProjection.Args = []string{"server.js", "--stdio"}
	claudeProjection.Env = map[string]string{"API_TOKEN": "${HOST_TOKEN}"}
	claudeCanonical := mustCanonicalClaudeProjectMCPServerEntry(t, claudeProjection)

	command, args, env, err := claudeOperations.RuntimeProbeLaunch(string(claudeCanonical))
	if err != nil {
		t.Fatalf("Claude RuntimeProbeLaunch returned error: %v", err)
	}
	if command != "node" ||
		!slices.Equal(args, []string{"server.js", "--stdio"}) ||
		env["API_TOKEN"] != "HOST_TOKEN" {
		t.Fatalf("Claude runtime launch = %q %#v %#v", command, args, env)
	}
	args[0] = "mutated"
	env["API_TOKEN"] = "MUTATED"
	_, secondArgs, secondEnv, err := claudeOperations.RuntimeProbeLaunch(string(claudeCanonical))
	if err != nil {
		t.Fatalf("second Claude RuntimeProbeLaunch returned error: %v", err)
	}
	if secondArgs[0] != "server.js" || secondEnv["API_TOKEN"] != "HOST_TOKEN" {
		t.Fatalf("runtime launch aliased caller mutations: %#v %#v", secondArgs, secondEnv)
	}

	openCodeOperations, ok := ImplementedMCPPlacementOperationsForID(aggregate.MCPPlacementOpenCodeProject)
	if !ok {
		t.Fatal("OpenCode project operation row missing")
	}
	openCodeProjection := validOpenCodeMCPProjection("context7")
	openCodeProjection.Command = "node"
	openCodeProjection.Args = []string{"server.js"}
	openCodeCanonical, err := CanonicalOpenCodeProjectMCPServerEntry(openCodeProjection)
	if err != nil {
		t.Fatalf("CanonicalOpenCodeProjectMCPServerEntry returned error: %v", err)
	}
	command, args, env, err = openCodeOperations.RuntimeProbeLaunch(string(openCodeCanonical))
	if err != nil {
		t.Fatalf("OpenCode RuntimeProbeLaunch returned error: %v", err)
	}
	if command != "node" || !slices.Equal(args, []string{"server.js"}) || len(env) != 0 {
		t.Fatalf("OpenCode runtime launch = %q %#v %#v", command, args, env)
	}

	codexOperations, ok := ImplementedMCPPlacementOperationsForID(aggregate.MCPPlacementCodexProject)
	if !ok {
		t.Fatal("Codex project operation row missing")
	}
	if _, _, _, err := codexOperations.RuntimeProbeLaunch(`{"command":"node"}`); err == nil ||
		!strings.Contains(err.Error(), "does not support runtime probes") {
		t.Fatalf("unsupported RuntimeProbeLaunch error = %v", err)
	}
}

func TestMCPPlacementRuntimeProbeLaunchRejectsMalformedOrSecretBearingCanonicalEntries(t *testing.T) {
	claudeOperations, ok := ImplementedMCPPlacementOperationsForID(aggregate.MCPPlacementClaudeProject)
	if !ok {
		t.Fatal("Claude project operation row missing")
	}
	if _, _, _, err := claudeOperations.RuntimeProbeLaunch(
		`{"type":"stdio","command":"node","args":[],"env":{"TOKEN":"SECRET"}}`,
	); err == nil {
		t.Fatal("Claude RuntimeProbeLaunch accepted a literal secret")
	}

	openCodeOperations, ok := ImplementedMCPPlacementOperationsForID(aggregate.MCPPlacementOpenCodeProject)
	if !ok {
		t.Fatal("OpenCode project operation row missing")
	}
	if _, _, _, err := openCodeOperations.RuntimeProbeLaunch(
		`{"type":"local","command":[]}`,
	); err == nil {
		t.Fatal("OpenCode RuntimeProbeLaunch accepted an empty command vector")
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
	operations, ok := ImplementedMCPPlacementOperationsForID(aggregate.MCPPlacementClaudeProject)
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
	operations, ok := ImplementedMCPPlacementOperationsForID(aggregate.MCPPlacementClaudeProject)
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
	operations, ok := ImplementedMCPPlacementOperationsForID(aggregate.MCPPlacementCodexGlobal)
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
			operations, ok := ImplementedMCPPlacementOperationsForID(tc.placement)
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
	operations, ok := ImplementedMCPPlacementOperationsForID(aggregate.MCPPlacementCodexProject)
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

func mustCanonicalCodexProjectMCPServerEntry(t *testing.T, projection CodexProjectMCPServerProjection) []byte {
	t.Helper()
	canonical, err := CanonicalCodexProjectMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("CanonicalCodexProjectMCPServerEntry returned error: %v", err)
	}
	return canonical
}
