package fileset

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestPublicationClassificationCoversWholeSetWithoutStaticCount(t *testing.T) {
	root := mustCanonicalStateDir(t, t.TempDir())
	firstPath := filepath.Join(root, "state-0.json")
	writeFixture(t, firstPath, "after", 0o600)
	info, err := os.Stat(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	before, err := NewPublicationImage([]byte("before"), info.Mode().Perm())
	if err != nil {
		t.Fatal(err)
	}
	after, err := NewPublicationImage([]byte("after"), info.Mode().Perm())
	if err != nil {
		t.Fatal(err)
	}
	var targets []PublicationTarget
	for index := range 9 {
		path := filepath.Join(root, fmt.Sprintf("state-%d.json", index))
		writeFixture(t, path, "after", 0o600)
		targets = append(targets, publicationTargetForTest(t, path, before, after))
	}
	intent, err := NewPublicationIntent([]byte("original selection"), targets)
	if err != nil {
		t.Fatal(err)
	}
	got, err := classifyPublicationTargets(t.Context(), intent)
	if err != nil || !got.cleanAfter || !got.recoverable {
		t.Fatal("complete After set was not recognized", got, err)
	}
	writeFixture(t, targets[0].path, "before", 0o600)
	got, err = classifyPublicationTargets(t.Context(), intent)
	if err != nil || got.cleanAfter || !got.recoverable {
		t.Fatal("mixed Before/After set was not recoverable", got, err)
	}
	writeFixture(t, targets[8].path, "third", 0o600)
	got, err = classifyPublicationTargets(t.Context(), intent)
	if err != nil || got.cleanAfter || got.recoverable {
		t.Fatal("same-size third image was accepted", got, err)
	}
}

func TestPublicationClassificationDistinguishesAbsenceEmptyAndMode(t *testing.T) {
	path := filepath.Join(mustCanonicalStateDir(t, t.TempDir()), "state.json")
	absent := AbsentPublicationImage()
	writeFixture(t, path, "", 0o600)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := NewPublicationImage(nil, info.Mode().Perm())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := publicationTargetForTest(t, path, absent, empty)
	intent, err := NewPublicationIntent([]byte("selection"), []PublicationTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	got, err := classifyPublicationTargets(t.Context(), intent)
	if err != nil || got.cleanAfter || !got.recoverable {
		t.Fatal("absent Before was not recognized", got, err)
	}
	writeFixture(t, path, "", 0o600)
	got, err = classifyPublicationTargets(t.Context(), intent)
	if err != nil || !got.cleanAfter || !got.recoverable {
		t.Fatal("present empty After was not recognized", got, err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(path, info.Mode().Perm()); err != nil {
			t.Error(err)
		}
	})
	if err := os.Chmod(path, info.Mode().Perm()&^0o222); err != nil {
		t.Fatal(err)
	}
	got, err = classifyPublicationTargets(t.Context(), intent)
	if err != nil || got.cleanAfter || got.recoverable {
		t.Fatal("changed image permissions were accepted", got, err)
	}
}

func TestPublicationClassificationRefusesUnexpectedSizeOrKind(t *testing.T) {
	path := filepath.Join(mustCanonicalStateDir(t, t.TempDir()), "state.json")
	image := publicationImageForTest(t, []byte("a"))
	target := publicationTargetForTest(t, path, image, image)
	intent, err := NewPublicationIntent([]byte("selection"), []PublicationTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, path, "larger third state", 0o600)
	got, err := classifyPublicationTargets(t.Context(), intent)
	if err != nil || got.recoverable || got.cleanAfter {
		t.Fatal("unexpected size was accepted", got, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	got, err = classifyPublicationTargets(t.Context(), intent)
	if err != nil || got.recoverable || got.cleanAfter {
		t.Fatal("directory was treated as a regular-file image", got, err)
	}
}

func TestPublicationClassificationRejectsZeroAndPreservesCancellation(t *testing.T) {
	if _, err := classifyPublicationTargets(t.Context(), PublicationIntent{}); err == nil {
		t.Fatal("uninitialized intent was classified")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := classifyPublicationTargets(ctx, PublicationIntent{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation was replaced with record failure", err)
	}
}

func TestPublicationClassificationDoesNotProveAbsenceThroughAncestorSymlink(t *testing.T) {
	root := mustCanonicalStateDir(t, t.TempDir())
	parent := filepath.Join(root, "managed")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "state.json")
	absent := AbsentPublicationImage()
	intent, err := NewPublicationIntent([]byte("selection"), []PublicationTarget{
		publicationTargetForTest(t, path, absent, absent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := classifyPublicationTargets(t.Context(), intent); err != nil || !got.cleanAfter || !got.recoverable {
		t.Fatal("unchanged absent namespace was not classified", got, err)
	}
	if err := os.Rename(parent, filepath.Join(root, "previous")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), parent); err != nil {
		t.Fatal(err)
	}
	if got, err := classifyPublicationTargets(t.Context(), intent); err == nil || got.cleanAfter || got.recoverable {
		t.Fatal("ancestor alias became proof of absence", got, err)
	}
}
