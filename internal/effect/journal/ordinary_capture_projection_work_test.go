package journal

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/target"
)

func TestOrdinaryProjectionCaptureChargesOneReadAndOnlySelectedBackupBytes(t *testing.T) {
	projection, err := mcpcodec.CanonicalClaudeGlobalMCPServerEntry(mcpcodec.ClaudeGlobalMCPServerProjection{
		ServerID: "context7", Command: "npx", Args: []string{"-y", "@upstream/context7"},
		AdapterContract: aggregate.ClaudeGlobalMCPStdioEnvAdapterV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
		Foreign string                     `json:"foreign"`
	}{Servers: map[string]json.RawMessage{"context7": projection}, Foreign: strings.Repeat("unmanaged", 1024)})
	if err != nil {
		t.Fatal(err)
	}
	rootPath, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(rootPath, ".claude.json")
	requireWholeGroupWrite(t, host, content, 0o640)
	root := nativeNamespaceRoot(t, rootPath).Root
	budget := imageBudget(t)
	remaining := int64(len(content) + len(projection))
	consume, err := workmeasure.NewArtifactWork(0, budget.RemainingBytes()-remaining)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.AdmitTree(consume); err != nil {
		t.Fatal(err)
	}
	authority, err := root.AuthorityBounded(budget)
	if err != nil {
		t.Fatal(err)
	}
	relative, err := rootedpath.NewRelativeDestination(".claude.json")
	if err != nil {
		t.Fatal(err)
	}
	bound, err := authority.Bind(relative)
	if err != nil {
		t.Fatal(err)
	}
	work := &ordinaryCaptureWork{budget: budget}
	action := claudeRecoveryBaselineMutation(t, target.ScopeGlobal, "context7")
	cache, err := newRecoveryContentPathBaselineCache([]captureMutation{action}, journalTestCodecs(), ordinaryCaptureReader{Reader: journalTestFilesystem(), work: work})
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(output.Destination) (string, error) { return host, nil }
	acquire := work.rootedResolver(func(output.Destination, rootedpath.PhysicalTraversalBudget) (rootedpath.CommitCapability, bool, error) {
		capability, err := root.AcquireBounded(bound, physicalwork.MaximumPhysicalPathDepth, budget)
		return capability, true, err
	})
	for range 2 {
		baseline, err := cache.capture(t.Context(), action, resolve, nil, acquire)
		if err != nil {
			t.Fatal(err)
		}
		selected, present, err := baseline.projection(action.ContentPath())
		if err != nil || !present || !bytes.Equal(selected, projection) {
			t.Fatalf("selected backup lost its canonical projection: %s %v", selected, err)
		}
	}
	backup := filepath.Join(t.TempDir(), "files", "000001")
	if err := work.writeFile(backup, projection, 0o600); err != nil {
		t.Fatal("capture charged the whole document twice rather than its selected backup", err)
	}
	if budget.RemainingBytes() != 0 {
		t.Fatal("capture omitted read or backup-write work")
	}
	actual, err := os.ReadFile(host)
	if err != nil || !bytes.Equal(actual, content) {
		t.Fatal("capture changed the source document", err)
	}
}
