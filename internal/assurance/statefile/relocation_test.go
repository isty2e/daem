package statefile

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
)

func TestTransferredSnapshotPreservesPendingAndHistoricalFacts(t *testing.T) {
	original := testV9Snapshot(t)
	from := original.ManagedCarrierClaims()[0].Owner()
	path, err := mutation.ObservePersistedDirectoryEntryAuthority(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	to, err := stateauthority.New(path.Exact(), filepath.Join(t.TempDir(), "different-provenance.toml"))
	if err != nil {
		t.Fatal(err)
	}
	before, err := Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	transferred, err := original.TransferAuthority(from, to)
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range []stateauthority.Authority{
		transferred.ManagedCarrierClaims()[0].Owner(), transferred.PendingCarrierInstalls()[0].Owner(), transferred.PendingCarrierRemovals()[0].Owner(),
	} {
		if !owner.Equal(to) || owner.ManifestPath() != from.ManifestPath() {
			t.Fatalf("transferred owner = %#v", owner)
		}
	}
	if !reflect.DeepEqual(transferred.ManagedPaths(), original.ManagedPaths()) ||
		!reflect.DeepEqual(transferred.ManagedAggregates(), original.ManagedAggregates()) ||
		!reflect.DeepEqual(transferred.DelegateAttempts(), original.DelegateAttempts()) ||
		!reflect.DeepEqual(transferred.HostRouteAttempts(), original.HostRouteAttempts()) {
		t.Fatal("transfer changed non-owner facts")
	}
	back, err := transferred.TransferAuthority(to, from)
	if err != nil || !back.Equal(original) {
		t.Fatalf("reverse transfer lost facts: %v", err)
	}
	unchanged, err := Marshal(original)
	if err != nil || !bytes.Equal(before, unchanged) {
		t.Fatalf("transfer mutated input: %v", err)
	}
	encoded, err := Marshal(transferred)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded)
	if err != nil || !decoded.Equal(transferred) {
		t.Fatalf("transferred codec round trip: %v", err)
	}
	if _, err := original.TransferAuthority(to, from); err == nil {
		t.Fatal("foreign source owner accepted")
	}
}

func TestRelocationReceiptIsStrictAndNotASnapshot(t *testing.T) {
	root := t.TempDir()
	fromPath, err := mutation.ObservePersistedDirectoryEntryAuthority(filepath.Join(root, "old.json"))
	if err != nil {
		t.Fatal(err)
	}
	toPath, err := mutation.ObservePersistedDirectoryEntryAuthority(filepath.Join(root, "new.json"))
	if err != nil {
		t.Fatal(err)
	}
	from, err := stateauthority.New(fromPath.Exact(), filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	to, err := from.WithStatefile(toPath.Exact())
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := NewRelocation(from, to)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := MarshalRelocation(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(encoded); err == nil {
		t.Fatal("receipt admitted as active state")
	}
	if _, err := NewRelocation(from, from); err == nil {
		t.Fatal("same-authority relocation accepted")
	}
	for name, content := range map[string][]byte{
		"valid":     encoded,
		"unknown":   []byte(strings.Replace(string(encoded), `"kind":`, `"unexpected": 1, "kind":`, 1)),
		"duplicate": []byte(strings.Replace(string(encoded), `"kind":`, `"kind": "state-relocation", "kind":`, 1)),
		"trailing":  append(append([]byte(nil), encoded...), []byte(`{}`)...),
		"version":   []byte(strings.Replace(string(encoded), `"version": 1`, `"version": 99`, 1)),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(root, name+".json")
			if err := os.WriteFile(path, content, 0o600); err != nil {
				t.Fatal(err)
			}
			loaded, found, err := LoadRelocation(t.Context(), path)
			if name == "valid" {
				if err != nil || !found || !loaded.From().ExactEqual(from) || !loaded.To().ExactEqual(to) {
					t.Fatalf("receipt = %#v, %t, %v", loaded, found, err)
				}
			} else if err == nil {
				t.Fatal("malformed receipt accepted")
			}
		})
	}
}
