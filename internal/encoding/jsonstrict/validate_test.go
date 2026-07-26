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
