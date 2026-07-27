package testkit

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/lockfile"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func WriteHookManifestAndLock(t *testing.T, root string, content string) {
	t.Helper()
	WriteFile(t, root, "daem.toml", content)
	manifestPath := filepath.Join(root, "daem.toml")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("lock exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
}

func HookAggregateStatesFromLock(t *testing.T, path string) []durable.ManagedAggregateState {
	t.Helper()
	locked, err := lockfile.Load(path)
	if err != nil {
		t.Fatalf("load Hook lockfile: %v", err)
	}
	states := make([]durable.ManagedAggregateState, 0)
	for _, contract := range locked.Locked.Subjects() {
		if contract.EntityID().Kind() != entity.KindHook {
			continue
		}
		realization, realized := contract.Realization()
		if !realized {
			continue
		}
		contribution, aggregateRealization := realization.ManagedAggregateContribution()
		if !aggregateRealization {
			continue
		}
		state, err := durable.NewManagedAggregateState(contract.SubjectID(), contribution)
		if err != nil {
			t.Fatalf("construct Hook aggregate state: %v", err)
		}
		states = append(states, state)
	}
	return states
}

func WriteHookAggregateStateFromLock(t *testing.T, root string) {
	t.Helper()
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedAggregates: HookAggregateStatesFromLock(t, filepath.Join(root, "daem.lock.toml")),
	})
	if err != nil {
		t.Fatalf("durable.NewSnapshot returned error: %v", err)
	}
	WriteStatefile(t, filepath.Join(root, ".daem", "state.json"), snapshot)
}

func AssertHookAggregateState(
	t *testing.T,
	file durable.Snapshot,
	name string,
	targetValue string,
	scopeValue string,
	destination string,
) {
	t.Helper()
	for _, aggregateState := range file.ManagedAggregates() {
		entityID, entityBacked := topologyprojection.EntityID(aggregateState.Subject())
		if !entityBacked || entityID.Kind() != entity.KindHook || entityID.Name() != name {
			continue
		}
		contribution := aggregateState.Contribution()
		if string(contribution.Target()) != targetValue ||
			string(contribution.Scope()) != scopeValue || contribution.AggregateRoot().String() != destination ||
			contribution.ContentPath() != "/hooks" {
			t.Fatalf("Hook aggregate state = %#v", aggregateState)
		}
		return
	}
	t.Fatalf("Hook aggregate state %s/%s/%s %q not found in %#v", name, targetValue, scopeValue, destination, file.ManagedAggregates())
}

func AssertHookAggregateStateMissing(
	t *testing.T,
	file durable.Snapshot,
	name string,
	targetValue string,
	scopeValue string,
) {
	t.Helper()
	for _, aggregateState := range file.ManagedAggregates() {
		entityID, entityBacked := topologyprojection.EntityID(aggregateState.Subject())
		if !entityBacked || entityID.Kind() != entity.KindHook || entityID.Name() != name {
			continue
		}
		contribution := aggregateState.Contribution()
		if string(contribution.Target()) == targetValue && string(contribution.Scope()) == scopeValue {
			t.Fatalf("Hook aggregate state unexpectedly found in %#v", file.ManagedAggregates())
		}
	}
}
