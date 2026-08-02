package readiness

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe/filesnapshot"
)

func TestReadAggregateDocumentEnforcesPhysicalReadLimit(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "hooks.json")
	if err := os.WriteFile(path, []byte("12345"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readAggregateDocument(t.Context(), path, 4); !errors.Is(err, filesnapshot.ErrLimitExceeded) {
		t.Fatalf("readAggregateDocument error = %v, want ErrLimitExceeded", err)
	}
}
