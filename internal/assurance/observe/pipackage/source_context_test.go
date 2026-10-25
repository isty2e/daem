package pipackage_test

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	observepipackage "github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestPiSourceContextUsesCapturedLexicalRootsInsteadOfLiveHome(t *testing.T) {
	root := t.TempDir()
	base, home := filepath.Join(root, "absent-project"), filepath.Join(root, "captured-home")
	context, err := extensiontopology.NewLocalSourceContext(base, home)
	if err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(root, "package with space")
	fileURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(filePath)}).String()
	for _, liveHome := range []string{filepath.Join(root, "different-live-home"), ""} {
		t.Setenv("HOME", liveHome)
		t.Setenv("USERPROFILE", liveHome)
		for _, test := range []struct {
			name, source, absolute string
		}{
			{"relative", "./plugins/a", filepath.Join(base, "plugins", "a")},
			{"parent relative", "../sibling", filepath.Join(root, "sibling")},
			{"home", "~", home},
			{"home relative", "~/plugins/a", filepath.Join(home, "plugins", "a")},
			{"absolute", filePath, filePath},
			{"encoded file URL", fileURL, filePath},
		} {
			for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
				identity, err := observepipackage.HostLoadIdentityForContext(test.source, scope, context)
				if err != nil || identity != "local:"+string(scope)+":"+test.absolute {
					t.Fatalf("%s with explicit %s context: identity=%q error=%v", test.name, scope, identity, err)
				}
			}
		}
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			identity, err := observepipackage.HostLoadIdentityForInput("npm:tools@1", base, scope)
			if err != nil || identity != "npm:tools" {
				t.Fatalf("live non-local input acquired a home dependency: %q, %v", identity, err)
			}
		}
	}
}

func TestPiSourceContextAgreesWithLiveInterpreterUnderTheSameContext(t *testing.T) {
	base := t.TempDir()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	context, err := extensiontopology.NewLocalSourceContext(base, filepath.Clean(home))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, source string }{
		{"npm version", "npm:@acme/tools@1.0.0"},
		{"npm selector", "npm:tools@next"},
		{"npm alias operand", "npm:alias@npm:target@1"},
		{"git suffix ref", "https://github.com/acme/tools.git@v1"},
		{"git fragment ref", "git+https://github.com/acme/tools.git#v2"},
		{"encoded git path", "git+https://example.com/acme/100%25-tool.git#v1"},
		{"relative", "./plugins/a"},
		{"home relative", "~/plugins/a"},
		{"empty", ""},
		{"control character", "./plugin\nname"},
		{"remote file authority", "file://remote.example/package"},
		{"credential text", "--token=example-secret"},
	} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			live, liveErr := observepipackage.HostLoadIdentityForInput(test.source, base, scope)
			captured, capturedErr := observepipackage.HostLoadIdentityForContext(test.source, scope, context)
			if (liveErr == nil) != (capturedErr == nil) || (liveErr == nil && live != captured) {
				t.Fatalf("%s: live/captured identity or admission differed", test.name)
			}
			if capturedErr != nil && captured != "" {
				t.Fatalf("%s returned an identity with its error", test.name)
			}
		}
	}
}

func TestPiSourceContextKeepsIdentityPartitionsAndRefusesInvalidLocalEvidence(t *testing.T) {
	base := t.TempDir()
	first, err := extensiontopology.NewLocalSourceContext(base, filepath.Join(base, "first-home"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := extensiontopology.NewLocalSourceContext(base, filepath.Join(base, "second-home"))
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		left, leftErr := observepipackage.HostLoadIdentityForContext("~/package", scope, first)
		right, rightErr := observepipackage.HostLoadIdentityForContext("~/package", scope, second)
		if leftErr != nil || rightErr != nil || left == right {
			t.Fatalf("distinct captured homes collapsed: %v, %v", leftErr, rightErr)
		}
		for _, pair := range [][2]string{
			{"npm:@acme/tools@1.0.0", "npm:@acme/tools@2.0.0"},
			{"https://github.com/acme/tools.git@v1", "git:https://github.com/acme/tools@v2"},
		} {
			left, leftErr := observepipackage.HostLoadIdentityForContext(pair[0], scope, first)
			right, rightErr := observepipackage.HostLoadIdentityForContext(pair[1], scope, second)
			if leftErr != nil || rightErr != nil || left != right {
				t.Fatalf("non-local selector equivalence depended on local context: %v, %v", leftErr, rightErr)
			}
		}
	}
	for _, source := range []string{"./package", "~/package", "file://remote.example/package"} {
		if identity, err := observepipackage.HostLoadIdentityForContext(source, target.ScopeProject, extensiontopology.LocalSourceContext{}); err == nil || identity != "" {
			t.Fatal("invalid local context produced usable source identity")
		}
	}
	if identity, err := observepipackage.HostLoadIdentityForContext("npm:tools@1", target.Scope("invalid"), first); err == nil || identity != "" {
		t.Fatal("invalid scope produced usable source identity")
	}
}
