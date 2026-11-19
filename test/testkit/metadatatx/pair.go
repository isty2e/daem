package metadatatx

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
)

// WriteInterruptedPair stages an exact version-3 record with both files
// replaced, or with the lockfile still at its before-image when unfinished.
func WriteInterruptedPair(t testing.TB, stateDir, manifest, lock, manifestAfter, lockAfter string, finished bool) string {
	t.Helper()
	type beforeImage struct {
		Exists bool   `json:"exists"`
		Hash   string `json:"hash"`
		Path   string `json:"backup_path"`
		Mode   uint32 `json:"mode"`
	}
	type row struct {
		Path  string      `json:"path"`
		Old   beforeImage `json:"before"`
		New   string      `json:"after_hash"`
		Write bool        `json:"write"`
	}
	canonical := func(path string) string {
		t.Helper()
		resolved, err := mutation.CanonicalDirectoryEntryPath(path)
		if err != nil {
			t.Fatal(err)
		}
		return resolved
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	evidence, err := fileset.FileSetAuthorityPath(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	hash := func(content string) string { return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(content))) }
	manifest, lock = canonical(manifest), canonical(lock)
	after := map[string]string{manifest: manifestAfter, lock: lockAfter}
	files := []string{manifest, lock}
	slices.Sort(files)
	rows := make([]row, 0, len(files))
	for index, path := range files {
		before := after[path] + "# before\n"
		backup := filepath.Join(evidence, fmt.Sprintf("target-%03d.before", index))
		write(backup, before)
		rows = append(rows, row{Path: path, Old: beforeImage{true, hash(before), backup, 0o600}, New: hash(after[path]), Write: true})
		content := after[path]
		if path == lock && !finished {
			content = before
		}
		write(path, content)
	}
	marker, err := json.Marshal(struct {
		Version int   `json:"version"`
		Targets []row `json:"targets"`
	}{contractversion.MetadataTransaction, rows})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(evidence, "transaction.json")
	write(path, string(marker))
	return path
}
