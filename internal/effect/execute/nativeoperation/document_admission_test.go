package nativeoperation

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestIndependentDocumentRouteRecheckRejectsNewAlias(t *testing.T) {
	root := t.TempDir()
	write, err := output.Parse("writer.toml")
	if err != nil {
		t.Fatal(err)
	}
	peer, err := output.Parse("peer.toml")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []output.Destination{write, peer} {
		if err := os.WriteFile(filepath.Join(root, path.RelativePath()), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	admission := ForwardDocumentAdmission{
		resolver: func(path output.Destination, _ rootedpath.PhysicalTraversalBudget) (string, error) {
			return filepath.Join(root, path.RelativePath()), nil
		},
		groups: map[output.Destination][]documentRouteCheck{
			write: {{writer: documentMember{route: documentRoute{path: write}}, peer: documentMember{route: documentRoute{path: peer}}}},
		},
	}
	budget := physicalwork.NewCaptureBudget(nil)
	if err := admission.recheck(t.Context(), budget, write); err != nil {
		t.Fatal("separate paths were refused", err)
	}
	peerPath := filepath.Join(root, peer.RelativePath())
	if err := os.Remove(peerPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(write.RelativePath(), peerPath); err != nil {
		t.Skipf("symlinks not available: %v", err)
	}
	var refusal *DocumentAdmissionError
	if err := admission.recheck(t.Context(), budget, write); !errors.As(err, &refusal) || refusal.Unverified() {
		t.Fatal("confirmed route overlap was not refused", err)
	}
}

func TestDocumentComparisonUsesNativeRootOutsideSelection(t *testing.T) {
	workspace, home, custom := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", custom)
	global, err := output.Parse("~/.claude/prefs.json")
	if err != nil {
		t.Fatal(err)
	}
	peer, err := aggregate.NewDocumentAddress(target.TargetClaudeCode, target.ScopeGlobal, global)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := output.Parse("writer.json")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(workspace, selected.RelativePath())
	if err := os.WriteFile(destination, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(destination, filepath.Join(custom, "prefs.json")); err != nil {
		t.Skipf("symlinks not available: %v", err)
	}
	observer := newDocumentRouteObservation(workspace, func(path output.Destination, _ rootedpath.PhysicalTraversalBudget) (string, error) {
		return filepath.Join(workspace, path.RelativePath()), nil
	}, physicalwork.NewCaptureBudget(nil))
	if err := compareSeparateRoutes(observer.fact(documentRoute{path: selected}), observer.fact(nativeAggregateRoute(peer)), false); !errors.Is(err, overlappingDocumentRoutes) {
		t.Fatal("retained document was not observed at the selected native root", err)
	}
}
