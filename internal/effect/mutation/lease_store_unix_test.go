//go:build darwin || linux

package mutation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreRejectsSymlinkAtEveryLeaseNamespaceComponent(t *testing.T) {
	for _, components := range [][]string{
		{"locks"},
		{"locks", "mutation"},
		{"locks", "mutation", "v1"},
	} {
		t.Run(strings.Join(components, "-"), func(t *testing.T) {
			dataDir := t.TempDir()
			external := t.TempDir()
			parent := dataDir
			for _, component := range components[:len(components)-1] {
				parent = filepath.Join(parent, component)
				if err := os.Mkdir(parent, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(external, filepath.Join(parent, components[len(components)-1])); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}

			store, err := NewStore(dataDir)
			if err == nil {
				domain := mutationTestLogicalDomain(
					t,
					filepath.Join(t.TempDir(), "resource"),
					AccessExclusive,
				)
				_, err = store.Acquire(context.Background(), domain)
			}
			if err == nil {
				t.Fatal("mutation lease store followed a nested namespace symlink")
			}
			entries, readErr := os.ReadDir(external)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if len(entries) != 0 {
				t.Fatalf("external lease target contains %d entries", len(entries))
			}
		})
	}
}

func TestStoreRejectsLeaseNamespaceSymlinkInsertedAfterConstruction(t *testing.T) {
	for _, components := range [][]string{
		{"locks"},
		{"locks", "mutation"},
		{"locks", "mutation", "v1"},
	} {
		t.Run(strings.Join(components, "-"), func(t *testing.T) {
			dataDir := t.TempDir()
			store, err := NewStore(dataDir)
			if err != nil {
				t.Fatal(err)
			}
			external := t.TempDir()
			parent := dataDir
			for _, component := range components[:len(components)-1] {
				parent = filepath.Join(parent, component)
				if err := os.Mkdir(parent, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(external, filepath.Join(parent, components[len(components)-1])); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			domain := mutationTestLogicalDomain(
				t,
				filepath.Join(t.TempDir(), "resource"),
				AccessExclusive,
			)
			if _, err := store.Acquire(context.Background(), domain); err == nil {
				t.Fatal("mutation lease store followed a namespace symlink inserted after construction")
			}
			entries, err := os.ReadDir(external)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("external lease target contains %d entries", len(entries))
			}
		})
	}
}

func TestHeldLeaseRejectsNamespaceDirectoryReplacement(t *testing.T) {
	store := mutationTestStore(t)
	domain := mutationTestLogicalDomain(
		t,
		filepath.Join(t.TempDir(), "resource"),
		AccessExclusive,
	)
	set, err := store.Acquire(context.Background(), domain)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Release()

	locks := filepath.Join(store.dataDir, "locks")
	retired := filepath.Join(store.dataDir, "locks-retired")
	if err := os.Rename(locks, retired); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(locks, 0o700); err != nil {
		t.Fatal(err)
	}
	if matches, err := set.DomainsMatchCurrent(context.Background()); err == nil || matches ||
		!strings.Contains(err.Error(), "namespace") {
		t.Fatalf("DomainsMatchCurrent after namespace replacement = %v, %v", matches, err)
	}
}
