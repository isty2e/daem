package fileset

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestPublicationIntentNormalizesAndOwnsFacts(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-created")
	before := publicationImageForTest(t, []byte("before"))
	after := publicationImageForTest(t, []byte("after"))
	selection := []byte("original qualified selection")
	var targets []PublicationTarget
	for index := 8; index >= 0; index-- {
		target, err := NewPublicationTarget(filepath.Join(root, fmt.Sprintf("state-%02d.json", index)), before, after)
		if err != nil {
			t.Fatal(err)
		}
		targets = append(targets, target)
	}
	intent, err := NewPublicationIntent(selection, targets)
	if err != nil {
		t.Fatal(err)
	}
	canonical := intent.Targets()
	if len(canonical) != 9 || canonical[0].Path() != filepath.Join(root, "state-00.json") || !intent.HasChanges() {
		t.Fatal("publication lost or failed to canonicalize its complete target set")
	}
	if !canonical[0].Before().Equal(before) || !canonical[0].After().Equal(after) {
		t.Fatal("publication changed its selected image facts")
	}
	selection[0] = 'x'
	targets[0] = PublicationTarget{}
	canonical[0] = PublicationTarget{}
	returnedSelection := intent.Selection()
	returnedSelection[0] = 'y'
	if string(intent.Selection()) != "original qualified selection" || intent.Targets()[0].Validate() != nil {
		t.Fatal("publication shares mutable input or output slices")
	}
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("publication model created filesystem entries", err)
	}
}

func TestPublicationImagesDistinguishZeroAbsenceAndEmptyFile(t *testing.T) {
	var zero PublicationImage
	absent := AbsentPublicationImage()
	empty := publicationImageForTest(t, nil)
	if zero.Validate() == nil || absent.Validate() != nil || empty.Validate() != nil {
		t.Fatal("publication image initialization was lost")
	}
	if absent.Exists() || !empty.Exists() || absent.Equal(empty) || empty.Size() != 0 {
		t.Fatal("absent and empty publication images were merged")
	}
	if empty.Hash() != hashBytes(nil) || empty.Mode() != 0o600 {
		t.Fatal("publication image did not retain its fingerprint and mode")
	}
	if _, err := NewPublicationImage(nil, fs.ModeDir|0o600); err == nil {
		t.Fatal("publication admitted a non-permission mode")
	}
}

func TestPublicationIntentRejectsInvalidTargets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	image := publicationImageForTest(t, []byte("before"))
	for _, test := range []struct {
		name   string
		path   string
		before PublicationImage
		after  PublicationImage
	}{
		{name: "relative", path: "state.json", before: image, after: image},
		{name: "unclean", path: filepath.Dir(path) + string(filepath.Separator) + "." + string(filepath.Separator) + "state.json", before: image, after: image},
		{name: "nul", path: path + "\x00", before: image, after: image},
		{name: "invalid utf8", path: path + "\xff", before: image, after: image},
		{name: "zero image", path: path, after: image},
		{name: "zero after image", path: path, before: image},
		{name: "deletion", path: path, before: image, after: AbsentPublicationImage()},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewPublicationTarget(test.path, test.before, test.after); err == nil {
				t.Fatal("invalid publication target was admitted")
			}
		})
	}
	target := publicationTargetForTest(t, path, image, image)
	if _, err := NewPublicationIntent([]byte("selection"), []PublicationTarget{target, target}); err == nil {
		t.Fatal("duplicate publication namespaces were admitted")
	}
	if _, err := NewPublicationIntent(nil, []PublicationTarget{target}); err == nil {
		t.Fatal("missing semantic selection was admitted")
	}
	if _, err := NewPublicationIntent([]byte("selection"), nil); err == nil {
		t.Fatal("empty target set was admitted")
	}
	if (PublicationIntent{}).Validate() == nil {
		t.Fatal("zero publication intent was admitted")
	}
	intent, err := NewPublicationIntent([]byte("selection"), []PublicationTarget{target})
	if err != nil || intent.HasChanges() {
		t.Fatal("retained publication image became a write", err)
	}
}

func TestPublicationIntentBoundsCapturedBeforeBytes(t *testing.T) {
	image, err := publicationImageFromFacts(true, maximumTargetBytes, hashBytes([]byte("expected image")), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	var targets []PublicationTarget
	for index := range 5 {
		targets = append(targets, publicationTargetForTest(t, filepath.Join(root, fmt.Sprintf("state-%d.json", index)), image, image))
	}
	if _, err := NewPublicationIntent([]byte("selection"), targets[:4]); err != nil {
		t.Fatal("admitted captured-Before bound was reduced", err)
	}
	if _, err := NewPublicationIntent([]byte("selection"), targets); err == nil {
		t.Fatal("aggregate captured-Before byte limit was lost")
	}
}

func TestPublicationIntentRejectsNestedNamespacesAcrossLexicalSiblings(t *testing.T) {
	root := t.TempDir()
	image := publicationImageForTest(t, []byte("After"))
	var targets []PublicationTarget
	for _, path := range []string{filepath.Join(root, "a"), filepath.Join(root, "a-b"), filepath.Join(root, "a", "c")} {
		targets = append(targets, publicationTargetForTest(t, path, AbsentPublicationImage(), image))
	}
	if _, err := NewPublicationIntent([]byte("selection"), targets); err == nil {
		t.Fatal("an interleaved lexical sibling hid an ancestor target")
	}
	if _, err := NewPublicationIntent([]byte("selection"), targets[:2]); err != nil {
		t.Fatal("a shared string prefix became an ancestor namespace", err)
	}
}

func publicationImageForTest(t *testing.T, content []byte) PublicationImage {
	t.Helper()
	image, err := NewPublicationImage(content, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	return image
}

func publicationTargetForTest(t *testing.T, path string, before, after PublicationImage) PublicationTarget {
	t.Helper()
	target, err := NewPublicationTarget(path, before, after)
	if err != nil {
		t.Fatal(err)
	}
	return target
}
