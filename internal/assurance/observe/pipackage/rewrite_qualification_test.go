package pipackage_test

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"

	observepipackage "github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestPackageRewriteQualifiesOnlySelectedIdentityRows(t *testing.T) {
	for _, test := range []struct {
		name, before, after string
		preserved           bool
	}{
		{"append string", `{"packages":["npm:other"]}`, `{"packages":["npm:other","npm:tools@2"]}`, true},
		{"prepend object", `{"packages":["npm:other"]}`, `{"packages":[{"source":"npm:tools@2","extensions":[]},"npm:other"]}`, true},
		{"remove string", `{"packages":["npm:tools@1","npm:other"]}`, `{"packages":["npm:other"]}`, true},
		{"remove object", `{"packages":["npm:other",{"source":"npm:tools@1","skills":["a"]}]}`, `{"packages":["npm:other"]}`, true},
		{"update string version", `{"packages":["npm:tools@1","npm:other"]}`, `{"packages":["npm:tools@2","npm:other"]}`, true},
		{"update object options", `{"packages":[{"source":"npm:tools@1","skills":["a"]},"npm:other"]}`, `{"packages":[{"source":"npm:tools@2","extensions":false},"npm:other"]}`, true},
		{"replace row shape and move", `{"packages":["npm:tools@1","npm:other","npm:last"]}`, `{"packages":["npm:other","npm:last",{"source":"npm:tools@2","skills":[]}]}`, true},
		{"replace object with string", `{"packages":[{"source":"npm:tools@1","skills":false},"npm:other"]}`, `{"packages":["npm:other","npm:tools@2"]}`, true},
		{"different source identity", `{"packages":["npm:tools@1","npm:other"]}`, `{"packages":["npm:different","npm:other"]}`, false},
		{"different source class", `{"packages":["npm:tools@1"]}`, `{"packages":["./tools"]}`, false},
		{"change unrelated version", `{"packages":["npm:other@1","npm:tools@1"]}`, `{"packages":["npm:other@2","npm:tools@2"]}`, false},
		{"change unrelated object options", `{"packages":[{"source":"npm:other","skills":["a","b"]},"npm:tools@1"]}`, `{"packages":[{"source":"npm:other","skills":["b","a"]},"npm:tools@2"]}`, false},
		{"reorder unrelated rows", `{"packages":["npm:other","npm:tools@1","npm:last"]}`, `{"packages":["npm:last","npm:tools@2","npm:other"]}`, false},
		{"drop unrelated row", `{"packages":["npm:other","npm:tools@1"]}`, `{"packages":["npm:tools@2"]}`, false},
		{"add unrelated row", `{"packages":["npm:tools@1"]}`, `{"packages":["npm:other","npm:tools@2"]}`, false},
		{"drop unrelated while adding selected", `{"packages":["npm:other"]}`, `{"packages":["npm:tools@2"]}`, false},
		{"add unrelated while removing selected", `{"packages":["npm:tools@1"]}`, `{"packages":["npm:other"]}`, false},
		{"no selected rows preserves unrelated", `{"packages":["npm:other"]}`, `{"packages":["npm:other"]}`, true},
		{"no selected rows cannot change unrelated", `{"packages":["npm:other@1"]}`, `{"packages":["npm:other@2"]}`, false},
		{"selected duplicates can all change", `{"packages":["npm:tools@1","npm:other",{"source":"npm:tools@2","skills":false}]}`, `{"packages":[{"source":"npm:tools@3"},"npm:tools@4","npm:other","npm:tools@5"]}`, true},
		{"selected duplicates can all disappear", `{"packages":["npm:tools@1","npm:other","npm:tools@2"]}`, `{"packages":["npm:other"]}`, true},
		{"keep unrelated duplicates", `{"packages":["npm:other","npm:tools@1","npm:other"]}`, `{"packages":["npm:other","npm:other","npm:tools@2"]}`, true},
		{"drop unrelated duplicate", `{"packages":["npm:other","npm:tools@1","npm:other"]}`, `{"packages":["npm:other","npm:tools@2"]}`, false},
		{"add unrelated duplicate", `{"packages":["npm:other","npm:tools@1"]}`, `{"packages":["npm:other","npm:other","npm:tools@2"]}`, false},
		{"reorder unequal unrelated duplicates", `{"packages":[{"source":"npm:other","skills":["a"]},"npm:tools@1",{"source":"npm:other","skills":["b"]}]}`, `{"packages":[{"source":"npm:other","skills":["b"]},"npm:tools@2",{"source":"npm:other","skills":["a"]}]}`, false},
		{"formatting and object key order", `{"packages":[{"source":"npm:other","options":{"b":2,"a":1}},"npm:tools@1"],"n":1000}`, "{\n\"n\":1e3, \"packages\":[\"npm:tools@2\",{\"options\":{\"a\":1,\"b\":2},\"source\":\"npm:other\"}]}\n", true},
		{"foreign empty values preserved", `{"packages":["npm:tools@1"],"empty":[]}`, `{"packages":["npm:tools@2"],"empty":[]}`, true},
		{"foreign empty values cannot disappear", `{"packages":["npm:tools@1"],"empty":[]}`, `{"packages":["npm:tools@2"]}`, false},
		{"foreign empty values cannot change type", `{"packages":["npm:tools@1"],"empty":[]}`, `{"packages":["npm:tools@2"],"empty":{}}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			before, after := rewriteDocument(t, test.before), rewriteDocument(t, test.after)
			preserved, err := observepipackage.PreservesOutsidePackageRewrite(t.Context(), before, after,
				"npm:tools@selected", target.ScopeProject, rewriteSourceContext(t))
			if err != nil || preserved != test.preserved {
				t.Fatalf("preservation=%v, want %v; error=%v", preserved, test.preserved, err)
			}
			if string(before.Bytes()) != test.before || string(after.Bytes()) != test.after {
				t.Fatal("qualification mutated a loaded document")
			}
		})
	}
}

func TestPackageRewriteKeepsNecessaryContainerTransitionsPaired(t *testing.T) {
	states := []struct {
		name, property string
		selected       bool
	}{
		{"absent", "", false},
		{"empty", `,"packages":[]`, false},
		{"selected", `,"packages":["npm:tools@1"]`, true},
		{"selected duplicates", `,"packages":["npm:tools@1",{"source":"npm:tools@2"}]`, true},
	}
	for _, before := range states {
		for _, after := range states {
			t.Run(before.name+" to "+after.name, func(t *testing.T) {
				left := rewriteDocument(t, `{"native":{"keep":[1,true,null]}`+before.property+`}`)
				right := rewriteDocument(t, `{"native":{"keep":[1,true,null]}`+after.property+`}`)
				want := before.name == after.name || before.selected || after.selected
				preserved, err := observepipackage.PreservesOutsidePackageRewrite(t.Context(), left, right,
					"npm:tools", target.ScopeProject, rewriteSourceContext(t))
				if err != nil || preserved != want {
					t.Fatalf("preservation=%v, want %v; error=%v", preserved, want, err)
				}

				changed := rewriteDocument(t, `{"native":{"keep":[1,false,null]}`+after.property+`}`)
				preserved, err = observepipackage.PreservesOutsidePackageRewrite(t.Context(), left, changed,
					"npm:tools", target.ScopeProject, rewriteSourceContext(t))
				if err != nil || preserved {
					t.Fatalf("container transition hid an unrelated root change: preserved=%v, error=%v", preserved, err)
				}
			})
		}
	}
}

func TestPackageRewriteHandlesMissingDocumentsWithoutOwningOtherValues(t *testing.T) {
	for _, test := range []struct {
		name, content string
		preserved     bool
	}{
		{"missing", "", true},
		{"empty root", `{}`, true},
		{"selected only", `{"packages":["npm:tools@1"]}`, true},
		{"selected duplicates only", `{"packages":["npm:tools@1",{"source":"npm:tools@2"}]}`, true},
		{"unjustified empty array", `{"packages":[]}`, false},
		{"foreign root value", `{"packages":["npm:tools@1"],"native":true}`, false},
		{"unrelated package", `{"packages":["npm:other"]}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			missing, document := rewriteDocument(t, ""), rewriteDocument(t, test.content)
			for _, pair := range [][2]configdocument.Document{{missing, document}, {document, missing}} {
				preserved, err := observepipackage.PreservesOutsidePackageRewrite(t.Context(), pair[0], pair[1],
					"npm:tools", target.ScopeProject, rewriteSourceContext(t))
				if err != nil || preserved != test.preserved {
					t.Fatalf("preservation=%v, want %v; error=%v", preserved, test.preserved, err)
				}
			}
		})
	}
}

func TestPackageRewriteUsesCanonicalGitIdentity(t *testing.T) {
	before := rewriteDocument(t, `{"packages":["https://github.com/acme/tools.git@v1","npm:other"]}`)
	after := rewriteDocument(t, `{"packages":["npm:other",{"source":"git:https://github.com/acme/tools@v2"}]}`)
	preserved, err := observepipackage.PreservesOutsidePackageRewrite(t.Context(), before, after,
		"git+https://github.com/acme/tools.git#selected", target.ScopeGlobal, rewriteSourceContext(t))
	if err != nil || !preserved {
		t.Fatalf("equivalent Git spellings failed: preserved=%v, error=%v", preserved, err)
	}
	for _, source := range []string{"git:GitHub.com/acme/tools@v2", "git:github.com/acme/%74ools@v2"} {
		after := rewriteDocument(t, `{"packages":[`+strconv.Quote(source)+`,"npm:other"]}`)
		preserved, err := observepipackage.PreservesOutsidePackageRewrite(t.Context(), before, after,
			"git:github.com/acme/tools", target.ScopeGlobal, rewriteSourceContext(t))
		if err != nil || preserved {
			t.Fatalf("distinct Git identity was admitted: preserved=%v, error=%v", preserved, err)
		}
	}
}

func TestPackageRewriteUsesSuppliedLocalContext(t *testing.T) {
	root := t.TempDir()
	base, home := filepath.Join(root, "captured-base"), filepath.Join(root, "captured-home")
	sourceContext, err := extensiontopology.NewLocalSourceContext(base, home)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	for _, test := range []struct{ name, selected, before, after string }{
		{"relative", filepath.Join(base, "tools"), "./tools", "./tools/../tools/."},
		{"home", filepath.Join(home, "tools"), "~/tools", "~/tools/."},
		{"file URL", filepath.Join(base, "tool with space"), "./tool with space", (&url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(base, "tool with space"))}).String()},
	} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			t.Run(test.name+" "+string(scope), func(t *testing.T) {
				before := rewriteDocument(t, `{"packages":[`+strconv.Quote(test.before)+`,"npm:other"]}`)
				after := rewriteDocument(t, `{"packages":["npm:other",{"source":`+strconv.Quote(test.after)+`,"extensions":false}]}`)
				preserved, err := observepipackage.PreservesOutsidePackageRewrite(t.Context(), before, after,
					test.selected, scope, sourceContext)
				if err != nil || !preserved {
					t.Fatalf("captured local context was not used: preserved=%v, error=%v", preserved, err)
				}

				otherContext, err := extensiontopology.NewLocalSourceContext(filepath.Join(root, "other-base"), filepath.Join(root, "other-home"))
				if err != nil {
					t.Fatal(err)
				}
				preserved, err = observepipackage.PreservesOutsidePackageRewrite(t.Context(), before, after,
					test.selected, scope, otherContext)
				if err != nil || preserved {
					t.Fatalf("different lexical roots collapsed: preserved=%v, error=%v", preserved, err)
				}
			})
		}
	}
}

func TestPackageRewriteRefusesInvalidEvidenceOnEitherSide(t *testing.T) {
	for _, test := range []struct {
		name, content string
		syntax        configdocument.Syntax
	}{
		{"packages null", `{"packages":null}`, configdocument.JSON},
		{"packages object", `{"packages":{}}`, configdocument.JSON},
		{"packages string", `{"packages":"npm:tools"}`, configdocument.JSON},
		{"packages boolean", `{"packages":true}`, configdocument.JSON},
		{"row null", `{"packages":[null]}`, configdocument.JSON},
		{"row number", `{"packages":[42]}`, configdocument.JSON},
		{"row array", `{"packages":[[]]}`, configdocument.JSON},
		{"object without source", `{"packages":[{}]}`, configdocument.JSON},
		{"non-string source", `{"packages":[{"source":1}]}`, configdocument.JSON},
		{"empty source", `{"packages":[""]}`, configdocument.JSON},
		{"untrimmed source", `{"packages":[" npm:tools"]}`, configdocument.JSON},
		{"source control", `{"packages":["npm:tools\n"]}`, configdocument.JSON},
		{"unsafe local source", `{"packages":["npm:tools","file://remote.example/package"]}`, configdocument.JSON},
		{"invalid source grammar", `{"packages":["npm:tools","--token=example"]}`, configdocument.JSON},
		{"array root", `[]`, configdocument.JSON},
		{"scalar root", `null`, configdocument.JSON},
		{"non-strict comments", `{"packages":["npm:tools"] /* foreign */}`, configdocument.JSONC},
		{"non-strict trailing comma", `{"packages":["npm:tools"],}`, configdocument.JSONC},
		{"wrong grammar", "packages = ['npm:tools']\n", configdocument.TOML},
	} {
		t.Run(test.name, func(t *testing.T) {
			invalid := rewriteDocumentSyntax(t, test.content, test.syntax)
			valid := rewriteDocument(t, `{"packages":["npm:tools"]}`)
			for _, pair := range [][2]configdocument.Document{{invalid, valid}, {valid, invalid}} {
				preserved, err := observepipackage.PreservesOutsidePackageRewrite(t.Context(), pair[0], pair[1],
					"npm:tools", target.ScopeProject, rewriteSourceContext(t))
				if err == nil || preserved {
					t.Fatalf("invalid evidence qualified: preserved=%v, error=%v", preserved, err)
				}
			}
		})
	}
}

func TestPackageRewriteRefusesInvalidInputs(t *testing.T) {
	document := rewriteDocument(t, `{"packages":["npm:tools"]}`)
	sourceContext := rewriteSourceContext(t)
	for _, selected := range []string{"", " npm:tools", "file://remote.example/package", "--token=example"} {
		preserved, err := observepipackage.PreservesOutsidePackageRewrite(t.Context(), document, document,
			selected, target.ScopeProject, sourceContext)
		if err == nil || preserved {
			t.Fatalf("invalid selected source qualified: preserved=%v, error=%v", preserved, err)
		}
	}
	for _, pair := range [][2]configdocument.Document{{{}, document}, {document, {}}, {{}, {}}} {
		if preserved, err := observepipackage.PreservesOutsidePackageRewrite(t.Context(), pair[0], pair[1],
			"npm:tools", target.ScopeProject, sourceContext); err == nil || preserved {
			t.Fatalf("uninitialized document qualified: preserved=%v, error=%v", preserved, err)
		}
	}
	if preserved, err := observepipackage.PreservesOutsidePackageRewrite(t.Context(), document, document,
		"npm:tools", target.Scope("invalid"), sourceContext); err == nil || preserved {
		t.Fatalf("invalid scope qualified: preserved=%v, error=%v", preserved, err)
	}
	local := rewriteDocument(t, `{"packages":["./tools"]}`)
	for _, pair := range [][2]configdocument.Document{{local, document}, {document, local}} {
		if preserved, err := observepipackage.PreservesOutsidePackageRewrite(t.Context(), pair[0], pair[1],
			"npm:tools", target.ScopeProject, extensiontopology.LocalSourceContext{}); err == nil || preserved {
			t.Fatalf("unresolved observed local source qualified: preserved=%v, error=%v", preserved, err)
		}
	}
	if preserved, err := observepipackage.PreservesOutsidePackageRewrite(t.Context(), document, document,
		"./tools", target.ScopeProject, extensiontopology.LocalSourceContext{}); err == nil || preserved {
		t.Fatalf("unresolved selected local source qualified: preserved=%v, error=%v", preserved, err)
	}
	if preserved, err := observepipackage.PreservesOutsidePackageRewrite(nil, document, document,
		"npm:tools", target.ScopeProject, sourceContext); err == nil || preserved {
		t.Fatalf("nil context qualified: preserved=%v, error=%v", preserved, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if preserved, err := observepipackage.PreservesOutsidePackageRewrite(ctx, document, document,
		"npm:tools", target.ScopeProject, sourceContext); !errors.Is(err, context.Canceled) || preserved {
		t.Fatalf("cancelled context qualified: preserved=%v, error=%v", preserved, err)
	}
}

func rewriteDocument(t *testing.T, content string) configdocument.Document {
	t.Helper()
	return rewriteDocumentSyntax(t, content, configdocument.JSON)
}

func rewriteDocumentSyntax(t *testing.T, content string, syntax configdocument.Syntax) configdocument.Document {
	t.Helper()
	limits := configdocument.Limits{MaximumBytes: observepipackage.MaximumSettingsBytes, MaximumDepth: 32}
	var document configdocument.Document
	var err error
	if content == "" {
		document, err = configdocument.Absent(syntax, limits)
	} else {
		document, err = configdocument.Parse(t.Context(), []byte(content), syntax, limits)
	}
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func rewriteSourceContext(t *testing.T) extensiontopology.LocalSourceContext {
	t.Helper()
	root := t.TempDir()
	sourceContext, err := extensiontopology.NewLocalSourceContext(filepath.Join(root, "base"), filepath.Join(root, "home"))
	if err != nil {
		t.Fatal(err)
	}
	return sourceContext
}
