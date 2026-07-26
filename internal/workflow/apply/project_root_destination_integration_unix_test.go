//go:build darwin || linux

package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestExecuteWithOptionsRejectsProjectAncestorAliasIntroducedAfterDisclosure(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	paths := applyTestPaths(t, root)
	sourcePath := filepath.Join(root, "skills", "oracle")
	writeApplyFile(t, filepath.Join(sourcePath, "SKILL.md"), "---\nname: oracle\ndescription: oracle\n---\n")
	writeApplyFile(t, paths.ManifestPath, `
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]
`)
	lockedSkill := snapshottest.ExactSupply(t, snapshottest.ExactSupplyInput{
		Kind:         entity.KindSkill,
		Name:         "oracle",
		SourceID:     "local:skills/oracle?mode=vendor",
		ArtifactKind: artifact.ArtifactKindDirectory,
		ContentHash:  artifact.ContentHash(hashApplyPath(t, sourcePath)),
	})
	placements, err := profile.ManagedPathPlacementsFor(
		entity.KindSkill,
		target.ScopeProject,
		[]target.Target{target.TargetCodex},
	)
	if err != nil || len(placements) != 1 {
		t.Fatalf("ManagedPathPlacementsFor = %#v, %v", placements, err)
	}
	destination, err := placements[0].ChildDestination("oracle")
	if err != nil {
		t.Fatal(err)
	}
	writeRoute, err := profile.ManagedPathOperationRoute(placements[0], profile.OperationWrite)
	if err != nil {
		t.Fatal(err)
	}
	removeRoute, err := profile.ManagedPathOperationRoute(placements[0], profile.OperationRemove)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := placements[0].Realize(destination, realization.PathProjectionCopy, writeRoute)
	if err != nil {
		t.Fatal(err)
	}
	projectionSubject, err := topologyprojection.Subject(lockedSkill.EntityID(), placements[0].ID())
	if err != nil {
		t.Fatal(err)
	}
	projection, err := lock.NewManagedPathSubjectContract(lock.ManagedPathSubjectInput{
		EntityID: lockedSkill.EntityID(), SubjectID: projectionSubject, Realization: spec,
		WriteRouteID: writeRoute.RouteID(), RemoveRouteID: removeRoute.RouteID(),
	})
	if err != nil {
		t.Fatal(err)
	}
	writeApplyLockfile(t, paths.LockfilePath, snapshottest.File(t, lockedSkill, projection))
	planned, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: paths.ManifestPath,
		LockfilePath: paths.LockfilePath,
	})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	defer planned.Close()

	if err := os.Symlink(outside, filepath.Join(root, ".agents")); err != nil {
		t.Fatalf("introduce project ancestor alias: %v", err)
	}
	_, err = ExecuteWithOptions(context.Background(), planned, ExecuteOptions{PlanWasDisclosed: true})
	var stale mutation.StalePlanError
	if !errors.As(err, &stale) || !hasRootedPathFailureKind(err, rootedpath.FailureAncestorSymlink) {
		t.Fatalf("ExecuteWithOptions error = %v, want stale plan with ancestor-symlink cause", err)
	}
	entries, readErr := os.ReadDir(outside)
	if readErr != nil {
		t.Fatalf("read outside directory: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("stale apply wrote through introduced alias: %v", entries)
	}
	for _, path := range []string{paths.StatefilePath, paths.RecoveryDir} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("%s stat error = %v, want absent", path, statErr)
		}
	}
}
