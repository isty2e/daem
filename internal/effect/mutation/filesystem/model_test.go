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

func TestTreeTraversalLimitsRequireFiniteCanonicalBounds(t *testing.T) {
	structure, err := NewTreeStructureLimits(3, 0)
	if err != nil {
		t.Fatal(err)
	}
	if structure.MaximumEntries() != 3 ||
		structure.MaximumDepth() != 0 {
		t.Fatalf(
			"structure limits = (%d, %d)",
			structure.MaximumEntries(),
			structure.MaximumDepth(),
		)
	}
	emptyStructure, err := NewTreeStructureLimits(0, 0)
	if err != nil || emptyStructure.MaximumEntries() != 0 {
		t.Fatalf("exact-empty structure limit = %#v, constructor = %v", emptyStructure, err)
	}

	for _, test := range []struct {
		entries int
		depth   int
		bytes   int64
	}{
		{entries: -1, depth: 0, bytes: 1},
		{entries: 1, depth: -1, bytes: 1},
		{entries: 1, depth: 0, bytes: -1},
	} {
		if _, err := NewTreeTraversalLimits(
			test.entries,
			test.depth,
			test.bytes,
		); err == nil {
			t.Fatalf(
				"NewTreeTraversalLimits(%d, %d, %d) succeeded",
				test.entries,
				test.depth,
				test.bytes,
			)
		}
	}
	exactEmpty, err := NewTreeTraversalLimits(0, 0, 0)
	if err != nil || exactEmpty.Validate() != nil {
		t.Fatalf("exact-empty traversal limits validation = %v, constructor = %v", exactEmpty.Validate(), err)
	}
	if err := (TreeTraversalLimits{}).Validate(); err == nil {
		t.Fatal("zero-value traversal limits passed validation")
	}
	limits, err := NewTreeTraversalLimits(3, 0, 7)
	if err != nil {
		t.Fatal(err)
	}
	if limits.MaximumEntries() != 3 ||
		limits.MaximumDepth() != 0 ||
		limits.MaximumBytes() != 7 ||
		limits.Validate() != nil {
		t.Fatalf(
			"limits = (%d, %d, %d), validation=%v",
			limits.MaximumEntries(),
			limits.MaximumDepth(),
			limits.MaximumBytes(),
			limits.Validate(),
		)
	}
}

func TestRootedCleanupWorkEnvelopeOwnsCompleteStorageWork(t *testing.T) {
	fileLimits, err := NewTreeTraversalLimits(0, 0, 11)
	if err != nil {
		t.Fatal(err)
	}
	file, err := NewRootedCleanupWorkEnvelope(EntryKindFile, fileLimits)
	if err != nil {
		t.Fatalf("construct file cleanup envelope: %v", err)
	}
	filePathWork, err := file.PathWork(1)
	if err != nil {
		t.Fatalf("file cleanup path work: %v", err)
	}
	if file.EntryWork() != 0 || file.ByteWork() != 33 || filePathWork != 8 {
		t.Fatalf(
			"file cleanup envelope = entries:%d bytes:%d namespace:%d",
			file.EntryWork(),
			file.ByteWork(),
			filePathWork,
		)
	}

	directoryLimits, err := NewTreeTraversalLimits(5, 3, 11)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := NewRootedCleanupWorkEnvelope(EntryKindDirectory, directoryLimits)
	if err != nil {
		t.Fatalf("construct directory cleanup envelope: %v", err)
	}
	directoryPathWork, err := directory.PathWork(1)
	if err != nil {
		t.Fatalf("directory cleanup path work: %v", err)
	}
	if directory.EntryWork() != 36 || directory.ByteWork() != 33 ||
		directoryPathWork != 88 {
		t.Fatalf(
			"directory cleanup envelope = entries:%d bytes:%d namespace:%d",
			directory.EntryWork(),
			directory.ByteWork(),
			directoryPathWork,
		)
	}
	if pathWork, err := directory.PathWork(7); err != nil || pathWork != 616 {
		t.Fatalf("directory path work = %d, err=%v, want 616", pathWork, err)
	}

	if _, err := NewRootedCleanupWorkEnvelope(EntryKindSymlink, directoryLimits); err == nil {
		t.Fatal("cleanup envelope accepted an unsupported root kind")
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

func TestCommitOutcomeEnforcesStateAndRetainedNameContract(t *testing.T) {
	retained := []string{".stage-b", ".stage-a"}
	outcome, err := NewCommitOutcome(CommitOutcomeRetainedRecoverable, retained)
	if err != nil {
		t.Fatalf("NewCommitOutcome: %v", err)
	}
	retained[0] = ".changed"

	if got := outcome.State(); got != CommitOutcomeRetainedRecoverable {
		t.Fatalf("State = %q, want %q", got, CommitOutcomeRetainedRecoverable)
	}
	names := outcome.RetainedNames()
	if len(names) != 2 || names[0] != ".stage-a" || names[1] != ".stage-b" {
		t.Fatalf("RetainedNames = %q, want lexical private names", names)
	}
	names[0] = ".changed-again"
	if got := outcome.RetainedNames()[0]; got != ".stage-a" {
		t.Fatalf("RetainedNames after caller mutation = %q, want %q", got, ".stage-a")
	}

	for _, test := range []struct {
		name     string
		state    CommitOutcomeState
		retained []string
	}{
		{name: "missing state"},
		{name: "unknown state", state: "unknown"},
		{name: "uncommitted residue", state: CommitOutcomeUncommitted, retained: []string{".stage"}},
		{name: "complete residue", state: CommitOutcomeComplete, retained: []string{".stage"}},
		{name: "retained without residue", state: CommitOutcomeRetainedRecoverable},
		{name: "empty name", state: CommitOutcomeIndeterminate, retained: []string{""}},
		{name: "dot name", state: CommitOutcomeIndeterminate, retained: []string{"."}},
		{name: "parent name", state: CommitOutcomeIndeterminate, retained: []string{".."}},
		{name: "nested name", state: CommitOutcomeIndeterminate, retained: []string{"nested/stage"}},
		{name: "duplicate name", state: CommitOutcomeIndeterminate, retained: []string{".stage", ".stage"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewCommitOutcome(test.state, test.retained); err == nil {
				t.Fatal("NewCommitOutcome succeeded")
			}
		})
	}

	for _, state := range []CommitOutcomeState{
		CommitOutcomeUncommitted,
		CommitOutcomeIndeterminate,
		CommitOutcomeComplete,
	} {
		if _, err := NewCommitOutcome(state, nil); err != nil {
			t.Fatalf("NewCommitOutcome(%q, nil): %v", state, err)
		}
	}
	if _, err := NewCommitOutcome(
		CommitOutcomeIndeterminate,
		[]string{`legal-on-unix\entry`},
	); err != nil {
		t.Fatalf("NewCommitOutcome rejected opaque same-parent name: %v", err)
	}
}
