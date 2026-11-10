package configdocument_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
)

func TestProjectionPreservesUnchangedDescendantsAcrossNativeGrammars(t *testing.T) {
	for _, syntax := range []configdocument.Syntax{configdocument.JSON, configdocument.JSONC, configdocument.JSONLineComments, configdocument.TOML} {
		t.Run(syntax.String(), func(t *testing.T) {
			foreign := `"foreign": ["literal",1e+03]`
			original := `{"box":{"selected":false,` + foreign + `},"outside":true}`
			candidate := `{"box":{"selected":true,"foreign":["literal",1000]},"outside":false}`
			if syntax == configdocument.TOML {
				foreign = `foreign=['literal',1_000]`
				original = "# keep\noutside=true\n[box]\nselected=false\n" + foreign + "\n"
				candidate = "outside=false\n[box]\nselected=true\nforeign=[\"literal\",1000]\n"
			} else if syntax != configdocument.JSON {
				original = "// keep\n" + original
			}
			before := parse(t, syntax, original)
			after := parse(t, syntax, candidate)
			areas := []output.KeyPath{path(t, "/box")}
			changed, err := configdocument.Project(t.Context(), before, []configdocument.Projection{{Document: after, Areas: areas}})
			if err != nil {
				t.Fatal(err)
			}
			if !lookup(t, changed, "/box/selected").Equal(lookup(t, after, "/box/selected")) ||
				!lookup(t, changed, "/outside").Equal(lookup(t, before, "/outside")) {
				t.Fatal("projection did not confine its semantic image")
			}
			restored, err := configdocument.Project(t.Context(), changed, []configdocument.Projection{{Document: before, Areas: areas}})
			if err != nil {
				t.Fatal(err)
			}
			for _, actual := range []configdocument.Document{changed, restored} {
				if !strings.Contains(string(actual.Bytes()), foreign) {
					t.Fatal("projection rewrote an unchanged descendant")
				}
				assertComments(t, before, actual)
			}
			assertEqual(t, lookup(t, before, ""), lookup(t, restored, ""))
		})
	}
}

func TestProjectionRetainsStructuralParentTransitions(t *testing.T) {
	for _, test := range []struct{ before, after string }{
		{`{"a":false}`, `{"a":{"child":true}}`},
		{`{"a":{"child":true}}`, `{"a":false}`},
		{`{"a":{"child":true}}`, `{"a":{}}`},
		{`{}`, `{"a":{"child":true}}`},
		{`{"a":{"child":true}}`, `{}`},
	} {
		before := parse(t, configdocument.JSON, test.before)
		after := parse(t, configdocument.JSON, test.after)
		actual, err := configdocument.Project(t.Context(), before, []configdocument.Projection{{Document: after, Areas: []output.KeyPath{path(t, "/a")}}})
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, lookup(t, actual, ""), lookup(t, after, ""))
	}
}

func TestProjectionPreservesUnchangedInlineTableMembers(t *testing.T) {
	before := parse(t, configdocument.TOML, "box = { selected = false, keep = 'native', count = 0x1f }\n")
	after := parse(t, configdocument.TOML, "box = { selected = true, keep = \"native\", count = 31 }\n")
	actual, err := configdocument.Project(t.Context(), before, []configdocument.Projection{{Document: after, Areas: []output.KeyPath{path(t, "/box")}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(actual.Bytes()), "keep = 'native', count = 0x1f") {
		t.Fatal("changed inline member rewrote its unchanged native siblings")
	}
	assertEqual(t, lookup(t, actual, ""), lookup(t, after, ""))
}

func TestProjectionRejectsInvalidScopesBeforeReturningAnImage(t *testing.T) {
	before := parse(t, configdocument.JSON, `{"a":false}`)
	after := parse(t, configdocument.JSON, `{"a":true}`)
	otherLimits := testLimits
	otherLimits.MaximumBytes *= 2
	unrelatedBudget, err := configdocument.Parse(t.Context(), []byte(`{"a":true}`), configdocument.JSON, otherLimits)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		images []configdocument.Projection
		reason error
	}{
		{"zero source", []configdocument.Projection{{Areas: []output.KeyPath{path(t, "/a")}}}, configdocument.ErrInvalidDocument},
		{"empty scope", []configdocument.Projection{{Document: after}}, configdocument.ErrInvalidDocument},
		{"root", []configdocument.Projection{{Document: after, Areas: []output.KeyPath{{}}}}, configdocument.ErrInvalidValue},
		{"duplicate", []configdocument.Projection{{Document: after, Areas: []output.KeyPath{path(t, "/a"), path(t, "/a")}}}, configdocument.ErrOverlappingChange},
		{"ancestor", []configdocument.Projection{{Document: after, Areas: []output.KeyPath{path(t, "/a"), path(t, "/a/child")}}}, configdocument.ErrOverlappingChange},
		{"cross-input overlap", []configdocument.Projection{{Document: after, Areas: []output.KeyPath{path(t, "/a")}}, {Document: after, Areas: []output.KeyPath{path(t, "/a/child")}}}, configdocument.ErrOverlappingChange},
		{"grammar", []configdocument.Projection{{Document: parse(t, configdocument.TOML, "a=true\n"), Areas: []output.KeyPath{path(t, "/a")}}}, configdocument.ErrInvalidDocument},
		{"operation budget", []configdocument.Projection{{Document: unrelatedBudget, Areas: []output.KeyPath{path(t, "/a")}}}, configdocument.ErrInvalidDocument},
		{"scope depth", []configdocument.Projection{{Document: after, Areas: []output.KeyPath{path(t, strings.Repeat("/a", testLimits.MaximumDepth+1))}}}, configdocument.ErrLimitExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			actual, err := configdocument.Project(t.Context(), before, test.images)
			if !errors.Is(err, test.reason) || actual.Exists() {
				t.Fatal("invalid projection returned an image", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := configdocument.Project(ctx, before, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("projection lost cancellation", err)
	}
}

func TestProjectionSharesExistingPrefixesAcrossMixedChanges(t *testing.T) {
	before := parse(t, configdocument.JSONC, `{"p":{/* removed */"a":{"v":1},"b":2,"outside":true}}`)
	first := parse(t, configdocument.JSONC, `{"p":{"b":3}}`)
	second := parse(t, configdocument.JSONC, `{"p":{"q":{"c":4}}}`)
	actual, err := configdocument.Project(t.Context(), before, []configdocument.Projection{
		{Document: first, Areas: []output.KeyPath{path(t, "/p/a"), path(t, "/p/b")}},
		{Document: second, Areas: []output.KeyPath{path(t, "/p/q/c")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	expected := parse(t, configdocument.JSONC, `{"p":{"b":3,"outside":true,"q":{"c":4}}}`)
	assertEqual(t, lookup(t, actual, ""), lookup(t, expected, ""))
	assertComments(t, before, actual)
}

func TestProjectionRefusesScalarAndArrayAncestors(t *testing.T) {
	for _, source := range []string{`{"a":false}`, `{"a":[]}`} {
		before := parse(t, configdocument.JSON, source)
		after := parse(t, configdocument.JSON, `{"a":{"child":true}}`)
		actual, err := configdocument.Project(t.Context(), before, []configdocument.Projection{{Document: after, Areas: []output.KeyPath{path(t, "/a/child")}}})
		if !errors.Is(err, configdocument.ErrPathConflict) || actual.Exists() {
			t.Fatal("projection replaced an ancestor outside its area", err)
		}
	}
}

func TestProjectionIsStableAcrossDisjointInputOrderAndNoop(t *testing.T) {
	original, err := configdocument.Absent(configdocument.JSON, testLimits)
	if err != nil {
		t.Fatal(err)
	}
	first := configdocument.Projection{Document: parse(t, configdocument.JSON, `{"a":{"v":true}}`), Areas: []output.KeyPath{path(t, "/a")}}
	second := configdocument.Projection{Document: parse(t, configdocument.JSON, `{"b":false}`), Areas: []output.KeyPath{path(t, "/b")}}
	left, err := configdocument.Project(t.Context(), original, []configdocument.Projection{first, second})
	if err != nil {
		t.Fatal(err)
	}
	right, err := configdocument.Project(t.Context(), original, []configdocument.Projection{second, first})
	if err != nil || !bytes.Equal(left.Bytes(), right.Bytes()) {
		t.Fatal("disjoint input order changed the native image", err)
	}
	stable, err := configdocument.Project(t.Context(), left, []configdocument.Projection{{Document: left, Areas: []output.KeyPath{path(t, "/a"), path(t, "/b")}}})
	if err != nil || !bytes.Equal(left.Bytes(), stable.Bytes()) {
		t.Fatal("no-op projection rewrote its native image", err)
	}
}

func TestProjectionAcceptsBoundedImagesWithoutExpandedPathBudget(t *testing.T) {
	prefix := strings.Repeat("p", 1<<17)
	var before, after strings.Builder
	fmt.Fprintf(&before, "{%q:{", prefix)
	fmt.Fprintf(&after, "{%q:{", prefix)
	for index := range 200 {
		if index != 0 {
			before.WriteByte(',')
			after.WriteByte(',')
		}
		fmt.Fprintf(&before, "%q:false", fmt.Sprintf("key-%d", index))
		fmt.Fprintf(&after, "%q:true", fmt.Sprintf("key-%d", index))
	}
	before.WriteString("}}")
	after.WriteString("}}")
	original := parse(t, configdocument.JSON, before.String())
	expected := parse(t, configdocument.JSON, after.String())
	area, err := output.NewKeyPath(prefix)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := configdocument.Project(t.Context(), original, []configdocument.Projection{{Document: expected, Areas: []output.KeyPath{area}}})
	if err != nil {
		t.Fatal("bounded native image was subjected to a flattened-path limit", err)
	}
	assertEqual(t, lookup(t, actual, ""), lookup(t, expected, ""))
}
