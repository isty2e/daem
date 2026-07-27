package listworkflow

import (
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

type locationExpectation struct {
	target          target.Target
	scope           target.Scope
	resource        entity.Kind
	variant         string
	role            LocationRole
	path            string
	operation       profile.Operation
	reason          string
	selected        bool
	requested       bool
	defaultChoice   bool
	selectionSource LocationSelectionSource
}

func assertLocationEntry(
	t *testing.T,
	entries []LocationEntry,
	want locationExpectation,
) LocationEntry {
	t.Helper()
	for _, entry := range entries {
		if entry.Target() != want.target ||
			entry.Scope() != want.scope ||
			entry.ResourceKind() != want.resource ||
			entry.Variant() != want.variant ||
			entry.Role() != want.role ||
			entry.Path() != want.path ||
			entry.Operation() != want.operation ||
			entry.Reason() != want.reason {
			continue
		}
		if entry.Selected() != want.selected ||
			entry.Requested() != want.requested ||
			entry.Default() != want.defaultChoice {
			t.Fatalf("location entry = %#v, want selected/requested/default = %t/%t/%t",
				entry, want.selected, want.requested, want.defaultChoice)
		}
		if want.selectionSource != "" && entry.SelectionSource() != want.selectionSource {
			t.Fatalf("location selection source = %q, want %q", entry.SelectionSource(), want.selectionSource)
		}
		return entry
	}
	t.Fatalf("location entry not found: %#v", want)
	return LocationEntry{}
}

func assertLocationInventoryOrder(t *testing.T, entries []LocationEntry) {
	t.Helper()
	for index := 1; index < len(entries); index++ {
		if compareLocationEntries(entries[index-1], entries[index]) >= 0 {
			t.Fatalf("location entries out of order at %d: %#v then %#v", index, entries[index-1], entries[index])
		}
	}
}

func writePathInventoryManifest(t *testing.T, content string) string {
	t.Helper()
	manifestPath := t.TempDir() + "/daem.toml"
	writeFile(t, manifestPath, content)
	return manifestPath
}
