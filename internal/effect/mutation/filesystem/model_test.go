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

type testEntryIdentity struct {
	value string
	kind  EntryKind
}

func (identity testEntryIdentity) Equal(other EntryIdentity) bool {
	candidate, ok := other.(testEntryIdentity)
	return ok && identity == candidate
}

func (identity testEntryIdentity) Kind() EntryKind {
	return identity.kind
}

func TestRegularFileSnapshotOwnsContentAndMode(t *testing.T) {
	content := []byte("before")
	identity := testEntryIdentity{value: "regular", kind: EntryKindFile}
	if _, err := NewRegularFileSnapshot(
		content,
		fs.FileMode(0o764)|fs.ModeSetuid,
		identity,
	); err == nil {
		t.Fatal("NewRegularFileSnapshot accepted non-permission mode bits")
	}
	snapshot, err := NewRegularFileSnapshot(content, 0o764, identity)
	if err != nil {
		t.Fatalf("NewRegularFileSnapshot: %v", err)
	}
	content[0] = 'x'

	first := snapshot.Content()
	first[0] = 'y'
	if got := string(snapshot.Content()); got != "before" {
		t.Fatalf("snapshot content = %q, want %q", got, "before")
	}
	if got := snapshot.Mode(); got != 0o764 {
		t.Fatalf("snapshot mode = %04o, want 0764", got)
	}
	if got := snapshot.Identity(); !got.Equal(identity) {
		t.Fatalf("snapshot identity = %#v, want %#v", got, identity)
	}
	if _, err := NewRegularFileSnapshot(
		content,
		0o600,
		testEntryIdentity{value: "directory", kind: EntryKindDirectory},
	); err == nil {
		t.Fatal("NewRegularFileSnapshot accepted a directory identity")
	}
}

func TestDirectorySnapshotNormalizesAndOwnsEntries(t *testing.T) {
	root := testEntryIdentity{value: "root", kind: EntryKindDirectory}
	second, err := NewDirectoryEntrySnapshot(
		"second",
		testEntryIdentity{value: "second", kind: EntryKindSpecial},
		0o640,
		false,
		7,
	)
	if err != nil {
		t.Fatalf("NewDirectoryEntrySnapshot(second): %v", err)
	}
	first, err := NewDirectoryEntrySnapshot(
		"first",
		testEntryIdentity{value: "first", kind: EntryKindFile},
		0o600,
		true,
		3,
	)
	if err != nil {
		t.Fatalf("NewDirectoryEntrySnapshot(first): %v", err)
	}
	input := []DirectoryEntrySnapshot{second, first}
	snapshot, err := NewDirectorySnapshot(root, 0o700, true, input)
	if err != nil {
		t.Fatalf("NewDirectorySnapshot: %v", err)
	}
	input[0] = DirectoryEntrySnapshot{}

	entries := snapshot.Entries()
	if len(entries) != 2 || entries[0].Name() != "first" || entries[1].Name() != "second" {
		t.Fatalf("Entries = %#v, want lexical first/second", entries)
	}
	entries[0] = DirectoryEntrySnapshot{}
	if got := snapshot.Entries()[0].Name(); got != "first" {
		t.Fatalf("snapshot entry after caller mutation = %q, want first", got)
	}
	if snapshot.RootIdentity() != root || snapshot.RootMode() != 0o700 ||
		!snapshot.RootOwnedByInvoker() {
		t.Fatalf("root facts = (%v, %04o, %t)", snapshot.RootIdentity(), snapshot.RootMode(), snapshot.RootOwnedByInvoker())
	}
	if entries = snapshot.Entries(); entries[0].Kind() != EntryKindFile ||
		!entries[0].OwnedByInvoker() || entries[0].Mode() != 0o600 || entries[0].Size() != 3 {
		t.Fatalf("first entry facts = (%s, %t, %04o, %d)", entries[0].Kind(), entries[0].OwnedByInvoker(), entries[0].Mode(), entries[0].Size())
	}
	if entries[1].Kind() != EntryKindSpecial || entries[1].OwnedByInvoker() {
		t.Fatalf("second entry facts = (%s, %t)", entries[1].Kind(), entries[1].OwnedByInvoker())
	}
	equivalent, err := NewDirectorySnapshot(root, 0o700, true, []DirectoryEntrySnapshot{first, second})
	if err != nil {
		t.Fatalf("NewDirectorySnapshot(equivalent): %v", err)
	}
	if !snapshot.Equal(equivalent) {
		t.Fatal("equivalent directory snapshots did not compare equal")
	}
	different, err := NewDirectorySnapshot(root, 0o700, true, []DirectoryEntrySnapshot{first})
	if err != nil {
		t.Fatalf("NewDirectorySnapshot(different): %v", err)
	}
	if snapshot.Equal(different) {
		t.Fatal("different directory snapshots compared equal")
	}
}

func TestDirectorySnapshotRejectsInvalidAndDuplicateFacts(t *testing.T) {
	validIdentity := testEntryIdentity{value: "entry", kind: EntryKindFile}
	for _, test := range []struct {
		name     string
		identity EntryIdentity
		mode     fs.FileMode
		size     int64
	}{
		{name: ""},
		{name: "."},
		{name: ".."},
		{name: "a/b", identity: validIdentity},
		{name: "a\x00b", identity: validIdentity},
		{name: "entry"},
		{name: "entry", identity: testEntryIdentity{}},
		{name: "entry", identity: validIdentity, mode: fs.ModeSetuid | 0o600},
		{name: "entry", identity: validIdentity, size: -1},
	} {
		if _, err := NewDirectoryEntrySnapshot(
			test.name,
			test.identity,
			test.mode,
			true,
			test.size,
		); err == nil {
			t.Fatalf("NewDirectoryEntrySnapshot(%q, %#v, %04o, %d) succeeded", test.name, test.identity, test.mode, test.size)
		}
	}

	entry, err := NewDirectoryEntrySnapshot("entry", validIdentity, 0o600, true, 0)
	if err != nil {
		t.Fatalf("NewDirectoryEntrySnapshot(valid): %v", err)
	}
	for _, test := range []struct {
		name    string
		root    EntryIdentity
		mode    fs.FileMode
		entries []DirectoryEntrySnapshot
	}{
		{name: "missing root"},
		{name: "non-directory root", root: validIdentity},
		{name: "invalid root mode", root: testEntryIdentity{value: "root", kind: EntryKindDirectory}, mode: fs.ModeSetgid},
		{name: "duplicate", root: testEntryIdentity{value: "root", kind: EntryKindDirectory}, entries: []DirectoryEntrySnapshot{entry, entry}},
		{name: "zero entry", root: testEntryIdentity{value: "root", kind: EntryKindDirectory}, entries: []DirectoryEntrySnapshot{{}}},
	} {
		if _, err := NewDirectorySnapshot(test.root, test.mode, true, test.entries); err == nil {
			t.Fatalf("NewDirectorySnapshot(%s) succeeded", test.name)
		}
	}
	if (DirectorySnapshot{}).RootIdentity() != nil ||
		(DirectorySnapshot{}).Entries() != nil ||
		(DirectorySnapshot{}).RootOwnedByInvoker() {
		t.Fatal("zero DirectorySnapshot exposed initialized facts")
	}
	if (DirectorySnapshot{}).Equal(DirectorySnapshot{}) {
		t.Fatal("zero DirectorySnapshot compared equal")
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
