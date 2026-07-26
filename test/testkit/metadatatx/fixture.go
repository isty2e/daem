package metadatatx

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/effect/mutation"
)

// WriteInterrupted installs valid persisted evidence for a transaction whose
// target is intentionally absent.
func WriteInterrupted(t testing.TB, stateDir string) {
	WriteInterruptedForAbsentTarget(
		t,
		stateDir,
		filepath.Join(stateDir, "absent-test-target"),
	)
}

// WriteInterruptedForAbsentTarget installs a valid retained-target marker for
// one caller-selected path that does not exist.
func WriteInterruptedForAbsentTarget(
	t testing.TB,
	stateDir string,
	targetPath string,
) {
	t.Helper()
	authorityPath, err := transaction.FileSetAuthorityPath(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(authorityPath, 0o700); err != nil {
		t.Fatal(err)
	}
	targetPath, err = mutation.CanonicalDirectoryEntryPath(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	content := `{"version":1,"targets":[{"path":` + strconv.Quote(targetPath) +
		`,"before":{"exists":false},"write":false}]}`
	if err := os.WriteFile(
		filepath.Join(authorityPath, "transaction.json"),
		[]byte(content),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
}
