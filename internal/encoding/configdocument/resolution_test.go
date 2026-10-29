package configdocument_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
)

func TestResolutionDistinguishesMissingPresentAndBlockedAcrossGrammars(t *testing.T) {
	for _, test := range []struct {
		syntax configdocument.Syntax
		input  string
	}{
		{configdocument.JSON, `{"a":{"scalar":1,"array":[1],"empty":{},"branch":{"leaf":2}}}`},
		{configdocument.JSONC, "/* keep */ {\"a\":{\"scalar\":1,\"array\":[1],\"empty\":{},\"branch\":{\"leaf\":2},},}"},
		{configdocument.JSONLineComments, "// keep\n{\"a\":{\"scalar\":1,\"array\":[1],\"empty\":{},\"branch\":{\"leaf\":2},},}"},
		{configdocument.TOML, "# keep\n[a]\nscalar=1\narray=[1]\nempty={}\n[a.branch]\nleaf=2\n"},
	} {
		document := parse(t, test.syntax, test.input)
		before, comments := document.Bytes(), document.Comments()
		for _, query := range []struct {
			pointer string
			blocked string
			missing bool
		}{
			{pointer: ""},
			{pointer: "/a/branch/leaf"},
			{pointer: "/a/empty"},
			{pointer: "/a/missing/deeper", missing: true},
			{pointer: "/a/empty/child", missing: true},
			{pointer: "/a/scalar/child/deeper", blocked: "/a/scalar"},
			{pointer: "/a/array/0", blocked: "/a/array"},
		} {
			key := path(t, query.pointer)
			resolution, err := document.Resolve(key)
			if err != nil || resolution.Absent() != query.missing {
				t.Fatalf("resolution changed missing/present meaning for %s %q: %v", test.syntax, query.pointer, err)
			}
			ancestor, blocked := resolution.BlockedBy()
			value, present := resolution.Value()
			if blocked != (query.blocked != "") || blocked && ancestor != path(t, query.blocked) || present != (!query.missing && !blocked) {
				t.Fatalf("resolution lost value or first obstruction for %s %q", test.syntax, query.pointer)
			}
			legacy, found, legacyErr := document.Lookup(key)
			if blocked {
				if found || !errors.Is(legacyErr, configdocument.ErrPathConflict) {
					t.Fatal("legacy lookup no longer rejects non-object traversal")
				}
			} else if legacyErr != nil || found != present || present && !legacy.Equal(value) {
				t.Fatal("resolution and legacy lookup disagree on native value/presence")
			}
		}
		if !bytes.Equal(before, document.Bytes()) || !reflect.DeepEqual(comments, document.Comments()) {
			t.Fatal("read-only resolution changed native document or lexical comments")
		}
	}
}

func TestResolutionRetainsLiteralBlockingAncestorKeys(t *testing.T) {
	for _, name := range []string{"", "/", "~1", ".", "..", "0", "한글", "control\n\x00"} {
		content, err := json.Marshal(map[string]int{name: 1})
		if err != nil {
			t.Fatal(err)
		}
		document, err := configdocument.Parse(t.Context(), content, configdocument.JSON, testLimits)
		if err != nil {
			t.Fatal(err)
		}
		key, err := output.NewKeyPath(name, "child")
		if err != nil {
			t.Fatal(err)
		}
		want, err := output.NewKeyPath(name)
		if err != nil {
			t.Fatal(err)
		}
		resolution, err := document.Resolve(key)
		if err != nil {
			t.Fatal(err)
		}
		ancestor, blocked := resolution.BlockedBy()
		if !blocked || ancestor != want || resolution.Absent() {
			t.Fatalf("native ancestor identity was normalized: %q", name)
		}
	}
}

func TestResolutionDistinguishesRootBlockerAbsentAndInvalidDocument(t *testing.T) {
	key := path(t, "/child")
	for _, input := range []string{"1", "[]", "null"} {
		document := parse(t, configdocument.JSON, input)
		resolution, err := document.Resolve(key)
		if err != nil {
			t.Fatal(err)
		}
		ancestor, blocked := resolution.BlockedBy()
		if !blocked || !ancestor.IsRoot() || resolution.Absent() {
			t.Fatal("root obstruction collapsed into no obstruction or missing property")
		}
		if _, found, err := document.Lookup(key); found || !errors.Is(err, configdocument.ErrPathConflict) {
			t.Fatal("legacy root traversal conflict changed")
		}
	}
	missing, err := configdocument.Absent(configdocument.JSON, testLimits)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := missing.Resolve(key)
	if err != nil || !resolution.Absent() {
		t.Fatal("absent document did not resolve to an absent property")
	}
	if _, err := (configdocument.Document{}).Resolve(key); !errors.Is(err, configdocument.ErrInvalidDocument) {
		t.Fatal("invalid zero document became an absent observation")
	}
}
