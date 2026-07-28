package journal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/target"
	mcptest "github.com/isty2e/daem/test/testkit/mcp"
)

func TestRecoveryContentPathBaselineCacheReusesOneGlobalSnapshot(t *testing.T) {
	root := t.TempDir()
	hostPath := filepath.Join(root, "config.json")
	canonical, err := mcpcodec.CanonicalClaudeGlobalMCPServerEntry(mcpcodec.ClaudeGlobalMCPServerProjection{
		ServerID:        "context7",
		Command:         "npx",
		Args:            []string{"-y", "@upstream/context7"},
		AdapterContract: aggregate.ClaudeGlobalMCPStdioEnvAdapterV1,
	})
	if err != nil {
		t.Fatalf("canonical entry: %v", err)
	}
	operations, ok := mcptest.OperationsForPlacementID(aggregate.MCPPlacementClaudeGlobal)
	if !ok {
		t.Fatal("Claude global MCP placement operations missing")
	}
	before, err := operations.MergeCanonicalEntry(nil, "context7", canonical)
	if err != nil {
		t.Fatalf("merge baseline entry: %v", err)
	}
	if err := os.WriteFile(hostPath, before, 0o640); err != nil {
		t.Fatalf("write baseline: %v", err)
	}
	action := claudeRecoveryBaselineMutation(t, target.ScopeGlobal, "context7")
	resolveCalls := 0
	resolver := func(destination output.Destination) (string, error) {
		resolveCalls++
		return hostPath, nil
	}
	cache, err := newRecoveryContentPathBaselineCache(
		[]pathMutation{action},
		journalTestCodecs(),
		journalTestFilesystem(),
	)
	if err != nil {
		t.Fatalf("newRecoveryContentPathBaselineCache: %v", err)
	}

	first, err := cache.capture(context.Background(), action, resolver, nil, nil)
	if err != nil {
		t.Fatalf("first capture: %v", err)
	}
	if err := os.WriteFile(hostPath, []byte(`{"mcpServers":{}}`), 0o600); err != nil {
		t.Fatalf("replace host path: %v", err)
	}
	second, err := cache.capture(context.Background(), action, resolver, nil, nil)
	if err != nil {
		t.Fatalf("second capture: %v", err)
	}

	if resolveCalls != 1 {
		t.Fatalf("resolver calls = %d, want one", resolveCalls)
	}
	firstProjection, firstPresent, err := first.projection(action.ContentPath)
	if err != nil {
		t.Fatalf("first projection: %v", err)
	}
	secondProjection, secondPresent, err := second.projection(action.ContentPath)
	if err != nil {
		t.Fatalf("second projection: %v", err)
	}
	if !firstPresent || !secondPresent || string(firstProjection) != string(canonical) || string(secondProjection) != string(canonical) {
		t.Fatalf("captured projections = %q then %q, want immutable canonical %q", firstProjection, secondProjection, canonical)
	}
	if first.mode.Perm() != 0o640 || second.mode.Perm() != 0o640 {
		t.Fatalf("captured modes = %o then %o, want original 0640", first.mode.Perm(), second.mode.Perm())
	}
	if first.pathContentHash != "" || second.pathContentHash != "" {
		t.Fatalf("global captured path hashes = %q then %q, want no unused whole-file hash", first.pathContentHash, second.pathContentHash)
	}
}

func TestRecoveryContentPathBaselineCacheReadsLargeAggregateOnce(t *testing.T) {
	const memberCount = 256
	root := t.TempDir()
	hostPath := filepath.Join(root, "config.json")
	serverEntries := make(map[string]json.RawMessage, memberCount)
	mutations := make([]pathMutation, 0, memberCount)
	for index := range memberCount {
		serverID := fmt.Sprintf("server-%03d", index)
		canonical, err := mcpcodec.CanonicalClaudeGlobalMCPServerEntry(mcpcodec.ClaudeGlobalMCPServerProjection{
			ServerID:        serverID,
			Command:         "npx",
			Args:            []string{"-y", "@upstream/" + serverID},
			AdapterContract: aggregate.ClaudeGlobalMCPStdioEnvAdapterV1,
		})
		if err != nil {
			t.Fatalf("canonical entry %q: %v", serverID, err)
		}
		serverEntries[serverID] = json.RawMessage(canonical)
		mutations = append(mutations, claudeRecoveryBaselineMutation(t, target.ScopeGlobal, serverID))
	}
	baseline, err := json.Marshal(map[string]any{
		"mcpServers": serverEntries,
		"unmanaged":  "JOURNAL_BASELINE_CANARY",
	})
	if err != nil {
		t.Fatalf("marshal large baseline: %v", err)
	}
	if err := os.WriteFile(hostPath, baseline, 0o640); err != nil {
		t.Fatalf("write large baseline: %v", err)
	}
	resolveCalls := 0
	resolver := func(output.Destination) (string, error) {
		resolveCalls++
		return hostPath, nil
	}
	cache, err := newRecoveryContentPathBaselineCache(
		mutations,
		journalTestCodecs(),
		journalTestFilesystem(),
	)
	if err != nil {
		t.Fatalf("newRecoveryContentPathBaselineCache: %v", err)
	}

	for index, action := range mutations {
		captured, err := cache.capture(t.Context(), action, resolver, nil, nil)
		if err != nil {
			t.Fatalf("capture member %d: %v", index, err)
		}
		projection, present, err := captured.projection(action.ContentPath)
		serverID := fmt.Sprintf("server-%03d", index)
		if err != nil || !present || !bytes.Equal(projection, serverEntries[serverID]) {
			t.Fatalf(
				"projection %d present=%t content=%s want=%s err=%v",
				index,
				present,
				projection,
				serverEntries[serverID],
				err,
			)
		}
		if index == 0 {
			if err := os.WriteFile(hostPath, []byte(`{"mcpServers":{}}`), 0o600); err != nil {
				t.Fatalf("replace host path after baseline: %v", err)
			}
		}
	}
	if resolveCalls != 1 {
		t.Fatalf("resolver calls = %d, want one for %d journal subjects", resolveCalls, memberCount)
	}
}

func TestRecoveryContentPathBaselineCacheRejectsDuplicateSelectionPerScope(t *testing.T) {
	action := claudeRecoveryBaselineMutation(t, target.ScopeGlobal, "context7")

	if _, err := newRecoveryContentPathBaselineCache(
		[]pathMutation{action, action},
		journalTestCodecs(),
		journalTestFilesystem(),
	); err == nil {
		t.Fatal("newRecoveryContentPathBaselineCache accepted a duplicate selection")
	}
	projectAction := claudeRecoveryBaselineMutation(t, target.ScopeProject, "context7")
	cache, err := newRecoveryContentPathBaselineCache(
		[]pathMutation{action, projectAction},
		journalTestCodecs(),
		journalTestFilesystem(),
	)
	if err != nil {
		t.Fatalf("cross-scope selections must remain distinct: %v", err)
	}
	if len(cache.requests) != 2 {
		t.Fatalf("request count = %d, want two scope-distinct baselines", len(cache.requests))
	}
}

func claudeRecoveryBaselineMutation(
	t *testing.T,
	scope target.Scope,
	serverID string,
) pathMutation {
	t.Helper()
	placement, ok := aggregate.ImplementedMCPPlacement(target.TargetClaudeCode, scope)
	if !ok {
		t.Fatalf("Claude MCP placement for %s is missing", scope)
	}
	contract, err := placement.ProjectionContract(serverID)
	if err != nil {
		t.Fatalf("ProjectionContract(%q): %v", serverID, err)
	}
	return pathMutation{
		Scope:             scope,
		Destination:       placement.ConfigPath(),
		ContentPath:       output.ContentPath(contract.Address().ContentPath()),
		AggregateContract: pointerToAggregateContract(contract),
	}
}
