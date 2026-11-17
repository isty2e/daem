package qualification_test

import "github.com/isty2e/daem/internal/assurance/durable"

func statefileFor(resources ...durable.ManagedPathState) durable.Snapshot {
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedPaths: append([]durable.ManagedPathState(nil), resources...),
	})
	if err != nil {
		panic(err)
	}
	return snapshot
}
