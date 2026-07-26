package extension_test

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestResolveLocalCollapsesLexicalAliasesWithoutFilesystemAccess(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	context, err := extensiontopology.NewLocalSourceContext(root, home)
	if err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(root, "packages", "tools")
	fileURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(canonical)}).String()
	localhostURL := "file://localhost" + (&url.URL{Path: filepath.ToSlash(canonical)}).EscapedPath()
	encodedSeparatorURL := strings.TrimSuffix(fileURL, "/packages/tools") + "/packages%2Ftools"
	upperSchemeURL := strings.Replace(fileURL, "file://", "FILE://", 1)

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "relative", source: filepath.Join("packages", ".", "tools"), want: canonical},
		{name: "absolute", source: canonical, want: canonical},
		{name: "file URL", source: fileURL, want: canonical},
		{name: "localhost file URL", source: localhostURL, want: canonical},
		{name: "encoded separator file URL", source: encodedSeparatorURL, want: canonical},
		{name: "upper-case scheme file URL", source: upperSchemeURL, want: canonical},
		{name: "home", source: filepath.Join("~", "tools"), want: filepath.Join(home, "tools")},
		{name: "missing path remains lexical", source: filepath.Join("missing", "..", "tools"), want: filepath.Join(root, "tools")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			identity, err := localCarrierSource(t, test.source).ResolveLocal(context)
			if err != nil {
				t.Fatal(err)
			}
			if identity.Path() != test.want {
				t.Fatalf("ResolveLocal().Path() = %q, want %q", identity.Path(), test.want)
			}
		})
	}
}

func TestResolveLocalRejectsUnsafeFileURLsAndInvalidContext(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	context, err := extensiontopology.NewLocalSourceContext(root, home)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "remote authority", source: "file://remote.example/package", want: "unsupported authority"},
		{name: "fragment", source: "file:///package#fragment", want: "unsupported authority"},
		{name: "empty path", source: "file://localhost", want: "path is required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := localCarrierSource(t, test.source).ResolveLocal(context)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ResolveLocal() error = %v, want containing %q", err, test.want)
			}
		})
	}

	if _, err := extensiontopology.NewLocalSourceContext("relative", home); err == nil {
		t.Fatal("NewLocalSourceContext accepted a relative base root")
	}
	if _, err := localCarrierSource(t, "tools").ResolveLocal(extensiontopology.LocalSourceContext{}); err == nil {
		t.Fatal("ResolveLocal accepted a zero context")
	}
}

func TestResolveLocalRejectsNonLocalCarrierSource(t *testing.T) {
	source := interpretedPiSource(t, "npm:@acme/tools@1.2.3")
	context, err := extensiontopology.NewLocalSourceContext(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.ResolveLocal(context); err == nil || !strings.Contains(err.Error(), "is not local") {
		t.Fatalf("ResolveLocal() error = %v, want non-local rejection", err)
	}
}

func localCarrierSource(t *testing.T, source string) extensiontopology.CarrierSource {
	t.Helper()
	interpreted := interpretedPiSource(t, source)
	if interpreted.Class() != extensiontopology.CarrierSourceLocal {
		t.Fatalf("source %q class = %q, want local", source, interpreted.Class())
	}
	return interpreted
}

func interpretedPiSource(t *testing.T, source string) extensiontopology.CarrierSource {
	t.Helper()
	ref, err := desiredextension.NewSourceRef(desiredextension.SourceKindHostSource, source)
	if err != nil {
		t.Fatalf("NewSourceRef(%q): %v", source, err)
	}
	key, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		target.ScopeGlobal,
		ref,
	)
	if err != nil {
		t.Fatalf("NewCarrierKey(%q): %v", source, err)
	}
	interpreted, err := extensiontopology.InterpretCarrierSource(key)
	if err != nil {
		t.Fatalf("InterpretCarrierSource(%q): %v", source, err)
	}
	return interpreted
}
