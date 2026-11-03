package statefile

import (
	"encoding/json"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/output"
)

type legacySnapshotDTO = snapshotDocumentDTO[legacyManagedAggregateDTO]

// legacySnapshotForTest projects populated fixtures into the frozen v8/v9 row
// grammar. Retagging current bytes would retain the wrong key-path field.
func legacySnapshotForTest(t testing.TB, snapshot durable.Snapshot, version int) legacySnapshotDTO {
	t.Helper()
	current := persistedSnapshot(snapshot)
	encoded, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	var legacy legacySnapshotDTO
	if err := json.Unmarshal(encoded, &legacy); err != nil {
		t.Fatal(err)
	}
	legacy.Version = version

	for index, record := range current.ManagedAggregateBaselines {
		key, err := output.ParseKeyPath(record.KeyPath)
		if err != nil {
			t.Fatal(err)
		}
		footprint, err := output.NewKeyFootprint(key)
		if err != nil {
			t.Fatal(err)
		}
		path, err := footprint.LegacyContentPath()
		if err != nil {
			t.Fatal(err)
		}
		legacy.ManagedAggregateBaselines[index].ContentPath = string(path)
	}
	return legacy
}
