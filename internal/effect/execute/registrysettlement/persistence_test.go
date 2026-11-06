package registrysettlement

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/test/outputtest"
)

func TestPersistenceSnapshotUsesFrozenDocumentScopeNotCompleteRegistryEquality(t *testing.T) {
	root := t.TempDir()
	owner, err := stateauthority.New(pathtest.DarwinCaseSensitive(filepath.Join(root, "state")), filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	selected, err := ownership.NewDocumentBinding(owner, outputtest.Parse(t, ".pi/settings.json"), pathtest.DarwinCaseSensitive(filepath.Join(root, "selected.json")))
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := ownership.NewDocumentBinding(owner, outputtest.Parse(t, "@pi-agent/settings.json"), pathtest.DarwinCaseSensitive(filepath.Join(root, "foreign.json")))
	if err != nil {
		t.Fatal(err)
	}
	scope, err := ownership.NewDocumentScope([]pathauthority.Exact{selected.PathAuthority()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	before, err := ownership.NewRegistry(nil, selected, foreign)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewPersistenceSnapshot(durable.EmptySnapshot(), scope, before)
	if err != nil {
		t.Fatal(err)
	}
	outsideChanged, err := ownership.NewRegistry(nil, selected)
	if err != nil {
		t.Fatal(err)
	}
	if before.Equal(outsideChanged) {
		t.Fatal("fixture did not change the complete Registry")
	}
	if matches, err := snapshot.Matches(durable.EmptySnapshot(), outsideChanged); err != nil || !matches {
		t.Fatalf("unrelated document change invalidated the frozen scope: %v", err)
	}
	if matches, err := snapshot.MatchesRegistry(ownership.EmptyRegistry()); err != nil || matches {
		t.Fatalf("selected document removal disappeared from the scope: matches=%v error=%v", matches, err)
	}
	if matches, err := snapshot.MatchesRegistry(ownership.Registry{}); err == nil || matches {
		t.Fatal("unqualified Registry became matching evidence")
	}
}

func TestPersistenceSnapshotRefusesUnqualifiedViews(t *testing.T) {
	state := durable.EmptySnapshot()
	if _, err := NewPersistenceSnapshot(state, ownership.DocumentScope{}, ownership.EmptyRegistry()); err == nil {
		t.Fatal("unqualified scope became persistence evidence")
	}
	scope, err := ownership.NewDocumentScope(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewPersistenceSnapshot(state, scope, ownership.Registry{}); err == nil {
		t.Fatal("unqualified Registry became persistence evidence")
	}
}
