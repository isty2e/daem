//go:build darwin || linux

package commit

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/sys/unix"
)

func TestAncestorPublicationCoordinatesStaleAbsenceAcrossHandles(t *testing.T) {
	root := canonicalTempDir(t)
	const workers = 8
	anchors := make([]*anchoredParent, workers)
	var publications atomic.Int64
	for index := range anchors {
		anchor, err := openAnchoredParent(filepath.Join(root, "entry"), false)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(anchor.close)
		var observed unix.Stat_t
		if err := unix.Fstatat(anchor.parentFD(), "shared", &observed, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
			t.Fatalf("initial child observation = %v, want ENOENT", err)
		}
		anchor.ancestorPublicationHooks.before = func(string) { publications.Add(1) }
		anchors[index] = anchor
	}

	results := make([]error, workers)
	var group sync.WaitGroup
	for index, anchor := range anchors {
		group.Go(func() {
			parent := anchor.directories[len(anchor.directories)-1]
			results[index] = anchor.createAndPublishChildDirectory(parent, "shared")
		})
	}
	group.Wait()

	var published unix.Stat_t
	if err := unix.Stat(filepath.Join(root, "shared"), &published); err != nil {
		t.Fatal(err)
	}
	owners := 0
	for index, anchor := range anchors {
		if results[index] != nil {
			t.Fatalf("creator %d: %v", index, results[index])
		}
		child := anchor.directories[len(anchor.directories)-1]
		var opened unix.Stat_t
		if err := unix.Fstat(child.fd, &opened); err != nil {
			t.Fatal(err)
		}
		if opened.Dev != published.Dev || opened.Ino != published.Ino {
			t.Fatalf("creator %d retained a different directory", index)
		}
		if child.created {
			owners++
		}
	}
	if owners != 1 || publications.Load() != 1 {
		t.Fatalf("cleanup owners=%d, publication attempts=%d; want one creator and seven adopters", owners, publications.Load())
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "shared" {
		t.Fatalf("unexpected parent contents after publication: %v, %v", entries, err)
	}
}

func TestAncestorPublicationReobservationRejectsNonDirectories(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		name := "file"
		if symlink {
			name = "symlink"
		}
		t.Run(name, func(t *testing.T) {
			root := canonicalTempDir(t)
			anchor, err := openAnchoredParent(filepath.Join(root, "entry"), false)
			if err != nil {
				t.Fatal(err)
			}
			defer anchor.close()
			parent := anchor.directories[len(anchor.directories)-1]
			path := filepath.Join(root, "shared")
			if symlink {
				if err := os.Symlink(canonicalTempDir(t), path); err != nil {
					t.Fatal(err)
				}
			} else {
				writeTestFile(t, path, "preserve", 0o600)
			}
			anchor.ancestorPublicationHooks.before = func(string) {
				t.Error("attempted publication over a reobserved non-directory")
			}
			if err := anchor.createAndPublishChildDirectory(parent, "shared"); err == nil {
				t.Fatal("accepted a non-directory ancestor")
			}
			if !symlink {
				assertFile(t, path, "preserve", 0o600)
			}
			if err := os.Rename(path, filepath.Join(root, "preserved")); err != nil {
				t.Fatal(err)
			}
			anchor.ancestorPublicationHooks.before = nil
			if err := anchor.createAndPublishChildDirectory(parent, "shared"); err != nil {
				t.Fatalf("publication after refusal: %v", err)
			}
		})
	}
}

func TestAncestorPublicationAllowsDistinctParentsToProgress(t *testing.T) {
	firstRoot := canonicalTempDir(t)
	secondRoot := canonicalTempDir(t)
	anchor, err := openAnchoredParent(filepath.Join(firstRoot, "entry"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer anchor.close()
	var otherErr error
	anchor.ancestorPublicationHooks.before = func(string) {
		otherErr = PrepareCommitParent(t.Context(), filepath.Join(secondRoot, "independent", "entry"))
	}
	parent := anchor.directories[len(anchor.directories)-1]
	if err := anchor.createAndPublishChildDirectory(parent, "shared"); err != nil {
		t.Fatal(err)
	}
	if otherErr != nil {
		t.Fatalf("independent parent publication: %v", otherErr)
	}
	if info, err := os.Stat(filepath.Join(secondRoot, "independent")); err != nil || !info.IsDir() {
		t.Fatalf("independent ancestor not published: info=%v err=%v", info, err)
	}
}
