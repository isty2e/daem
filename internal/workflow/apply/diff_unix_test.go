//go:build darwin || linux

package apply

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output/hostpath"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestReadManagedFileForDiffRejectsPostPlanReplacement(t *testing.T) {
	for _, test := range []struct {
		name    string
		replace func(*testing.T, string, string)
	}{
		{
			name: "regular file",
			replace: func(t *testing.T, destination string, _ string) {
				t.Helper()
				if err := os.WriteFile(destination, []byte("private replacement\n"), 0o600); err != nil {
					t.Fatalf("replace destination: %v", err)
				}
			},
		},
		{
			name: "symlink",
			replace: func(t *testing.T, destination string, secret string) {
				t.Helper()
				if err := os.Remove(destination); err != nil {
					t.Fatalf("remove destination: %v", err)
				}
				if err := os.Symlink(secret, destination); err != nil {
					t.Fatalf("replace destination with symlink: %v", err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			destination := filepath.Join(root, "AGENTS.md")
			secret := filepath.Join(root, "secret.txt")
			before := []byte("planned content\n")
			if err := os.WriteFile(destination, before, 0o600); err != nil {
				t.Fatalf("write destination: %v", err)
			}
			if err := os.WriteFile(secret, []byte("private replacement\n"), 0o600); err != nil {
				t.Fatalf("write secret: %v", err)
			}
			captured, err := rootedpath.CaptureRoot(root)
			if err != nil {
				t.Fatalf("capture root: %v", err)
			}
			defer captured.Close()

			test.replace(t, destination, secret)
			content, err := readManagedFileForDiff(
				context.Background(),
				captured,
				target.ScopeProject,
				outputtest.Parse(t, "AGENTS.md"),
				artifact.HashFileContent(before),
				hostpath.NewResolver(root).Resolve,
			)
			if err == nil {
				t.Fatal("readManagedFileForDiff returned nil error after replacement")
			}
			if len(content) != 0 {
				t.Fatalf("readManagedFileForDiff returned replaced content %q", content)
			}
			if strings.Contains(err.Error(), "private replacement") {
				t.Fatalf("replacement content leaked through diagnostic: %v", err)
			}
		})
	}
}

func TestReadManagedFileForDiffPreservesExecutableIdentityClass(t *testing.T) {
	root := t.TempDir()
	before := []byte("planned content\n")
	destination := filepath.Join(root, "AGENTS.md")
	if err := os.WriteFile(destination, before, 0o700); err != nil {
		t.Fatalf("write executable destination: %v", err)
	}
	if err := os.Chmod(destination, 0o700); err != nil {
		t.Fatalf("set executable destination mode: %v", err)
	}
	captured, err := rootedpath.CaptureRoot(root)
	if err != nil {
		t.Fatalf("capture root: %v", err)
	}
	defer captured.Close()

	content, err := readManagedFileForDiff(
		context.Background(),
		captured,
		target.ScopeProject,
		outputtest.Parse(t, "AGENTS.md"),
		artifact.HashFileContentWithExecutable(before, true),
		hostpath.NewResolver(root).Resolve,
	)
	if err != nil {
		t.Fatalf("readManagedFileForDiff rejected unchanged executable identity: %v", err)
	}
	if string(content) != string(before) {
		t.Fatalf("readManagedFileForDiff content = %q, want %q", content, before)
	}
}
