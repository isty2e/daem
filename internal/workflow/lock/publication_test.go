package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/effect/mutation"
	locksnapshot "github.com/isty2e/daem/internal/realization/lock"
)

func TestLockPublicationDoesNotReplaceAnEntryThatAppearedAfterObservation(t *testing.T) {
	for _, test := range []struct {
		name   string
		status PreviousLockStatus
	}{
		{"absent", PreviousMissing},
		{"damaged", PreviousDamaged},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			selected := filepath.Join(root, "daem.lock.toml")
			if test.status == PreviousDamaged {
				writeWorkflowTestFile(t, root, "daem.lock.toml", fmt.Sprintf("version = %d\n[[\n", locksnapshot.CurrentVersion))
			}
			revisions, err := mutation.CaptureBoundedFileRevisionSet(t.Context(), declarationartifact.MaximumBytes, selected)
			if err != nil {
				t.Fatal(err)
			}
			publication, previous, err := observeLockPublication(t.Context(), revisions, selected)
			if err != nil || previous.status != test.status {
				t.Fatalf("observed %q: %v", previous.status, err)
			}

			future := fmt.Sprintf("version = %d\n", locksnapshot.CurrentVersion+1)
			writeWorkflowTestFile(t, root, "arriving.toml", future)
			if err := os.Rename(filepath.Join(root, "arriving.toml"), selected); err != nil {
				t.Fatal(err)
			}
			// Exercise the last storage guard directly, without an earlier revision check.
			if err := publication.commit(t.Context(), []byte("replacement")); err == nil {
				t.Fatal("publication overwrote a competing entry")
			}
			if content, err := os.ReadFile(selected); err != nil || string(content) != future {
				t.Fatalf("competing entry changed: %q, %v", content, err)
			}
		})
	}
}
