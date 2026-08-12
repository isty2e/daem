//go:build darwin || linux

package mutation

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRequiredAbsentRevisionDoesNotTraverseAppearedDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alternate.jsonc")
	revisions, err := CaptureRevisionSet(
		context.Background(),
		NewRequiredAbsentRevisionRequest(path),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(path, "must-not-observe"), 0o600); err != nil {
		t.Fatal(err)
	}

	matches, err := revisions.MatchesCurrent(context.Background())
	if err != nil {
		t.Fatalf("shallow absence revalidation traversed the appeared directory: %v", err)
	}
	if matches {
		t.Fatal("appeared directory preserved required-absent revision")
	}
}
