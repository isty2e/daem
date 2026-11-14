package carrierclaim

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	pathreferencecodec "github.com/isty2e/daem/internal/encoding/pathreference"
)

func TestCarrierRegistryImageCompleteOwnersAndWireRoundTrip(t *testing.T) {
	image, expected := testCarrierRegistryImage(t)
	if len(image.References()) != len(expected) || len(image.Registry().Claims()) != 3 {
		t.Fatal("multiple claims lost their distinct owners")
	}
	content, err := MarshalImage(image)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := UnmarshalImage(content)
	if err != nil || !loaded.Registry().Equal(image.Registry()) {
		t.Fatal("carrier facts or pending pin reservation changed", err)
	}
	for _, reference := range expected {
		actual, present := loaded.Reference(reference.Identity())
		if !present || !actual.Equal(reference) {
			t.Fatal("carrier owner lost its independent locator")
		}
	}
	reencoded, err := MarshalImage(loaded)
	if err != nil || !bytes.Equal(reencoded, content) {
		t.Fatal("carrier image output is not canonical", err)
	}
	legacy, err := Marshal(image.Registry())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decode(content); err == nil {
		t.Fatal("default reader silently adopted the image format")
	}
	if _, err := UnmarshalImage(legacy); err == nil {
		t.Fatal("image reader invented legacy locators")
	}
	legacyAfter, err := Marshal(loaded.Registry())
	if err != nil || !bytes.Equal(legacyAfter, legacy) {
		t.Fatal("carrier image changed default format bytes", err)
	}

	if _, err := NewRegistryImage(image.Registry(), expected[:1]); err == nil {
		t.Fatal("missing owner reference was admitted")
	}
	conflicting, err := pathauthority.NewReference(expected[0].Identity(), filepath.Join(t.TempDir(), "ConflictingState.json"))
	if err != nil {
		t.Fatal(err)
	}
	unrelated, err := pathauthority.NewReference(pathtest.Exact(filepath.Join(t.TempDir(), "unrelated.json")), conflicting.Locator())
	if err != nil {
		t.Fatal(err)
	}
	for _, extra := range []pathauthority.Reference{conflicting, unrelated, {}} {
		values := append(append([]pathauthority.Reference(nil), expected...), extra)
		if _, err := NewRegistryImage(image.Registry(), values); err == nil {
			t.Fatal("conflicting, unrelated or invalid reference was admitted")
		}
	}
	values := append(append([]pathauthority.Reference(nil), expected...), expected[0])
	merged, err := NewRegistryImage(image.Registry(), values)
	if err != nil || len(merged.References()) != len(expected) {
		t.Fatal("builder did not merge identical evidence", err)
	}
	values[0] = pathauthority.Reference{}
	output := merged.References()
	output[0] = pathauthority.Reference{}
	claims := merged.Registry().Claims()
	claims[0] = durablecarrier.ManagedCarrierClaim{}
	if !merged.Registry().Equal(image.Registry()) {
		t.Fatal("caller mutated image claims")
	}
	for _, reference := range expected {
		actual, present := merged.Reference(reference.Identity())
		if !present || !actual.Equal(reference) {
			t.Fatal("caller mutated image references")
		}
	}
	if _, err := MarshalImage(RegistryImage{}); err == nil {
		t.Fatal("uninitialized carrier image produced metadata")
	}
	empty, err := NewRegistryImage(durablecarrier.EmptyGlobalCarrierClaims(), nil)
	if err != nil {
		t.Fatal(err)
	}
	emptyContent, err := MarshalImage(empty)
	if err != nil {
		t.Fatal(err)
	}
	if string(emptyContent) != "{\n  \"version\": 3,\n  \"claims\": [],\n  \"path_references\": []\n}\n" {
		t.Fatalf("empty version3 bytes changed: %s", emptyContent)
	}
	emptyLegacy, err := Marshal(empty.Registry())
	if err != nil || string(emptyLegacy) != "{\n  \"version\": 2,\n  \"claims\": []\n}\n" {
		t.Fatal("empty version2 bytes changed", err)
	}
	if loaded, err := UnmarshalImage(emptyContent); err != nil || len(loaded.Registry().Claims()) != 0 || len(loaded.References()) != 0 {
		t.Fatal("explicit empty image did not round trip", err)
	}
}

func TestCarrierRegistryImageWireAdmission(t *testing.T) {
	image, _ := testCarrierRegistryImage(t)
	content, err := MarshalImage(image)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		field string
		value json.RawMessage
	}{
		{name: "missing refs", field: "path_references"},
		{name: "null refs", field: "path_references", value: json.RawMessage("null")},
		{name: "missing owner refs", field: "path_references", value: json.RawMessage("[]")},
		{name: "null ref", field: "path_references", value: json.RawMessage("[null]")},
		{name: "missing claims", field: "claims"},
		{name: "unknown field", field: "unowned", value: json.RawMessage("true")},
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
				t.Fatal("malformed carrier image was admitted")
			}
		})
	}
	var record registryImageRecord
	if err := json.Unmarshal(content, &record); err != nil {
		t.Fatal(err)
	}
	baseRows := record.PathReferences
	conflicting := baseRows[0]
	conflicting.Locator = filepath.Join(t.TempDir(), "OtherState.json")
	unrelated := pathreferencecodec.Record{Key: filepath.Join(t.TempDir(), "unrelated.json"), SemanticsWitness: "exact-v1:", Locator: conflicting.Locator}
	for _, row := range []pathreferencecodec.Record{baseRows[0], conflicting, unrelated} {
		record.PathReferences = append(append([]pathreferencecodec.Record(nil), baseRows...), row)
		malformed, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := UnmarshalImage(malformed); err == nil {
			t.Fatal("repeated or unrelated owner was admitted")
		}
	}
	record.PathReferences = []pathreferencecodec.Record{baseRows[1], baseRows[0]}
	for left, right := 0, len(record.Claims)-1; left < right; left, right = left+1, right-1 {
		record.Claims[left], record.Claims[right] = record.Claims[right], record.Claims[left]
	}
	reordered, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := UnmarshalImage(reordered)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := MarshalImage(loaded)
	if err != nil || !bytes.Equal(canonical, content) {
		t.Fatal("claim or reference input order changed canonical output", err)
	}
	duplicate := bytes.Replace(content, []byte(`"version": 3`), []byte(`"version": 3, "version": 3`), 1)
	if _, err := UnmarshalImage(duplicate); err == nil {
		t.Fatal("duplicate JSON key was admitted")
	}
	if _, err := UnmarshalImage(bytes.Repeat([]byte(" "), maximumRegistryBytes+1)); err == nil {
		t.Fatal("oversized carrier image was admitted")
	}
}

func testCarrierRegistryImage(t *testing.T) (RegistryImage, []pathauthority.Reference) {
	t.Helper()
	root := t.TempDir()
	first := testGlobalClaim(t, "alpha", "alpha@official", filepath.Join(root, "first"))
	second := testGlobalClaim(t, "beta", "beta@official", filepath.Join(root, "first"))
	pin, _ := testPiPinClaim(t, filepath.Join(root, "second"), "tools", strings.Repeat("a", 40))
	_, next := testPiPinClaim(t, filepath.Join(root, "second"), "tools", strings.Repeat("b", 40))
	transition, err := pin.PinTransitionTo(next)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := transition.PendingClaim()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{first, second, pending})
	if err != nil {
		t.Fatal(err)
	}
	references := make([]pathauthority.Reference, 0, 2)
	for index, claim := range []durablecarrier.ManagedCarrierClaim{first, pending} {
		locator := filepath.Join(root, []string{"FirstState.json", "SecondState.json"}[index])
		reference, err := pathauthority.NewReference(claim.Owner().StatefileAuthority(), locator)
		if err != nil {
			t.Fatal(err)
		}
		references = append(references, reference)
	}
	image, err := NewRegistryImage(registry, references)
	if err != nil {
		t.Fatal(err)
	}
	return image, references
}
