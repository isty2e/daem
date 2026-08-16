package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clipresent "github.com/isty2e/daem/internal/cli/present"
)

func TestMissingManifestInitHintRequiresCurrentMissingManifestEvidence(t *testing.T) {
	root := t.TempDir()
	existingPath := filepath.Join(root, "existing.toml")
	if err := os.WriteFile(existingPath, []byte("version = 1\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	directoryPath := filepath.Join(root, "manifest-directory")
	if err := os.Mkdir(directoryPath, 0o700); err != nil {
		t.Fatalf("Mkdir returned error: %v", err)
	}

	missingFailure := fmt.Errorf("nested input missing: %w", os.ErrNotExist)
	for _, test := range []struct {
		name         string
		manifestPath string
		err          error
		wantHint     bool
	}{
		{name: "manifest missing", manifestPath: filepath.Join(root, "missing.toml"), err: missingFailure, wantHint: true},
		{name: "manifest exists", manifestPath: existingPath, err: missingFailure},
		{name: "manifest path is directory", manifestPath: directoryPath, err: missingFailure},
		{name: "manifest state unknown", manifestPath: filepath.Join(root, strings.Repeat("x", 5000)), err: missingFailure},
		{name: "failure is not missing", manifestPath: filepath.Join(root, "missing.toml"), err: fmt.Errorf("permission denied")},
		{name: "nil failure", manifestPath: filepath.Join(root, "missing.toml")},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output strings.Builder
			printMissingManifestInitHint(&output, test.manifestPath, test.err)

			if !test.wantHint {
				if output.Len() != 0 {
					t.Fatalf("output = %q, want empty", output.String())
				}
				return
			}
			command, err := clipresent.ShellCommand("daem", "init", "--manifest", test.manifestPath, "--dry-run")
			if err != nil {
				t.Fatalf("ShellCommand returned error: %v", err)
			}
			want := "next: run " + command + "\n"
			if output.String() != want {
				t.Fatalf("output = %q, want %q", output.String(), want)
			}
		})
	}
}
