package configdocument_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
)

func TestStandardJSONUsesEditorLineCommentRules(t *testing.T) {
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 8}
	for name, source := range map[string]string{
		"CR":   "{// comment\r\"live\":true}",
		"LF":   "{// comment\n\"live\":true}",
		"CRLF": "{// comment\r\n\"live\":true}",
		"EOF":  "{\"live\":true} // comment",
	} {
		t.Run(name, func(t *testing.T) {
			content := []byte(source)
			before := bytes.Clone(content)
			standard, err := configdocument.StandardJSON(t.Context(), content, configdocument.JSONC, limits)
			if err != nil || !json.Valid(standard) {
				t.Fatal("standard view is not valid JSON", err)
			}
			var values map[string]bool
			if err := json.Unmarshal(standard, &values); err != nil || !values["live"] {
				t.Fatal("standard view lost the live value", err)
			}
			standard[0] = '!'
			if !bytes.Equal(content, before) {
				t.Fatal("standard view aliases the caller's source")
			}
		})
	}
}

func TestStandardJSONRetainsTypedValidationAndBounds(t *testing.T) {
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 8}
	_, err := configdocument.StandardJSON(t.Context(), []byte(`{"a":1,/* duplicate */"a":2}`), configdocument.JSONC, limits)
	if !errors.Is(err, jsonstrict.ErrDuplicateObjectKey) {
		t.Fatal("duplicate-key classification was lost", err)
	}
	_, err = configdocument.StandardJSON(t.Context(), []byte("// invalid \xff\n{}"), configdocument.JSONC, limits)
	if err == nil {
		t.Fatal("standard view accepted invalid UTF-8 in a comment")
	}
	_, err = configdocument.StandardJSON(t.Context(), []byte(`[[[[[[[[[0]]]]]]]]]`), configdocument.JSONC, limits)
	if !errors.Is(err, configdocument.ErrLimitExceeded) {
		t.Fatal("depth was not rejected before the recursive parser", err)
	}
	_, err = configdocument.StandardJSON(t.Context(), []byte(`{"a":1}`), configdocument.JSONC, configdocument.Limits{MaximumBytes: 2, MaximumDepth: 8})
	if !errors.Is(err, configdocument.ErrLimitExceeded) {
		t.Fatal("source byte limit was not enforced", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = configdocument.StandardJSON(ctx, []byte(`{}`), configdocument.JSONC, limits)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation was not preserved", err)
	}
	for _, syntax := range []configdocument.Syntax{0, configdocument.TOML} {
		if _, err := configdocument.StandardJSON(t.Context(), []byte(`{}`), syntax, limits); err == nil {
			t.Fatal("non-JSON grammar was accepted")
		}
	}
}
