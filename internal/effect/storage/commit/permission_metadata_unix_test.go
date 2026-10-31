//go:build darwin || linux

package commit

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPermissionMetadataObservationReturnsActualFactsWithoutEffects(t *testing.T) {
	for _, mode := range []fs.FileMode{0o400, 0o600, 0o644, 0o755} {
		t.Run(fmt.Sprintf("%o", mode), func(t *testing.T) {
			root := canonicalTempDir(t)
			path := filepath.Join(root, "config")
			payload := "not configuration syntax [[[\x00"
			writePermissionFixture(t, path, payload, mode)
			var stat unix.Stat_t
			if err := unix.Stat(path, &stat); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			capability := rootedCapabilityForCommitTest(t, captureRootForCommitTest(t, root), "config")
			observed, err := observeRootedFilePermissionMetadata(t.Context(), capability, faultPlan{
				actions: map[phase]func(){
					phaseApplyMode:   func() { t.Error("observation attempted chmod") },
					phaseSyncPayload: func() { t.Error("observation attempted sync") },
				},
			})
			if err != nil || observed.UserID != stat.Uid || observed.GroupID != stat.Gid || observed.Mode != uint32(mode) {
				t.Fatalf("metadata = %+v: %v", observed, err)
			}
			after, err := os.Stat(path)
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
				t.Fatal("metadata observation changed the file", err)
			}
			assertFileContent(t, path, payload)
			assertClosedRootedCapability(t, capability)
		})
	}
}

func TestPermissionMetadataObservationDiscardsFailedFacts(t *testing.T) {
	for _, failure := range []string{"missing", "hard link", "symlink", "special bits", "cancelled", "entry changed", "parent changed"} {
		t.Run(failure, func(t *testing.T) {
			root := canonicalTempDir(t)
			parent := filepath.Join(root, "parent")
			if err := os.Mkdir(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(parent, "config")
			writePermissionFixture(t, path, "unchanged", 0o644)
			captured := captureRootForCommitTest(t, root)
			capability := rootedCapabilityForCommitTest(t, captured, "parent/config")
			ctx := t.Context()
			faults := faultPlan{}
			switch failure {
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "hard link":
				if err := os.Link(path, filepath.Join(root, "alias")); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(path, path+".target"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("config.target", path); err != nil {
					t.Fatal(err)
				}
			case "special bits":
				if err := unix.Chmod(path, 0o1644); err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "entry changed", "parent changed":
				faults.actions = map[phase]func(){phaseCaptureMetadata: func() {
					if failure == "parent changed" {
						if err := os.Rename(parent, parent+".old"); err != nil {
							t.Fatal(err)
						}
						if err := os.Mkdir(parent, 0o700); err != nil {
							t.Fatal(err)
						}
					} else if err := os.Rename(path, path+".old"); err != nil {
						t.Fatal(err)
					}
					writePermissionFixture(t, path, "replacement", 0o600)
				}}
			}
			observed, err := observeRootedFilePermissionMetadata(ctx, capability, faults)
			if err == nil || observed != (FilePermissionMetadata{}) {
				t.Fatal("failed observation exposed metadata", observed, err)
			}
			if failure == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation cause was lost", err)
			}
			if failure == "missing" && !errors.Is(err, fs.ErrNotExist) {
				t.Fatal("absence cause was lost", err)
			}
			assertClosedRootedCapability(t, capability)
		})
	}
}
