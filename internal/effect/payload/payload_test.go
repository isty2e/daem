package payload

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/topology"
)

func TestFilePayloadIsClosedImmutableEffectReadyContent(t *testing.T) {
	subject, err := topology.NewSubjectID(topology.SubjectProjection, "skill", "oracle")
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	content := []byte("oracle")
	value, err := NewFilePayload(subject, content, 0o700)
	if err != nil {
		t.Fatalf("NewFilePayload returned error: %v", err)
	}
	content[0] = 'O'

	if value.Subject() != subject {
		t.Fatalf("file payload subject = %#v, want %#v", value.Subject(), subject)
	}
	file, ok := value.File()
	if !ok {
		t.Fatal("File returned no file variant")
	}
	if _, ok := value.Directory(); ok {
		t.Fatal("Directory returned a variant for file payload")
	}
	if got := string(file.Bytes()); got != "oracle" {
		t.Fatalf("file bytes = %q, want immutable constructor input", got)
	}
	returned := file.Bytes()
	returned[0] = 'O'
	if got := string(file.Bytes()); got != "oracle" {
		t.Fatalf("file bytes = %q after caller mutation, want immutable output", got)
	}
	wantHash := artifact.HashFileContentWithExecutable([]byte("oracle"), true)
	if value.Hash() != wantHash || file.Hash() != wantHash || file.Mode() != 0o700 {
		t.Fatalf("file payload = hash %q mode %04o, want hash %q mode 0700", file.Hash(), file.Mode(), wantHash)
	}
}

func TestDirectoryPayloadRequiresExactDirectoryIdentityAndView(t *testing.T) {
	subject, err := topology.NewSubjectID(topology.SubjectProjection, "skill", "oracle")
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	view, err := access.OpenView(t.TempDir())
	if err != nil {
		t.Fatalf("OpenView returned error: %v", err)
	}
	hash, err := view.Hash(t.Context())
	if err != nil {
		t.Fatalf("View.Hash returned error: %v", err)
	}
	identity, err := artifact.NewExactIdentity("test:directory", "", artifact.ArtifactKindDirectory, hash)
	if err != nil {
		t.Fatalf("NewExactIdentity returned error: %v", err)
	}
	value, err := NewDirectoryPayload(t.Context(), subject, identity, view)
	if err != nil {
		t.Fatalf("NewDirectoryPayload returned error: %v", err)
	}

	if value.Hash() != hash {
		t.Fatalf("directory payload hash = %q, want %q", value.Hash(), hash)
	}
	directory, ok := value.Directory()
	if !ok || directory.Identity() != identity {
		t.Fatalf("Directory = %#v, found=%t, want exact identity", directory, ok)
	}
	if _, ok := value.File(); ok {
		t.Fatal("File returned a variant for directory payload")
	}
	if err := directory.View().Verify(t.Context(), directory.Identity()); err != nil {
		t.Fatalf("directory view does not verify exact identity: %v", err)
	}

	fileIdentity, err := artifact.NewExactIdentity(
		"test:file",
		"",
		artifact.ArtifactKindFile,
		artifact.HashFileContent(nil),
	)
	if err != nil {
		t.Fatalf("construct file identity: %v", err)
	}
	if _, err := NewDirectoryPayload(t.Context(), subject, fileIdentity, view); err == nil {
		t.Fatal("NewDirectoryPayload accepted a file identity")
	}
	staleIdentity, err := artifact.NewExactIdentity(
		"test:stale-directory",
		"",
		artifact.ArtifactKindDirectory,
		artifact.HashFileContent([]byte("not the directory view")),
	)
	if err != nil {
		t.Fatalf("construct stale directory identity: %v", err)
	}
	if _, err := NewDirectoryPayload(t.Context(), subject, staleIdentity, view); err == nil {
		t.Fatal("NewDirectoryPayload accepted a view that did not verify its identity")
	}
}

func TestPayloadSetRequiresOneValidPayloadPerUniqueSubject(t *testing.T) {
	subject, err := topology.NewSubjectID(topology.SubjectProjection, "skill", "oracle")
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	value, err := NewFilePayload(subject, []byte("oracle"), 0o600)
	if err != nil {
		t.Fatalf("NewFilePayload returned error: %v", err)
	}

	if _, err := NewPayloadSet([]Payload{{}}, nil); err == nil {
		t.Fatal("NewPayloadSet accepted a zero payload")
	}
	if _, err := NewPayloadSet([]Payload{value, value}, nil); err == nil {
		t.Fatal("NewPayloadSet accepted duplicate subject")
	}

	set, err := NewPayloadSet([]Payload{value}, nil)
	if err != nil {
		t.Fatalf("NewPayloadSet returned error: %v", err)
	}
	got, ok := set.LookupSubject(subject)
	if !ok || got.Subject() != subject {
		t.Fatalf("payload set = %#v, found=%t, want one subject-keyed payload", got, ok)
	}
}

func TestPayloadSetRejectsCorruptedInternalVariants(t *testing.T) {
	subject, err := topology.NewSubjectID(topology.SubjectProjection, "skill", "oracle")
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	valid, err := NewFilePayload(subject, []byte("oracle"), 0o600)
	if err != nil {
		t.Fatalf("NewFilePayload returned error: %v", err)
	}
	corruptFile := *valid.file
	corruptFile.content = []byte("different content")

	for _, test := range []struct {
		name  string
		value Payload
	}{
		{name: "zero", value: Payload{}},
		{name: "missing variant", value: Payload{subject: subject}},
		{name: "multiple variants", value: Payload{
			subject: subject,
			file:    valid.file,
			directory: &DirectoryPayload{
				identity: artifact.ExactIdentity{},
			},
		}},
		{name: "content hash mismatch", value: Payload{subject: subject, file: &corruptFile}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewPayloadSet([]Payload{test.value}, nil); err == nil {
				t.Fatalf("NewPayloadSet accepted %s payload", test.name)
			}
		})
	}
}

func TestPayloadVerifyHashRejectsPlannedMismatch(t *testing.T) {
	subject, err := topology.NewSubjectID(topology.SubjectProjection, "skill", "oracle")
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	value, err := NewFilePayload(subject, []byte("payload"), 0o600)
	if err != nil {
		t.Fatalf("NewFilePayload returned error: %v", err)
	}
	plannedHash := artifact.HashFileContent([]byte("planned"))
	if err := value.VerifyHash(plannedHash, "AGENTS.md"); err == nil {
		t.Fatal("VerifyHash accepted a mismatched planned hash")
	}
}

func TestPayloadSetCleanupIsReverseOrderedConcurrentAndErrorBearing(t *testing.T) {
	firstFailure := errors.New("release first")
	secondFailure := errors.New("release second")
	var calls atomic.Int32
	var orderMutex sync.Mutex
	var order []string
	cleanup := func(label string, result error) func() error {
		return func() error {
			calls.Add(1)
			orderMutex.Lock()
			order = append(order, label)
			orderMutex.Unlock()
			return result
		}
	}
	set, err := NewPayloadSet(nil, []func() error{
		cleanup("first", firstFailure),
		nil,
		cleanup("second", secondFailure),
	})
	if err != nil {
		t.Fatalf("NewPayloadSet returned error: %v", err)
	}

	const callers = 8
	errorsByCaller := make(chan error, callers)
	var wait sync.WaitGroup
	for range callers {
		wait.Go(func() {
			errorsByCaller <- set.Cleanup()
		})
	}
	wait.Wait()
	close(errorsByCaller)
	for cleanupErr := range errorsByCaller {
		if !errors.Is(cleanupErr, firstFailure) || !errors.Is(cleanupErr, secondFailure) {
			t.Fatalf("Cleanup error = %v, want both cleanup failures", cleanupErr)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("cleanup calls = %d, want 2", calls.Load())
	}
	orderMutex.Lock()
	defer orderMutex.Unlock()
	if len(order) != 2 || order[0] != "second" || order[1] != "first" {
		t.Fatalf("cleanup order = %v, want [second first]", order)
	}
}

func TestZeroPayloadSetCleanupIsNoOp(t *testing.T) {
	if err := (PayloadSet{}).Cleanup(); err != nil {
		t.Fatalf("zero PayloadSet Cleanup returned error: %v", err)
	}
}
