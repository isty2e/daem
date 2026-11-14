package store

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	pathreferencecodec "github.com/isty2e/daem/internal/encoding/pathreference"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestRegistryImageWirePreservesAllFamiliesAndKeepsVersionBarrier(t *testing.T) {
	root := canonicalTestRoot(t)
	claim := testActiveClaim(t, root, "owner", filepath.Join(root, "settings.json"), "/model")
	other := testActiveClaim(t, root, "other", claim.Address().Path(), "/theme")
	newClaim := testActiveClaim(t, root, "owner", filepath.Join(root, "new.json"), "/model")
	reserved, err := ownership.NewReservedClaim(newClaim.Address(), newClaim.Owner(), "operation-2")
	if err != nil {
		t.Fatal(err)
	}
	bindings := []ownership.DocumentBinding{
		storedDocumentBinding(t, claim, "@pi-agent/settings.json"),
		storedDocumentBinding(t, other, ".pi/settings.json"),
	}
	armed := storedPendingProtection(t, bindings...)
	bound, err := armed.Bind(storedProtectionExecution(t, armed))
	if err != nil {
		t.Fatal(err)
	}
	references := []pathauthority.Reference{
		mustImageReference(t, claim.Address().PathAuthority(), filepath.Join(root, "Selected.json")),
		mustImageReference(t, newClaim.Address().PathAuthority(), filepath.Join(root, "SelectedNew.json")),
		mustImageReference(t, claim.Owner().StatefileAuthority(), filepath.Join(root, "SelectedState.json")),
		mustImageReference(t, other.Owner().StatefileAuthority(), filepath.Join(root, "OtherState.json")),
	}
	for _, protection := range []ownership.PendingProtection{armed, bound} {
		t.Run(string(protection.Phase()), func(t *testing.T) {
			registry, err := ownership.NewRegistryWithProtections([]ownership.Claim{claim, reserved}, bindings, []ownership.PendingProtection{protection})
			if err != nil {
				t.Fatal(err)
			}
			image, err := NewRegistryImage(registry, references)
			if err != nil {
				t.Fatal(err)
			}
			content, err := MarshalImage(image)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := UnmarshalImage(content)
			if err != nil || !loaded.Registry().Equal(registry) || len(loaded.References()) != len(references) {
				t.Fatal("registry image changed a metadata family", err)
			}
			for _, expected := range references {
				actual, present := loaded.Reference(expected.Identity())
				if !present || !actual.Equal(expected) {
					t.Fatal("registry image lost independent access spelling")
				}
			}
			reencoded, err := MarshalImage(loaded)
			if err != nil || !bytes.Equal(reencoded, content) {
				t.Fatal("registry image encoding is not canonical", err)
			}
			legacy, err := Marshal(registry)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := UnmarshalImage(legacy); err == nil {
				t.Fatal("image decoder invented locators for legacy metadata")
			}
			if _, err := decode(content); err == nil {
				t.Fatal("default reader silently adopted the new format")
			}
		})
	}
}

func TestRegistryImageWireRejectsIncompleteOrAmbiguousReferences(t *testing.T) {
	root := canonicalTestRoot(t)
	claim := testActiveClaim(t, root, "owner", filepath.Join(root, "document.json"), "/model")
	registry, err := ownership.NewRegistry([]ownership.Claim{claim})
	if err != nil {
		t.Fatal(err)
	}
	image, err := NewRegistryImage(registry, []pathauthority.Reference{
		mustImageReference(t, claim.Address().PathAuthority(), claim.Address().Path()),
		mustImageReference(t, claim.Owner().StatefileAuthority(), claim.Owner().StatefileKey()),
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := MarshalImage(image)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		field string
		value json.RawMessage
	}{
		{name: "missing references", field: "path_references"},
		{name: "null references", field: "path_references", value: json.RawMessage("null")},
		{name: "empty references", field: "path_references", value: json.RawMessage("[]")},
		{name: "null reference", field: "path_references", value: json.RawMessage("[null]")},
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
				t.Fatal("invalid registry image was admitted")
			}
		})
	}
	duplicate := bytes.Replace(content, []byte(`"version": 5`), []byte(`"version": 5, "version": 5`), 1)
	if _, err := UnmarshalImage(duplicate); err == nil {
		t.Fatal("duplicate JSON field was admitted")
	}
	if content, err := MarshalImage(RegistryImage{}); err == nil || content != nil {
		t.Fatal("uninitialized image produced metadata", err)
	}
	if _, err := UnmarshalImage(bytes.Repeat([]byte(" "), int(maximumOwnershipRegistryBytes)+1)); err == nil {
		t.Fatal("oversized image was admitted")
	}

	var record registryImageRecord
	if err := json.Unmarshal(content, &record); err != nil {
		t.Fatal(err)
	}
	baseRows := record.PathReferences
	conflicting := baseRows[0]
	conflicting.Locator = filepath.Join(root, "another-location.json")
	unrelated := pathreferencecodec.Record{
		Key: filepath.Join(root, "unrelated.json"), SemanticsWitness: "exact-v1:", Locator: filepath.Join(root, "unrelated.json"),
	}
	for _, test := range []struct {
		name string
		row  pathreferencecodec.Record
	}{
		{name: "identical repeated identity", row: baseRows[0]},
		{name: "conflicting repeated identity", row: conflicting},
		{name: "unrelated identity", row: unrelated},
	} {
		t.Run(test.name, func(t *testing.T) {
			record.PathReferences = append(append([]pathreferencecodec.Record(nil), baseRows...), test.row)
			malformed, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := UnmarshalImage(malformed); err == nil {
				t.Fatal("ambiguous or unrelated wire reference was admitted")
			}
		})
	}

	t.Run("reordered unique references", func(t *testing.T) {
		record.PathReferences = []pathreferencecodec.Record{baseRows[1], baseRows[0]}
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
			t.Fatal("input reference order changed the canonical image", err)
		}
	})
}
