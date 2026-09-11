package authoring

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
)

func TestInstructionGitSourceInput(t *testing.T) {
	root := t.TempDir()
	for _, test := range []struct{ name, source, sourcePath, ref, wantGit, wantPath string }{
		{"https", "https://example.test/guidance.git", "docs/AGENTS.md", "main", "https://example.test/guidance.git", "docs/AGENTS.md"},
		{"shorthand", "owner/repo", "AGENTS.md", "main", "https://github.com/owner/repo.git", "AGENTS.md"},
		{"embedded", "owner/repo/docs/AGENTS.md", "", "main", "https://github.com/owner/repo.git", "docs/AGENTS.md"},
		{"scp", "git@example.test:guidance.git", "AGENTS.md", "main", "git@example.test:guidance.git", "AGENTS.md"},
		{"local repository", filepath.Join(root, "repo"), "file with spaces.md", "main", filepath.Join(root, "repo"), "file with spaces.md"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := InstructionFromAddRequest(AddInstructionRequest{Name: "project", SourceArg: test.source, SourcePath: test.sourcePath, Ref: test.ref}, root, declaration.ManifestHeader{})
			if err != nil {
				t.Fatal(err)
			}
			if got.Source.Git != test.wantGit || got.Source.Path != test.wantPath || got.Source.Ref != test.ref || got.Source.Mode != "" {
				t.Fatalf("source = %#v", got.Source)
			}
		})
	}
}

func TestInstructionGitSourceRejectsInvalidInput(t *testing.T) {
	root := t.TempDir()
	for _, test := range []struct{ name, source, sourcePath, ref, want string }{
		{"missing ref", "https://example.test/repo.git", "AGENTS.md", "", "--ref"},
		{"missing path", "https://example.test/repo.git", "", "main", "file path"},
		{"duplicate path", "owner/repo/docs/AGENTS.md", "other.md", "main", "do not combine"},
		{"escape", "owner/repo", "../AGENTS.md", "main", "path"},
		{"credentials", "https://user:secret@example.test/repo.git", "AGENTS.md", "main", "userinfo"},
		{"s3", "s3://bucket/AGENTS.md", "", "", "edit the manifest"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := InstructionFromAddRequest(AddInstructionRequest{Name: "project", SourceArg: test.source, SourcePath: test.sourcePath, Ref: test.ref}, root, declaration.ManifestHeader{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
			if test.name == "missing ref" && !errors.Is(err, ErrMissingGitRef) {
				t.Fatalf("missing ref classification: %v", err)
			}
		})
	}
}

func TestInstructionExistingColonPathRemainsLocal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not admit colon-bearing filenames")
	}
	root := t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile("notes:guide.md", []byte("local guidance"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := InstructionFromAddRequest(AddInstructionRequest{Name: "project", SourceArg: "notes:guide.md"}, root, declaration.ManifestHeader{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Source.Git != "" || got.Source.Path != "notes:guide.md" || got.Source.Mode != "vendor" {
		t.Fatalf("existing local file reinterpreted: %#v", got.Source)
	}
}

func TestInstructionBarePathsRemainLocal(t *testing.T) {
	root := t.TempDir()
	for _, input := range []string{"owner/repo/AGENTS.md", "missing.git", "./AGENTS.md"} {
		got, err := InstructionFromAddRequest(AddInstructionRequest{Name: "project", SourceArg: input}, root, declaration.ManifestHeader{})
		if err != nil {
			t.Fatal(err)
		}
		if got.Source.Git != "" || got.Source.Mode != "vendor" {
			t.Fatalf("%q reinterpreted: %#v", input, got.Source)
		}
	}
}
