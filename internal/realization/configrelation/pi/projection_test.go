package pi_test

import (
	"bytes"
	"testing"

	piconfig "github.com/isty2e/daem/internal/realization/configrelation/pi"
)

func TestPackageArrayEqualityKeepsLiteralProjectionAndPresence(t *testing.T) {
	for _, test := range []struct {
		name, left, right string
		equal, present    bool
	}{
		{"absent", `{"theme":"old"}`, `{"theme":"new"}`, true, false},
		{"empty", `{"packages":[]}`, `{"theme":"new","packages":[]}`, true, true},
		{"absent versus empty", `{}`, `{"packages":[]}`, false, false},
		{"property spelling and root layout", `{"packages": ["npm:a@1"]}`, "{\n\"theme\":true,\n\"\\u0070ackages\":[\"npm:a@1\"]\n}", true, true},
		{"row options", `{"packages":[{"source":"npm:a@1","autoload":true}]}`, `{"packages":[{"source":"npm:a@1","autoload":false}]}`, false, true},
		{"option number spelling", `{"packages":[{"source":"npm:a@1","n":1e999}]}`, `{"packages":[{"source":"npm:a@1","n":10e998}]}`, false, true},
		{"source escape spelling", `{"packages":["npm:a@1"]}`, `{"packages":["npm:\u0061@1"]}`, false, true},
		{"array slot whitespace", `{"packages":["npm:a@1", "npm:b@1"]}`, `{"packages":["npm:a@1","npm:b@1"]}`, false, true},
		{"empty slot whitespace", `{"packages":[]}`, `{"packages":[ ]}`, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			left := parseProjection(t, []byte(test.left))
			right := parseProjection(t, []byte(test.right))
			if left.HasPackages() != test.present || left.PackageArrayEqual(right) != test.equal || right.PackageArrayEqual(left) != test.equal {
				t.Fatal("package comparison lost literal array or presence evidence")
			}
			if !left.PackageArrayEqual(left) {
				t.Fatal("parsed package projection differs from itself")
			}
		})
	}
}

func TestPackageArrayEvidenceOwnsItsInput(t *testing.T) {
	content := []byte(`{"packages":[{"source":"npm:a@1","opaque":true}]}`)
	left := parseProjection(t, content)
	right := parseProjection(t, bytes.Clone(content))
	clear(content)
	rows := left.Entries()
	rows[0] = piconfig.Entry{}
	if !left.HasPackages() || !left.PackageArrayEqual(right) {
		t.Fatal("caller mutation changed parsed package evidence")
	}
	var unparsed piconfig.Document
	if unparsed.HasPackages() || unparsed.PackageArrayEqual(unparsed) || unparsed.PackageArrayEqual(left) || left.PackageArrayEqual(unparsed) {
		t.Fatal("an unparsed document became package evidence")
	}
}

func TestRejectedPackageArrayIsNotAbsentArrayEvidence(t *testing.T) {
	absent := parseProjection(t, []byte(`{}`))
	for _, content := range []string{`{"packages":null}`, `{"packages":{}}`, `{"packages":[null]}`} {
		document, err := piconfig.Parse([]byte(content))
		if err == nil || document.HasPackages() || document.PackageArrayEqual(absent) || absent.PackageArrayEqual(document) {
			t.Fatal("a rejected array became valid absence evidence")
		}
	}
}

func parseProjection(t testing.TB, content []byte) piconfig.Document {
	t.Helper()
	document, err := piconfig.Parse(content)
	if err != nil {
		t.Fatal(err)
	}
	return document
}
