package jsonstrict

import (
	"strings"
	"testing"
)

func TestValidateRejectsAmbiguousAndUnboundedDocuments(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		depth   int
		want    string
	}{
		{name: "invalid UTF-8", content: []byte{'{', '"', 'x', '"', ':', 0xff, '}'}, depth: 4, want: "valid UTF-8"},
		{name: "duplicate root key", content: []byte(`{"x":1,"x":2}`), depth: 4, want: "duplicate object key"},
		{name: "duplicate nested key", content: []byte(`{"x":{"y":1,"y":2}}`), depth: 4, want: "duplicate object key"},
		{name: "multiple values", content: []byte(`{} []`), depth: 4, want: "multiple JSON values"},
		{name: "excessive depth", content: []byte(`{"a":{"b":{"c":1}}}`), depth: 1, want: "maximum depth"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Validate(test.content, "test document", test.depth)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateAcceptsOneBoundedValue(t *testing.T) {
	if err := Validate([]byte(`{"x":[1,true,null,{"y":"z"}]}`), "test document", 4); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestValidateVersionedObjectRequiresOnePositiveIntegerVersion(t *testing.T) {
	version, err := ValidateVersionedObject(
		[]byte(`{"version":2,"future":true}`),
		"test document",
		4,
	)
	if err != nil || version != 2 {
		t.Fatalf("ValidateVersionedObject = (%d, %v), want (2, nil)", version, err)
	}

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "null document", content: `null`, want: "must be a JSON object"},
		{name: "array document", content: `[]`, want: "must be a JSON object"},
		{name: "missing version", content: `{}`, want: `field "version" is required`},
		{name: "null version", content: `{"version":null}`, want: "must be a positive integer"},
		{name: "string version", content: `{"version":"2"}`, want: "must be a positive integer"},
		{name: "fractional version", content: `{"version":2.5}`, want: "must be a positive integer"},
		{name: "zero version", content: `{"version":0}`, want: "must be a positive integer"},
		{name: "negative version", content: `{"version":-1}`, want: "must be a positive integer"},
		{name: "duplicate version", content: `{"version":1,"version":2}`, want: "duplicate object key"},
		{name: "trailing value", content: `{"version":1} {}`, want: "multiple JSON values"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ValidateVersionedObject([]byte(test.content), "test document", 4)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateVersionedObject error = %v, want %q", err, test.want)
			}
		})
	}
}
