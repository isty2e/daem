package aggregate

import (
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestBlockedNativeStateRequiresCorrelatedPresentAncestor(t *testing.T) {
	parent := blockedReadContract(t, "a")
	child := blockedReadContract(t, "a", "b")
	ancestor := parent.Address().KeyPath()
	blocked, err := NewBlockedProjectionState(child, ancestor)
	if err != nil {
		t.Fatal(err)
	}
	got, present := blocked.BlockedBy()
	if !present || got != ancestor || blocked.Present() || blocked.CanonicalProjection() != "" {
		t.Fatal("blocked read lost its distinct resolution or invented a selected value")
	}
	missing, err := NewProjectionState(child, true, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Equal(missing) || blocked.Absent() || !missing.Absent() {
		t.Fatal("blocked traversal collapsed into plain absence")
	}
	parentState, err := NewProjectionState(parent, true, true, "ancestor-value")
	if err != nil {
		t.Fatal(err)
	}
	selection, err := NewSelection([]ProjectionContract{parent, child})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewSnapshot(true, selection, []ProjectionState{blocked, parentState})
	if err != nil {
		t.Fatal(err)
	}
	observed, covered := snapshot.State(child)
	if !covered || !observed.Equal(blocked) {
		t.Fatal("snapshot dropped blocked-read identity")
	}
	states := snapshot.States()
	states[0] = ProjectionState{}
	observed, covered = snapshot.State(child)
	if !covered || !observed.Equal(blocked) {
		t.Fatal("snapshot exposed mutable state storage")
	}
	missingSnapshot, err := NewSnapshot(true, selection, []ProjectionState{missing, parentState})
	if err != nil || snapshot.Equal(missingSnapshot) {
		t.Fatal("snapshot equality omitted traversal resolution")
	}
	if _, err := NewRestoredDocumentWithExpected(ExistingDocument([]byte(`{}`)), snapshot, missingSnapshot); err == nil {
		t.Fatal("recovery equated blocked and missing selected state")
	}

	childOnly, err := NewSelection([]ProjectionContract{child})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewSnapshot(true, childOnly, []ProjectionState{blocked}); err == nil {
		t.Fatal("unselected blocker acquired correlated snapshot evidence")
	}
	absentParent, err := NewProjectionState(parent, true, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewSnapshot(true, selection, []ProjectionState{absentParent, blocked}); err == nil {
		t.Fatal("absent ancestor explained a traversal obstruction")
	}
	if _, err := NewSnapshot(false, selection, []ProjectionState{parentState, blocked}); err == nil {
		t.Fatal("absent document carried blocked/present read facts")
	}
}

func TestBlockedNativeStatePreservesLiteralKeyAncestry(t *testing.T) {
	for _, name := range []string{"", "a/b", "~1", ".", "..", "한글", "control\n\x00"} {
		parent := blockedReadContract(t, name)
		child := blockedReadContract(t, name, "child")
		if _, err := NewBlockedProjectionState(child, parent.Address().KeyPath()); err != nil {
			t.Fatalf("literal native ancestry was refused: %q: %v", name, err)
		}
		if _, err := NewBlockedProjectionState(child, child.Address().KeyPath()); err == nil {
			t.Fatal("selected key explained its own obstruction")
		}
	}
	child := blockedReadContract(t, "a!", "b")
	for _, ancestor := range []output.KeyPath{{}, blockedReadContract(t, "a").Address().KeyPath(), blockedReadContract(t, "other").Address().KeyPath()} {
		if _, err := NewBlockedProjectionState(child, ancestor); err == nil {
			t.Fatal("root, sibling or lexical prefix became a blocking ancestor")
		}
	}
	placement, admitted := MCPPlacementForID(MCPPlacementClaudeProject)
	if !admitted {
		t.Fatal("Claude MCP placement unavailable")
	}
	legacy, err := placement.ProjectionContract("server")
	if err != nil {
		t.Fatal(err)
	}
	ancestor, err := output.NewKeyPath("mcpServers")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewBlockedProjectionState(legacy, ancestor); err == nil {
		t.Fatal("native read resolution silently broadened the legacy codec protocol")
	}
}

func blockedReadContract(t testing.TB, segments ...string) ProjectionContract {
	t.Helper()
	contract, err := NewHostConfigProjectionContract(target.TargetPi, target.ScopeGlobal, outputtest.Parse(t, "@pi-agent/settings.json"), outputtest.KeyFootprint(t, segments...), "native-json-v1")
	if err != nil {
		t.Fatal(err)
	}
	return contract
}
