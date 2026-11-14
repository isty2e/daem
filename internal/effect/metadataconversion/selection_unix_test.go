//go:build darwin || linux

package metadataconversion

import (
	"encoding/json"
	"testing"
)

func TestSelectionRejectsMalformedOrAmbiguousRecords(t *testing.T) {
	fixture := compilerFixture(t, 1)
	plan := compileFixture(t, fixture.paths)
	valid, err := plan.selection.encode()
	if err != nil {
		t.Fatal(err)
	}
	changes := map[string]func(*selectionRecord){
		"version":            func(record *selectionRecord) { record.Version++ },
		"kind":               func(record *selectionRecord) { record.Kind = "another-operation" },
		"missing references": func(record *selectionRecord) { record.PathReferences = nil },
		"duplicate reference": func(record *selectionRecord) {
			record.PathReferences = append(record.PathReferences, record.PathReferences[0])
		},
		"missing barriers": func(record *selectionRecord) { record.BarrierStateDirs = nil },
		"empty barriers":   func(record *selectionRecord) { record.BarrierStateDirs = []string{} },
		"duplicate barrier": func(record *selectionRecord) {
			record.BarrierStateDirs = append(record.BarrierStateDirs, record.BarrierStateDirs[0])
		},
		"relative selection": func(record *selectionRecord) { record.StatefilePath = "state.json" },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			var record selectionRecord
			if err := json.Unmarshal(valid, &record); err != nil {
				t.Fatal(err)
			}
			change(&record)
			content, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeSelection(content); err == nil {
				t.Fatal("malformed selection was admitted")
			}
		})
	}
	for _, content := range [][]byte{
		append(append([]byte(nil), valid...), []byte(` {"version":1}`)...),
		append([]byte(`{"unknown":true,`), valid[1:]...),
		append([]byte(`{"version":1,`), valid[1:]...),
	} {
		if _, err := decodeSelection(content); err == nil {
			t.Fatal("trailing, unknown or duplicate selection fields were admitted")
		}
	}
}
