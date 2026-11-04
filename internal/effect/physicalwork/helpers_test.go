package physicalwork

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

func artifactWork(t *testing.T, entries int, bytes int64) workmeasure.ArtifactWork {
	t.Helper()
	work, err := workmeasure.NewArtifactWork(entries, bytes)
	if err != nil {
		t.Fatal(err)
	}
	return work
}

func captureBudgetDemands(t *testing.T) recovery.RemovalDemandSet {
	t.Helper()
	destination, err := output.Parse("skills/runner")
	if err != nil {
		t.Fatal(err)
	}
	state, err := recovery.NewBeforeRemovalState(recovery.BeforePathState{
		Existed: true, PathExisted: true, ParentExisted: true,
		PathMode: recovery.NewPermissionMode(0o600), Kind: recovery.PathKindFile,
		ContentHash: "sha256:" + strings.Repeat("3", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	demand, err := recovery.NewRemovalDemand(target.ScopeProject, destination, []recovery.RemovalState{state})
	if err != nil {
		t.Fatal(err)
	}
	set, err := recovery.NewRemovalDemandSet([]recovery.RemovalDemand{demand})
	if err != nil {
		t.Fatal(err)
	}
	return set
}
