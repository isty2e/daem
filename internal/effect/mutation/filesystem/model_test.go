package filesystem

import (
	"errors"
	"io/fs"
	"testing"
)

type classifiedTestFailure struct {
	kind FailureKind
}

func (failure classifiedTestFailure) Error() string     { return string(failure.kind) }
func (failure classifiedTestFailure) Kind() FailureKind { return failure.kind }

func TestRegularFileSnapshotOwnsContentAndMode(t *testing.T) {
	content := []byte("before")
	snapshot := NewRegularFileSnapshot(content, fs.FileMode(0o764)|fs.ModeSetuid)
	content[0] = 'x'

	first := snapshot.Content()
	first[0] = 'y'
	if got := string(snapshot.Content()); got != "before" {
		t.Fatalf("snapshot content = %q, want %q", got, "before")
	}
	if got := snapshot.Mode(); got != 0o764 {
		t.Fatalf("snapshot mode = %04o, want 0764", got)
	}
}

func TestTreeRelativePathRejectsUnsafeAndZeroValues(t *testing.T) {
	for _, components := range [][]string{
		nil,
		{""},
		{"."},
		{".."},
		{"a/b"},
		{"a\x00b"},
	} {
		if _, err := NewTreeRelativePath(components...); err == nil {
			t.Fatalf("NewTreeRelativePath(%q) succeeded", components)
		}
	}
	if err := (TreeRelativePath{}).Validate(); err == nil {
		t.Fatal("zero TreeRelativePath validated")
	}

	path, err := NewTreeRelativePath("one", "two")
	if err != nil {
		t.Fatalf("NewTreeRelativePath returned error: %v", err)
	}
	if got := path.Path(); got != "one/two" {
		t.Fatalf("path = %q, want %q", got, "one/two")
	}
}

func TestFailureVisibilityClassificationIsConservative(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "success"},
		{name: "uncommitted", err: classifiedTestFailure{kind: FailureUncommitted}},
		{name: "unsupported", err: classifiedTestFailure{kind: FailureUnsupportedGuarantee}},
		{name: "indeterminate", err: classifiedTestFailure{kind: FailureIndeterminateCommit}, want: true},
		{name: "residue", err: classifiedTestFailure{kind: FailureRetainedResidue}, want: true},
		{name: "unknown", err: errors.New("unknown"), want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := MayHaveVisibleEffect(test.err); got != test.want {
				t.Fatalf("MayHaveVisibleEffect() = %t, want %t", got, test.want)
			}
		})
	}
}
