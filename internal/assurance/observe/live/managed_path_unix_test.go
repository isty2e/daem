//go:build darwin || linux

package live

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/test/outputtest"
	"golang.org/x/sys/unix"
)

func TestManagedPathEvidenceRejectsLinkAndSpecialFileDestinations(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeTestFile(t, outside, "outside.md", "outside\n")

	if err := os.Symlink(filepath.Join(outside, "outside.md"), filepath.Join(root, "final-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "missing.md"), filepath.Join(root, "dangling-link")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(root, "fifo"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		destination string
		want        string
	}{
		{name: "final symlink", destination: "final-link", want: "symlink"},
		{name: "dangling final symlink", destination: "dangling-link", want: "symlink"},
		{name: "fifo", destination: "fifo", want: "expected regular file"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := managedPathTestRequest(t, test.name, outputtest.Parse(t, test.destination), realization.PathProjectionFile)
			_, err := ManagedPathEvidence(context.Background(), managedPathTestResolver(root), []ManagedPathRequest{request})
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), test.want) {
				t.Fatalf("ManagedPathEvidence error = %v, want %q", err, test.want)
			}
		})
	}
}
