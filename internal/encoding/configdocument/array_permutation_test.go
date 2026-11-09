package configdocument_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
)

func arrayDocument(t *testing.T, syntax configdocument.Syntax, content string) configdocument.Document {
	t.Helper()
	document, err := configdocument.Parse(t.Context(), []byte(content), syntax, configdocument.Limits{MaximumBytes: 8192, MaximumDepth: 32})
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func arrayKey(t *testing.T, segments ...string) output.KeyPath {
	t.Helper()
	path, err := output.NewKeyPath(segments...)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestJSONArrayPermutationPreservesSlotTriviaAndConcurrentOutsideBytes(t *testing.T) {
	beforeText := "{\r\n \"keep\": 1, \"g/~\": { \"plugins\": [\r\n // first slot\r\n \"second\",\r\n /* foreign slot */ {\"id\":\"Café\",\"data\":[1,2]},\r\n // last slot\r\n \"first\",\r\n ] }, \"tail\":true\r\n}\r\n// eof"
	expectedText := "{\r\n \"keep\": 1, \"g/~\": { \"plugins\": [\r\n // first slot\r\n \"first\",\r\n /* foreign slot */ {\"id\":\"Café\",\"data\":[1,2]},\r\n // last slot\r\n \"second\",\r\n ] }, \"tail\":true\r\n}\r\n// eof"
	path := arrayKey(t, "g/~", "plugins")
	before := arrayDocument(t, configdocument.JSONC, beforeText)
	expected, err := configdocument.PermuteJSONArray(t.Context(), before, path, []int{2, 1, 0})
	if err != nil || !bytes.Equal(expected.Bytes(), []byte(expectedText)) {
		t.Fatal("permutation changed slot trivia or unrelated bytes", string(expected.Bytes()), err)
	}
	permutation, err := configdocument.NewJSONArrayPermutation(t.Context(), before, expected, path)
	if err != nil {
		t.Fatal(err)
	}
	concurrent := bytes.Replace(expected.Bytes(), []byte(`"keep": 1`), []byte(`"keep": 2`), 1)
	current := arrayDocument(t, configdocument.JSONC, string(concurrent))
	if matches, err := permutation.MatchesExpected(t.Context(), current); err != nil || !matches {
		t.Fatal("outside changes invalidated selected-array identity", matches, err)
	}
	restored, changed, err := permutation.Restore(t.Context(), current)
	want := bytes.Replace(before.Bytes(), []byte(`"keep": 1`), []byte(`"keep": 2`), 1)
	if err != nil || !changed || !bytes.Equal(restored.Bytes(), want) {
		t.Fatal("restore overwrote concurrent outside bytes", changed, string(restored.Bytes()), err)
	}
	if matches, err := permutation.MatchesBefore(t.Context(), restored); err != nil || !matches {
		t.Fatal("restored array did not match its captured before state", matches, err)
	}
	if unchanged, changed, err := permutation.Restore(t.Context(), restored); err != nil || changed || !bytes.Equal(unchanged.Bytes(), want) {
		t.Fatal("already-restored array did not remain unchanged", changed, err)
	}
	foreignChange := bytes.Replace(expected.Bytes(), []byte(`"Café"`), []byte(`"foreign-change"`), 1)
	if rejected, changed, err := permutation.Restore(t.Context(), arrayDocument(t, configdocument.JSONC, string(foreignChange))); !errors.Is(err, configdocument.ErrPathConflict) || changed || rejected.Bytes() != nil {
		t.Fatal("a changed foreign array row was overwritten", changed, err)
	}
}

func TestJSONArrayPermutationRetainsEmptyAndDuplicateLiteralRows(t *testing.T) {
	path := arrayKey(t, "rows")
	for _, syntax := range []configdocument.Syntax{configdocument.JSON, configdocument.JSONC, configdocument.JSONLineComments} {
		for _, test := range []struct {
			before, expected string
			order            []int
		}{
			{`{"rows":[]}`, `{"rows":[]}`, []int{}},
			{`{"rows":[{"x":1}, "a", {"x":1}]}`, `{"rows":["a", {"x":1}, {"x":1}]}`, []int{1, 0, 2}},
		} {
			before := arrayDocument(t, syntax, test.before)
			expected, err := configdocument.PermuteJSONArray(t.Context(), before, path, test.order)
			if err != nil || string(expected.Bytes()) != test.expected {
				t.Fatal("literal permutation lost multiplicity or empty-array state", err)
			}
			if _, err := configdocument.NewJSONArrayPermutation(t.Context(), before, expected, path); err != nil {
				t.Fatal("valid literal permutation could not be captured", err)
			}
		}
	}
}

func TestJSONArrayPermutationRejectsMembershipSyntaxAndInvalidOrders(t *testing.T) {
	path := arrayKey(t, "rows")
	before := arrayDocument(t, configdocument.JSON, `{"rows":[1, 2, 3]}`)
	for _, order := range [][]int{{0, 1}, {0, 0, 2}, {0, 1, 3}, {-1, 1, 2}} {
		if rejected, err := configdocument.PermuteJSONArray(t.Context(), before, path, order); !errors.Is(err, configdocument.ErrInvalidValue) || rejected.Bytes() != nil {
			t.Fatal("invalid permutation produced partial document bytes", order, err)
		}
	}
	for _, content := range []string{`{"rows":[1, 2]}`, `{"rows":[1, 2, 4]}`, `{"rows":[3,2,1]}`} {
		if _, err := configdocument.NewJSONArrayPermutation(t.Context(), before, arrayDocument(t, configdocument.JSON, content), path); !errors.Is(err, configdocument.ErrInvalidValue) {
			t.Fatal("membership, literal or slot-syntax changes became a permutation", content, err)
		}
	}
	for _, content := range []string{`{}`, `{"rows":1}`} {
		if rejected, err := configdocument.PermuteJSONArray(t.Context(), arrayDocument(t, configdocument.JSON, content), path, nil); !errors.Is(err, configdocument.ErrPathConflict) || rejected.Bytes() != nil {
			t.Fatal("absent or scalar property became an array", content, err)
		}
	}
}

func TestJSONArrayPermutationKeepsCancellationAndUninitializedValuesInvalid(t *testing.T) {
	path := arrayKey(t, "rows")
	before := arrayDocument(t, configdocument.JSON, `{"rows":[1,2]}`)
	expected := arrayDocument(t, configdocument.JSON, `{"rows":[2,1]}`)
	permutation, err := configdocument.NewJSONArrayPermutation(t.Context(), before, expected, path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if rejected, err := configdocument.PermuteJSONArray(ctx, before, path, []int{1, 0}); !errors.Is(err, context.Canceled) || rejected.Bytes() != nil {
		t.Fatal("canceled permutation returned a candidate", err)
	}
	if _, err := configdocument.NewJSONArrayPermutation(ctx, before, expected, path); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled permutation capture continued", err)
	}
	if rejected, changed, err := permutation.Restore(ctx, expected); !errors.Is(err, context.Canceled) || changed || rejected.Bytes() != nil {
		t.Fatal("canceled permutation restore returned a candidate", changed, err)
	}
	if rejected, changed, err := (configdocument.JSONArrayPermutation{}).Restore(t.Context(), expected); err == nil || changed || rejected.Bytes() != nil {
		t.Fatal("zero permutation restored a document", err)
	}
	if rejected, err := configdocument.PermuteJSONArray(t.Context(), before, output.KeyPath{}, []int{1, 0}); err == nil || rejected.Bytes() != nil {
		t.Fatal("array operation silently selected the document root", err)
	}
	absent, err := configdocument.Absent(configdocument.JSON, configdocument.Limits{MaximumBytes: 1024, MaximumDepth: 16})
	if err != nil {
		t.Fatal(err)
	}
	if rejected, err := configdocument.PermuteJSONArray(t.Context(), absent, path, nil); err == nil || rejected.Exists() {
		t.Fatal("permutation created an absent document", err)
	}
}
