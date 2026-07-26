package rootedpath

import (
	"path"
	"path/filepath"
	"strings"
	"testing"
)

func FuzzRelativeDestinationNeverEscapesBoundRoot(f *testing.F) {
	for _, seed := range []string{
		".agents/skills/review",
		".agents//skills/./review/",
		"../outside",
		"inside/../../outside",
		"inside/.. /literal",
		"C:/outside",
		"z:drive-relative",
		"~",
		"~/global",
		"/absolute",
		`windows\path`,
		"unicode/스킬",
		"control\npath",
		"nul\x00path",
	} {
		f.Add(seed)
	}

	root := filepath.Join(f.TempDir(), "captured root")
	authority := mustAuthority(f, root, testIdentityToken(1), testIdentityToken(2))
	f.Fuzz(func(t *testing.T, input string) {
		relative, err := NewRelativeDestination(input)
		if err != nil {
			if !hasFailureKind(err, FailureInvalidDestination) {
				t.Fatalf("NewRelativeDestination(%q) error = %v, want typed invalid destination", input, err)
			}
			return
		}
		if err := relative.Validate(); err != nil {
			t.Fatalf("accepted relative destination %q failed validation: %v", relative.Path(), err)
		}
		if relative.Path() != path.Clean(relative.Path()) || path.IsAbs(relative.Path()) ||
			strings.Contains(relative.Path(), `\`) {
			t.Fatalf("accepted non-canonical relative destination %q", relative.Path())
		}
		for _, component := range strings.Split(relative.Path(), "/") {
			if component == ".." {
				t.Fatalf("accepted parent traversal in %q", relative.Path())
			}
		}

		destination, err := authority.Bind(relative)
		if err != nil {
			t.Fatalf("Bind(%q) returned error: %v", relative.Path(), err)
		}
		lexical, err := destination.LexicalPath()
		if err != nil {
			t.Fatalf("LexicalPath(%q) returned error: %v", relative.Path(), err)
		}
		fromRoot, err := filepath.Rel(root, lexical)
		if err != nil || fromRoot == "." || fromRoot == ".." || strings.HasPrefix(fromRoot, ".."+string(filepath.Separator)) {
			t.Fatalf("accepted destination %q escaped root %q as %q (relative %q, error %v)", input, root, lexical, fromRoot, err)
		}
	})
}

type authorityTest interface {
	Helper()
	Fatalf(string, ...any)
}

func mustAuthority(t authorityTest, root string, object identityToken, mount identityToken) Authority {
	t.Helper()
	authority, err := newCapturedAuthority(root, object, mount)
	if err != nil {
		t.Fatalf("newCapturedAuthority returned error: %v", err)
	}
	return authority
}
