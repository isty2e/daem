package qualification_test

import (
	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func testAppliedState(selectedTarget target.Target, contentHash string) durable.ManagedPathState {
	destination, placement := "AGENTS.md", "instructions.project.agents"
	consumer := target.TargetCodex
	if selectedTarget == target.TargetClaudeCode {
		destination, placement, consumer = "CLAUDE.md", "instructions.project.claude", target.TargetClaudeCode
	}
	id, err := entity.New(entity.KindInstructions, "shared")
	if err != nil {
		panic(err)
	}
	subject, err := topologyprojection.Subject(id, placement)
	if err != nil {
		panic(err)
	}
	path, err := output.Parse(destination)
	if err != nil {
		panic(err)
	}
	state, err := durable.NewManagedPathState(subject, []target.Target{consumer}, target.ScopeProject, path, testContentHash(contentHash), realization.PathProjectionFile, realization.PathPermissionsExecutableClass, 0)
	if err != nil {
		panic(err)
	}
	return state
}

func statefileFor(resources ...durable.ManagedPathState) durable.Snapshot {
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedPaths: append([]durable.ManagedPathState(nil), resources...),
	})
	if err != nil {
		panic(err)
	}
	return snapshot
}
