package commit

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func (failure *failure) failedPhase() string { return failure.phase }

func (failure *failure) retainedResidue() []string {
	return append([]string(nil), failure.residue...)
}

func TestEntryIdentityKindMapsCanonicalForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		internal entryKind
		want     mutationfs.EntryKind
	}{
		{internal: entryKindInvalid, want: mutationfs.EntryKindInvalid},
		{internal: entryKindRegular, want: mutationfs.EntryKindFile},
		{internal: entryKindDirectory, want: mutationfs.EntryKindDirectory},
		{internal: entryKindSymlink, want: mutationfs.EntryKindSymlink},
	}
	for _, test := range tests {
		if got := (EntryIdentity{kind: test.internal}).Kind(); got != test.want {
			t.Fatalf("EntryIdentity kind %d maps to %q, want %q", test.internal, got, test.want)
		}
	}
}

func TestNewFileCreateRejectsInvalidPathsAndModes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		mode fs.FileMode
	}{
		{name: "empty", path: "", mode: 0o600},
		{name: "relative", path: "state.json", mode: 0o600},
		{name: "unclean", path: "/tmp/../tmp/state.json", mode: 0o600},
		{name: "root", path: "/", mode: 0o600},
		{name: "NUL", path: "/tmp/state\x00.json", mode: 0o600},
		{name: "special mode", path: "/tmp/state.json", mode: fs.ModeSetuid | 0o600},
		{name: "temporary namespace", path: "/tmp/.daem-tmp-owned", mode: 0o600},
		{name: "tombstone namespace", path: "/tmp/.daem-tombstone-owned", mode: 0o600},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewFileCreate(test.path, nil, test.mode); err == nil {
				t.Fatal("NewFileCreate returned nil error")
			}
		})
	}
}

func TestNewFileCreateCopiesPayload(t *testing.T) {
	t.Parallel()

	payload := []byte("before")
	request, err := NewFileCreate("/tmp/state.json", payload, 0o600)
	if err != nil {
		t.Fatalf("NewFileCreate returned error: %v", err)
	}
	payload[0] = 'X'
	if got := string(request.payload); got != "before" {
		t.Fatalf("request payload = %q, want before", got)
	}
}

func TestWriteAllHandlesShortWrites(t *testing.T) {
	t.Parallel()

	writer := &chunkWriter{limit: 2}
	if err := writeAllContext(context.Background(), writer, []byte("abcdef")); err != nil {
		t.Fatalf("writeAll returned error: %v", err)
	}
	if got := writer.output.String(); got != "abcdef" {
		t.Fatalf("output = %q, want abcdef", got)
	}

	if err := writeAllContext(context.Background(), zeroWriter{}, []byte("x")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("zero writer error = %v, want io.ErrShortWrite", err)
	}
}

func TestWriteAllContextStopsBetweenPartialWrites(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	writer := &cancelingWriter{cancel: cancel}
	err := writeAllContext(ctx, writer, []byte("abcdef"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("writeAllContext error = %v, want context.Canceled", err)
	}
	if got := writer.output.String(); got != "a" {
		t.Fatalf("output = %q, want one byte", got)
	}
}

func TestFailureResidueReturnsCopy(t *testing.T) {
	t.Parallel()

	failure := &failure{residue: []string{"/tmp/a"}}
	residue := failure.retainedResidue()
	residue[0] = "/tmp/changed"
	if got := failure.retainedResidue()[0]; got != "/tmp/a" {
		t.Fatalf("stored residue = %q, want /tmp/a", got)
	}
}

type chunkWriter struct {
	limit  int
	output strings.Builder
}

func (writer *chunkWriter) Write(payload []byte) (int, error) {
	if len(payload) > writer.limit {
		payload = payload[:writer.limit]
	}
	return writer.output.Write(payload)
}

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) { return 0, nil }

type cancelingWriter struct {
	cancel context.CancelFunc
	output strings.Builder
}

func (writer *cancelingWriter) Write(payload []byte) (int, error) {
	writer.cancel()
	return writer.output.Write(payload[:1])
}
