package statefile

import (
	"encoding/json"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
)

func TestV10AggregateRowsPreserveNativeKeyIdentity(t *testing.T) {
	seen := make(map[string]bool)
	cases := [][]string{
		{""},
		{"/"},
		{"~1"},
		{"a/b"},
		{"a", "b"},
		{"a~1b"},
		{"."},
		{".."},
		{"line\n\x00key"},
		{"한글", "0"},
	}
	cases = append(cases, make([]string, 300))
	for _, keys := range cases {
		state := nativeAggregateState(t, keys...)
		snapshot, err := durable.NewSnapshot(durable.SnapshotInput{ManagedAggregates: []durable.ManagedAggregateState{state}})
		if err != nil {
			t.Fatal(err)
		}
		record := persistedSnapshot(snapshot).ManagedAggregateBaselines[0]
		encoded, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		var decodedRecord managedAggregateV10DTO
		if err := json.Unmarshal(encoded, &decodedRecord); err != nil {
			t.Fatal(err)
		}
		decoded, err := decodedRecord.canonical()
		if err != nil || !state.Equal(decoded) {
			t.Fatalf("native row changed identity or facts: %#v, %v", keys, err)
		}
		if seen[decodedRecord.KeyPath] {
			t.Fatal("distinct native keys collapsed in the v10 row")
		}
		seen[decodedRecord.KeyPath] = true
	}
}

func TestV10RowRefusesRootAndMalformedKeyPointers(t *testing.T) {
	state := nativeAggregateState(t, "ordinary")
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{ManagedAggregates: []durable.ManagedAggregateState{state}})
	if err != nil {
		t.Fatal(err)
	}
	record := persistedSnapshot(snapshot).ManagedAggregateBaselines[0]
	for _, invalid := range []string{"", "a/b", "/a~2", "/a~"} {
		candidate := record
		candidate.KeyPath = invalid
		if _, err := candidate.canonical(); err == nil {
			t.Fatalf("v10 row admitted invalid ownership pointer %q", invalid)
		}
	}
}

func TestStateVersionDispatchRejectsWrongScopeFieldGrammar(t *testing.T) {
	snapshot := testV9Snapshot(t)
	for _, version := range []int{8, 9, 10} {
		encoded := stateVersionFixture(t, snapshot, version)
		if _, err := Decode(encoded); err != nil {
			t.Fatalf("valid v%d fixture was rejected before mutation: %v", version, err)
		}
		var err error
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &raw); err != nil {
			t.Fatal(err)
		}
		var rows []map[string]json.RawMessage
		if err := json.Unmarshal(raw["managed_aggregate_contributions"], &rows); err != nil || len(rows) == 0 {
			t.Fatalf("aggregate fixture = %#v, %v", rows, err)
		}
		for _, both := range []bool{false, true} {
			candidate := make(map[string]json.RawMessage, len(rows[0])+1)
			for key, value := range rows[0] {
				candidate[key] = value
			}
			if _, modern := candidate["key_path"]; modern {
				candidate["content_path"] = candidate["key_path"]
				if !both {
					delete(candidate, "key_path")
				}
			} else {
				candidate["key_path"] = candidate["content_path"]
				if !both {
					delete(candidate, "content_path")
				}
			}
			raw["managed_aggregate_contributions"], err = json.Marshal([]map[string]json.RawMessage{candidate})
			if err != nil {
				t.Fatal(err)
			}
			tampered, err := json.Marshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Decode(tampered); err == nil {
				t.Fatal("versioned reader admitted the other format's scope field grammar")
			}
		}
	}
}
