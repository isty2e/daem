//go:build darwin || linux

package record

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRecoveryJournalRejectsPermissiveMode(t *testing.T) {
	content, err := marshalRecoveryJournal(defaultRecoveryJournal(), testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, FileName)
	if err := os.WriteFile(path, content, Mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = loadRecoveryJournal(t.Context(), journalTestFilesystem(), path, testStateCodec())
	if err == nil || !strings.Contains(err.Error(), "permissions are 0644, want 0600") {
		t.Fatalf("loadRecoveryJournal error = %v, want private-mode rejection", err)
	}
}
