package fileset

import (
	"fmt"
	"path/filepath"
	"sort"
	"testing"
)

func TestNewFileWriteOwnsAfterImage(t *testing.T) {
	content := []byte("after")
	target, err := NewFileWrite(filepath.Join(t.TempDir(), "target"), content)
	if err != nil {
		t.Fatal(err)
	}

	content[0] = 'X'
	if got := string(target.content); got != "after" {
		t.Fatalf("target content = %q, want constructor-owned snapshot", got)
	}
}

func TestCanonicalTargetsReuseOwnedAfterImages(t *testing.T) {
	root := t.TempDir()
	values := make([]FileTarget, 0, maximumFileSetTargets)
	owned := make(map[string]*byte, maximumFileSetTargets)
	expectedPaths := make([]string, 0, maximumFileSetTargets)
	for index := maximumFileSetTargets - 1; index >= 0; index-- {
		path := filepath.Join(root, fmt.Sprintf("target-%02d", index))
		target, err := NewFileWrite(path, []byte(fmt.Sprintf("after-%02d", index)))
		if err != nil {
			t.Fatal(err)
		}
		owned[target.path] = &target.content[0]
		expectedPaths = append(expectedPaths, target.path)
		values = append(values, target)
	}
	sort.Strings(expectedPaths)

	canonical, err := canonicalTargets(values)
	if err != nil {
		t.Fatal(err)
	}
	if len(canonical) != maximumFileSetTargets {
		t.Fatalf("canonical target count = %d, want %d", len(canonical), maximumFileSetTargets)
	}
	for index, target := range canonical {
		wantPath := expectedPaths[index]
		if target.path != wantPath {
			t.Fatalf("canonical target[%d] path = %q, want %q", index, target.path, wantPath)
		}
		if got := &target.content[0]; got != owned[target.path] {
			t.Fatalf("canonical target[%d] cloned its owned after-image", index)
		}
	}
}

func TestCanonicalTargetsDiscardRetainPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "target")
	canonical, err := canonicalTargets([]FileTarget{{
		path:    path,
		content: []byte("not-an-after-image"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if canonical[0].content != nil {
		t.Fatalf("retained target content = %q, want none", canonical[0].content)
	}
}
