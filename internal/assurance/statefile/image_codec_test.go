package statefile

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	pathreferencecodec "github.com/isty2e/daem/internal/encoding/pathreference"
	"github.com/isty2e/daem/internal/target"
)

func TestStateImageWirePreservesFactsAndVersionBarrier(t *testing.T) {
	complete := testV9Snapshot(t)
	native, err := durable.NewSnapshot(durable.SnapshotInput{NativeDocumentCommit: 1<<63 + 7})
	if err != nil {
		t.Fatal(err)
	}
	nativeFacts, err := durable.NewSnapshot(durable.SnapshotInput{
		NativeDocumentCommit: 19,
		ManagedPaths:         []durable.ManagedPathState{nativeDocumentCodecBaseline(t, target.TargetPi, target.ScopeGlobal, 0o600)},
		ManagedAggregates:    []durable.ManagedAggregateState{nativeAggregateState(t, "settings")},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		snapshot durable.Snapshot
		identity pathauthority.Exact
	}{
		{name: "empty", snapshot: durable.EmptySnapshot(), identity: pathtest.Exact(filepath.Join(t.TempDir(), "empty.json"))},
		{name: "native only", snapshot: native, identity: pathtest.Exact(filepath.Join(t.TempDir(), "native.json"))},
		{name: "native path and aggregate", snapshot: nativeFacts, identity: pathtest.Exact(filepath.Join(t.TempDir(), "native-facts.json"))},
		{name: "every fact family", snapshot: complete, identity: complete.PendingCarrierInstalls()[0].Owner().StatefileAuthority()},
	} {
		t.Run(test.name, func(t *testing.T) {
			reference, err := pathauthority.NewReference(test.identity, filepath.Join(t.TempDir(), "SelectedState.json"))
			if err != nil {
				t.Fatal(err)
			}
			image, err := NewStateImage(test.snapshot, reference)
			if err != nil {
				t.Fatal(err)
			}
			legacy, err := Marshal(test.snapshot)
			if err != nil {
				t.Fatal(err)
			}
			content, err := MarshalImage(image)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := UnmarshalImage(content)
			if err != nil || !loaded.Snapshot().Equal(test.snapshot) || !loaded.Reference().Equal(reference) {
				t.Fatal("state image lost a fact or access spelling", err)
			}
			reencoded, err := MarshalImage(loaded)
			if err != nil || !bytes.Equal(reencoded, content) {
				t.Fatal("state image encoding is not canonical", err)
			}
			legacyAfter, err := Marshal(loaded.Snapshot())
			if err != nil || !bytes.Equal(legacyAfter, legacy) {
				t.Fatal("state image changed default format bytes", err)
			}
			if _, err := Decode(content); err == nil {
				t.Fatal("default decoder silently adopted state images")
			}
			if _, err := UnmarshalImage(legacy); err == nil {
				t.Fatal("state image decoder invented missing legacy evidence")
			}
		})
	}
}

func TestStateImageWireRequiresCompleteStrictEvidence(t *testing.T) {
	reference, err := pathauthority.NewReference(pathtest.Exact(filepath.Join(t.TempDir(), "state.json")), filepath.Join(t.TempDir(), "SelectedState.json"))
	if err != nil {
		t.Fatal(err)
	}
	image, err := NewStateImage(durable.EmptySnapshot(), reference)
	if err != nil {
		t.Fatal(err)
	}
	content, err := MarshalImage(image)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		t.Fatal(err)
	}
	var referenceFields map[string]json.RawMessage
	if err := json.Unmarshal(fields["statefile_reference"], &referenceFields); err != nil {
		t.Fatal(err)
	}
	delete(referenceFields, "locator")
	incompleteReference, err := json.Marshal(referenceFields)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		field string
		value json.RawMessage
	}{
		{name: "missing reference", field: "statefile_reference"},
		{name: "null reference", field: "statefile_reference", value: json.RawMessage("null")},
		{name: "missing locator", field: "statefile_reference", value: incompleteReference},
		{name: "missing fact family", field: "managed_paths"},
		{name: "unknown field", field: "unowned", value: json.RawMessage("true")},
		{name: "invalid native counter", field: "native_document_commit", value: json.RawMessage("-1")},
	} {
		t.Run(test.name, func(t *testing.T) {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(content, &fields); err != nil {
				t.Fatal(err)
			}
			if test.value == nil {
				delete(fields, test.field)
			} else {
				fields[test.field] = test.value
			}
			malformed, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := UnmarshalImage(malformed); err == nil {
				t.Fatal("malformed state image was admitted")
			}
		})
	}
	duplicate := bytes.Replace(content, []byte(`"version": 11`), []byte(`"version": 11, "version": 11`), 1)
	if _, err := UnmarshalImage(duplicate); err == nil {
		t.Fatal("duplicate JSON field was admitted")
	}
	if content, err := MarshalImage(StateImage{}); err == nil || content != nil {
		t.Fatal("uninitialized image produced state metadata")
	}
	if _, err := UnmarshalImage(bytes.Repeat([]byte(" "), int(maximumStatefileBytes)+1)); err == nil {
		t.Fatal("oversized state image was admitted")
	}
}

func assertStateImageWireRejectsReference(t *testing.T, content []byte, reference pathauthority.Reference) {
	t.Helper()
	var record stateImageRecord
	if err := json.Unmarshal(content, &record); err != nil {
		t.Fatal(err)
	}
	encoded, err := pathreferencecodec.Encode(reference)
	if err != nil {
		t.Fatal(err)
	}
	record.StatefileReference = encoded
	malformed, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UnmarshalImage(malformed); err == nil {
		t.Fatal("wire reference granted a foreign owner")
	}
}
