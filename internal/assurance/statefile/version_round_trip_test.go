package statefile

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
)

func TestSupportedStateVersionsPreservePopulatedFactFamilies(t *testing.T) {
	snapshot := testV9Snapshot(t)
	for _, version := range []int{8, 9, 10} {
		content := stateVersionFixture(t, snapshot, version)
		decoded, err := Decode(content)
		if err != nil || !snapshot.Equal(decoded) {
			t.Fatalf("v%d changed populated durable facts: %v", version, err)
		}
		current, err := Marshal(decoded)
		if err != nil {
			t.Fatal(err)
		}
		var header struct {
			Version int `json:"version"`
		}
		if err := json.Unmarshal(current, &header); err != nil || header.Version != 10 {
			t.Fatalf("v%d was not published as current state: %d, %v", version, header.Version, err)
		}
		roundTrip, err := Decode(current)
		if err != nil || !snapshot.Equal(roundTrip) {
			t.Fatalf("v%d lost facts on current publication: %v", version, err)
		}
	}
}

func TestLoadingLegacyStateDoesNotRewriteIt(t *testing.T) {
	populated := testV9Snapshot(t)
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedPaths: populated.ManagedPaths(), ManagedAggregates: populated.ManagedAggregates(),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []int{8, 9} {
		content := stateVersionFixture(t, snapshot, version)
		path := filepath.Join(t.TempDir(), "state.json")
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		loaded, err := Load(t.Context(), path)
		if err != nil || !snapshot.Equal(loaded) {
			t.Fatalf("v%d could not be loaded: %v", version, err)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(content, after) {
			t.Fatalf("v%d load rewrote the statefile: %v", version, err)
		}
	}
}

func stateVersionFixture(t testing.TB, snapshot durable.Snapshot, version int) []byte {
	t.Helper()
	if version == 10 {
		content, err := json.Marshal(persistedSnapshot(snapshot))
		if err != nil {
			t.Fatal(err)
		}
		return content
	}
	legacy := legacySnapshotForTest(t, snapshot, version)
	if version == 8 {
		for index := range legacy.DelegateAttempts {
			legacy.DelegateAttempts[index].AttemptObserved = nil
			legacy.DelegateAttempts[index].ProcessReason = ""
		}
	}
	content, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	return content
}
