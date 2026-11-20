//go:build darwin || linux

package statefile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

func TestOptionalStateDistinguishesMissingEntryFromEmptyImage(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".daem")
	path := filepath.Join(dir, "state.json")
	state, err := LoadOptionalPhysical(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	before, absent := state.Missing()
	if !absent {
		t.Fatal("an absent ancestor was not admitted as a missing Statefile")
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	current, err := ObserveMissingState(t.Context(), path, physicalwork.NewCaptureBudget(nil))
	if err != nil || before.Equal(current) {
		t.Fatalf("appearance of the selected parent retained prior absence authority: %v", err)
	}
	image, err := Marshal(durable.EmptySnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, image, 0o600); err != nil {
		t.Fatal(err)
	}
	state, err = LoadOptionalPhysical(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, absent := state.Missing(); absent || !state.Snapshot().Equal(durable.EmptySnapshot()) {
		t.Fatal("an empty readable image is not a missing Statefile")
	}
}

func TestMissingStateDoesNotAdmitDanglingSymlink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.Symlink("missing-state-target", path); err != nil {
		t.Fatal(err)
	}
	state, err := LoadOptionalPhysical(t.Context(), path)
	if _, missing := state.Missing(); err == nil || missing {
		t.Fatalf("invalid State supplied an absence proof: %v", err)
	}
	if _, err := ObserveMissingState(t.Context(), path, physicalwork.NewCaptureBudget(nil)); err == nil {
		t.Fatal("release observation treated a dangling symlink as absence")
	}
}
