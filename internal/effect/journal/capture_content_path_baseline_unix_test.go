//go:build darwin || linux

package journal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestRecoveryContentPathBaselineRejectsGlobalFinalSymlink(t *testing.T) {
	targetPath := filepath.Join(t.TempDir(), "target.json")
	if err := os.WriteFile(targetPath, []byte(`{"mcpServers":{}}`), 0o600); err != nil {
		t.Fatalf("write target config: %v", err)
	}
	alias := filepath.Join(t.TempDir(), "config.json")
	if err := os.Symlink(targetPath, alias); err != nil {
		t.Fatalf("create final symlink: %v", err)
	}
	action := globalClaudeRecoveryBaselineMutation(t)
	cache, err := newRecoveryContentPathBaselineCache(
		[]pathMutation{action},
		journalTestCodecs(),
		journalTestFilesystem(),
	)
	if err != nil {
		t.Fatalf("newRecoveryContentPathBaselineCache: %v", err)
	}

	_, err = cache.capture(t.Context(), action, func(output.Destination) (string, error) {
		return alias, nil
	}, nil, nil)
	if err == nil {
		t.Fatal("global recovery baseline followed a final symlink")
	}
}

func TestRecoveryContentPathBaselineResolvesGlobalAncestorSymlink(t *testing.T) {
	physicalParent := t.TempDir()
	physicalPath := filepath.Join(physicalParent, "config.json")
	if err := os.WriteFile(physicalPath, []byte(`{"mcpServers":{}}`), 0o640); err != nil {
		t.Fatalf("write physical config: %v", err)
	}
	aliasParent := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(physicalParent, aliasParent); err != nil {
		t.Fatalf("create parent symlink: %v", err)
	}
	action := globalClaudeRecoveryBaselineMutation(t)
	cache, err := newRecoveryContentPathBaselineCache(
		[]pathMutation{action},
		journalTestCodecs(),
		journalTestFilesystem(),
	)
	if err != nil {
		t.Fatalf("newRecoveryContentPathBaselineCache: %v", err)
	}

	baseline, err := cache.capture(t.Context(), action, func(output.Destination) (string, error) {
		return filepath.Join(aliasParent, "config.json"), nil
	}, nil, nil)
	if err != nil {
		t.Fatalf("capture through ancestor symlink: %v", err)
	}
	if baseline.mode.Perm() != 0o640 {
		t.Fatalf("captured mode = %04o, want 0640", baseline.mode.Perm())
	}
}

func TestRecoveryContentPathBaselineRejectsOversizedAggregateDocument(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostPath := filepath.Join(directory, "config.json")
	file, err := os.OpenFile(hostPath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaximumRecoveryBackupFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	action := globalClaudeRecoveryBaselineMutation(t)
	cache, err := newRecoveryContentPathBaselineCache(
		[]pathMutation{action},
		journalTestCodecs(),
		journalTestFilesystem(),
	)
	if err != nil {
		t.Fatalf("newRecoveryContentPathBaselineCache: %v", err)
	}

	_, err = cache.capture(
		t.Context(),
		action,
		func(output.Destination) (string, error) { return hostPath, nil },
		nil,
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "exceeds 134217728 bytes") {
		t.Fatalf("capture error = %v, want bounded aggregate-document rejection", err)
	}
}

func TestObserveRecoveryContentPathRejectsOversizedAggregateDocument(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostPath := filepath.Join(directory, "config.json")
	file, err := os.OpenFile(hostPath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaximumRecoveryBackupFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	action := globalClaudeRecoveryBaselineMutation(t)

	observation := observeRecoveryPath(
		t.Context(),
		journalTestFilesystem(),
		action.Destination.String(),
		string(action.ContentPath),
		hostPath,
		action.AggregateContract,
		journalTestCodecs(),
	)
	if !observation.Exists || !strings.Contains(observation.Error, "exceeds 134217728 bytes") {
		t.Fatalf("observation = %#v, want bounded aggregate-document error", observation)
	}
}

func TestRecoveryContentPathBaselineRejectsPhysicalRootReplacement(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("create original parent: %v", err)
	}
	hostPath := filepath.Join(parent, "config.json")
	original := claudeGlobalConfig(t, claudeGlobalCanonicalEntry(t, "trusted"))
	if err := os.WriteFile(hostPath, original, 0o640); err != nil {
		t.Fatalf("write original config: %v", err)
	}
	root, destination, err := rootedpath.CaptureDestination(hostPath)
	if err != nil {
		t.Fatalf("capture original destination: %v", err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Errorf("close original root: %v", err)
		}
	})
	resolvedPath, err := destination.LexicalPath()
	if err != nil {
		t.Fatalf("read bound destination path: %v", err)
	}

	displaced := filepath.Join(filepath.Dir(parent), "displaced")
	if err := os.Rename(parent, displaced); err != nil {
		t.Fatalf("displace original parent: %v", err)
	}
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("replace original parent path: %v", err)
	}
	if err := os.WriteFile(hostPath, claudeGlobalConfig(t, claudeGlobalCanonicalEntry(t, "attacker")), 0o600); err != nil {
		t.Fatalf("write replacement config: %v", err)
	}

	action := globalClaudeRecoveryBaselineMutation(t)
	cache, err := newRecoveryContentPathBaselineCache(
		[]pathMutation{action},
		journalTestCodecs(),
		journalTestFilesystem(),
	)
	if err != nil {
		t.Fatalf("newRecoveryContentPathBaselineCache: %v", err)
	}
	_, err = cache.capture(
		t.Context(),
		action,
		func(output.Destination) (string, error) { return resolvedPath, nil },
		nil,
		func(output.Destination) (rootedpath.CommitCapability, bool, error) {
			capability, acquireErr := root.Acquire(destination)
			return capability, true, acquireErr
		},
	)
	if err == nil || !strings.Contains(err.Error(), "physical root binding changed") {
		t.Fatalf("capture error = %v, want physical-root replacement rejection", err)
	}
}

func TestRecoveryContentPathBaselineRejectsMismatchedResolverAndCapability(t *testing.T) {
	firstPath := filepath.Join(t.TempDir(), "first.json")
	secondPath := filepath.Join(t.TempDir(), "second.json")
	config := claudeGlobalConfig(t, claudeGlobalCanonicalEntry(t, "trusted"))
	for _, path := range []string{firstPath, secondPath} {
		if err := os.WriteFile(path, config, 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	firstRoot, firstDestination, err := rootedpath.CaptureDestination(firstPath)
	if err != nil {
		t.Fatalf("capture first destination: %v", err)
	}
	t.Cleanup(func() { _ = firstRoot.Close() })
	secondRoot, secondDestination, err := rootedpath.CaptureDestination(secondPath)
	if err != nil {
		t.Fatalf("capture second destination: %v", err)
	}
	t.Cleanup(func() { _ = secondRoot.Close() })
	firstResolved, err := firstDestination.LexicalPath()
	if err != nil {
		t.Fatalf("read first destination path: %v", err)
	}

	action := globalClaudeRecoveryBaselineMutation(t)
	cache, err := newRecoveryContentPathBaselineCache(
		[]pathMutation{action},
		journalTestCodecs(),
		journalTestFilesystem(),
	)
	if err != nil {
		t.Fatalf("newRecoveryContentPathBaselineCache: %v", err)
	}
	_, err = cache.capture(
		t.Context(),
		action,
		func(output.Destination) (string, error) { return firstResolved, nil },
		nil,
		func(output.Destination) (rootedpath.CommitCapability, bool, error) {
			capability, acquireErr := secondRoot.Acquire(secondDestination)
			return capability, true, acquireErr
		},
	)
	if err == nil || !strings.Contains(err.Error(), "does not match retained authority path") {
		t.Fatalf("capture error = %v, want resolver/capability mismatch", err)
	}
}

func TestAcquireMatchingRootedCapabilityRejectsNilPresentCapability(t *testing.T) {
	_, _, err := acquireMatchingRootedCapability(
		outputtest.Parse(t, "~/.claude.json"),
		filepath.Join(t.TempDir(), "config.json"),
		func(output.Destination) (rootedpath.CommitCapability, bool, error) {
			return nil, true, nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "nil retained root authority") {
		t.Fatalf("acquire error = %v, want nil-present rejection", err)
	}
}

func TestAcquireMatchingRootedCapabilityClosesContradictoryAbsentCapability(t *testing.T) {
	hostPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(hostPath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	root, destination, err := rootedpath.CaptureDestination(hostPath)
	if err != nil {
		t.Fatalf("capture destination: %v", err)
	}
	t.Cleanup(func() { _ = root.Close() })
	resolvedPath, err := destination.LexicalPath()
	if err != nil {
		t.Fatalf("read destination path: %v", err)
	}
	capability, err := root.Acquire(destination)
	if err != nil {
		t.Fatalf("acquire destination: %v", err)
	}

	_, _, err = acquireMatchingRootedCapability(
		outputtest.Parse(t, "~/.claude.json"),
		resolvedPath,
		func(output.Destination) (rootedpath.CommitCapability, bool, error) {
			return capability, false, nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "while reporting it absent") {
		t.Fatalf("acquire error = %v, want contradictory-absence rejection", err)
	}
	if err := capability.Validate(); err == nil {
		t.Fatal("contradictory absent capability remained open")
	}
}

func globalClaudeRecoveryBaselineMutation(t *testing.T) pathMutation {
	t.Helper()
	return claudeRecoveryBaselineMutation(t, target.ScopeGlobal, "context7")
}

func claudeGlobalCanonicalEntry(t *testing.T, command string) []byte {
	t.Helper()
	canonical, err := mcpcodec.CanonicalClaudeGlobalMCPServerEntry(mcpcodec.ClaudeGlobalMCPServerProjection{
		ServerID:        "context7",
		Command:         command,
		AdapterContract: aggregate.ClaudeGlobalMCPStdioAdapterV1,
	})
	if err != nil {
		t.Fatalf("canonical Claude global entry: %v", err)
	}
	return canonical
}

func claudeGlobalConfig(t *testing.T, canonical []byte) []byte {
	t.Helper()
	operations, ok := mcpcodec.ImplementedMCPPlacementOperationsForID(aggregate.MCPPlacementClaudeGlobal)
	if !ok {
		t.Fatal("Claude global MCP placement operations missing")
	}
	config, err := operations.MergeCanonicalEntry(nil, "context7", canonical)
	if err != nil {
		t.Fatalf("merge Claude global entry: %v", err)
	}
	return config
}
