package configdocument_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
)

func TestJSONArrayEditRemovesSlotsAndPermutesOnlySurvivingLiterals(t *testing.T) {
	path := arrayKey(t, "rows")
	for _, trailing := range []string{"", ","} {
		for mask := range 8 {
			t.Run(fmt.Sprintf("trailing=%q/removed=%03b", trailing, mask), func(t *testing.T) {
				before := arrayDocument(t, configdocument.JSONC, `{"keep": 1, "rows":[0, 1, 2`+trailing+`], "tail":true}`)
				var removed, order []int
				var wantRows []string
				for index := range 3 {
					if mask&(1<<index) != 0 {
						removed = append(removed, index)
					} else {
						order = append(order, len(order))
						wantRows = append(wantRows, fmt.Sprint(index))
					}
				}
				slices.Reverse(order)
				slices.Reverse(wantRows)
				expected, err := configdocument.EditJSONArray(t.Context(), before, path, removed, order)
				if err != nil {
					t.Fatal(err)
				}
				want := arrayDocument(t, configdocument.JSON, `{"rows":[`+strings.Join(wantRows, ",")+`]}`)
				gotValue, present, err := expected.Lookup(path)
				wantValue, _, _ := want.Lookup(path)
				if err != nil || !present || !gotValue.Equal(wantValue) || !bytes.HasPrefix(expected.Bytes(), []byte(`{"keep": 1, "rows":`)) || !bytes.HasSuffix(expected.Bytes(), []byte(`, "tail":true}`)) {
					t.Fatal("removal changed surviving rows, parent presence or outside bytes", string(expected.Bytes()), err)
				}
				edit, err := configdocument.NewJSONArrayEdit(t.Context(), before, expected, path, removed, order)
				if err != nil {
					t.Fatal(err)
				}
				restored, _, err := edit.Restore(t.Context(), expected)
				if err != nil || !bytes.Equal(restored.Bytes(), before.Bytes()) {
					t.Fatal("different-length array restoration lost original bytes", err)
				}
			})
		}
	}
}

func TestJSONArrayRemovalUsesEachAdmittedJSONGrammarWithoutCreatingParents(t *testing.T) {
	path := arrayKey(t, "rows")
	for _, test := range []struct {
		syntax configdocument.Syntax
		input  string
	}{
		{configdocument.JSON, `{"rows":[1]}`},
		{configdocument.JSONC, "{\"rows\":[{/* inner */\"x\":1},]} // eof"},
		{configdocument.JSONLineComments, "{\"rows\":[{\r\n// inner\r\n\"x\":1}]} // eof"},
	} {
		t.Run(test.syntax.String(), func(t *testing.T) {
			before := arrayDocument(t, test.syntax, test.input)
			expected, err := configdocument.EditJSONArray(t.Context(), before, path, []int{0}, nil)
			if err != nil || !expected.Exists() || !slices.Equal(expected.Comments(), before.Comments()) {
				t.Fatal("last-row removal erased the document or comments", err)
			}
			value, present, err := expected.Lookup(path)
			want, _, _ := arrayDocument(t, test.syntax, `{"rows":[]}`).Lookup(path)
			if err != nil || !present || !value.Equal(want) {
				t.Fatal("last-row removal dropped or replaced its parent", err)
			}
		})
	}
}

func TestJSONArrayRemovalKeepsNestedCommentsAndOutsideConcurrentChanges(t *testing.T) {
	text := "{\r\n \"keep\":1,\"rows\":[\r\n /* slot */ [\"removed\",{\"x\":/* nested */true,\r\n // inner\r\n \"y\":1}],\r\n // foreign\r\n [\"foreign\",{\"payload\":\"/* not a comment */\"}],\r\n /* last */ \"moved\",\r\n ],\"tail\":true\r\n}\r\n// eof"
	before := arrayDocument(t, configdocument.JSONC, text)
	path := arrayKey(t, "rows")
	expected, err := configdocument.EditJSONArray(t.Context(), before, path, []int{0}, []int{1, 0})
	if err != nil || !slices.Equal(before.Comments(), expected.Comments()) {
		t.Fatal("removing a commented tuple lost or rewrote lexical comments", expected.Comments(), err)
	}
	if !bytes.Contains(expected.Bytes(), []byte(`["foreign",{"payload":"/* not a comment */"}]`)) {
		t.Fatal("surviving tuple bytes changed")
	}
	edit, err := configdocument.NewJSONArrayEdit(t.Context(), before, expected, path, []int{0}, []int{1, 0})
	if err != nil {
		t.Fatal(err)
	}
	concurrent := arrayDocument(t, configdocument.JSONC, string(bytes.Replace(expected.Bytes(), []byte(`"keep":1`), []byte(`"keep":200`), 1)))
	restored, changed, err := edit.Restore(t.Context(), concurrent)
	want := bytes.Replace(before.Bytes(), []byte(`"keep":1`), []byte(`"keep":200`), 1)
	if err != nil || !changed || !bytes.Equal(restored.Bytes(), want) {
		t.Fatal("inverse did not splice the original array without resetting outside bytes", changed, err)
	}
	baseline, err := configdocument.EditJSONArray(t.Context(), before, path, []int{0}, []int{0, 1})
	if err != nil {
		t.Fatal(err)
	}
	if original, final, err := edit.Match(t.Context(), baseline); err != nil || original || final {
		t.Fatal("surviving-row planning baseline became an admitted recovery phase", err)
	}
	if rejected, changed, err := edit.Restore(t.Context(), baseline); !errors.Is(err, configdocument.ErrPathConflict) || changed || rejected.Bytes() != nil {
		t.Fatal("intermediate or foreign array was overwritten", changed, err)
	}
}

func TestJSONArrayEditRejectsInvalidPositionsAndContradictoryReplay(t *testing.T) {
	before := arrayDocument(t, configdocument.JSON, `{"rows":[1,2,3]}`)
	path := arrayKey(t, "rows")
	for _, test := range []struct{ removed, order []int }{
		{[]int{-1}, []int{0, 1}},
		{[]int{3}, []int{0, 1}},
		{[]int{1, 1}, []int{0}},
		{[]int{2, 0}, []int{0}},
		{[]int{1}, []int{0}},
		{[]int{1}, []int{0, 0}},
		{[]int{1}, []int{0, 2}},
		{[]int{0, 1, 2}, []int{0}},
	} {
		if rejected, err := configdocument.EditJSONArray(t.Context(), before, path, test.removed, test.order); !errors.Is(err, configdocument.ErrInvalidValue) || rejected.Bytes() != nil {
			t.Fatal("invalid positional witness returned candidate bytes", test, err)
		}
	}
	for _, text := range []string{`{"rows":[3,1,2]}`, `{"rows":[4,1]}`, `{"rows":[3, 1]}`} {
		if _, err := configdocument.NewJSONArrayEdit(t.Context(), before, arrayDocument(t, configdocument.JSON, text), path, []int{1}, []int{1, 0}); !errors.Is(err, configdocument.ErrInvalidValue) {
			t.Fatal("membership addition, literal replacement or slot change bypassed positional replay", text, err)
		}
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if rejected, err := configdocument.EditJSONArray(cancelled, before, path, []int{0}, []int{0, 1}); !errors.Is(err, context.Canceled) || rejected.Bytes() != nil {
		t.Fatal("cancellation exposed a partial removal", err)
	}
}

func TestJSONArrayEditRetainsExactPositionIdentityAndDefensiveCopies(t *testing.T) {
	before := arrayDocument(t, configdocument.JSON, `{"rows":[1,1,2]}`)
	expected := arrayDocument(t, configdocument.JSON, `{"rows":[1,2]}`)
	path := arrayKey(t, "rows")
	removed, order := []int{0}, []int{0, 1}
	left, err := configdocument.NewJSONArrayEdit(t.Context(), before, expected, path, removed, order)
	if err != nil {
		t.Fatal(err)
	}
	right, err := configdocument.NewJSONArrayEdit(t.Context(), before, expected, path, []int{1}, order)
	if err != nil || left.Equal(right) {
		t.Fatal("identical row bytes erased distinct original removal slots", err)
	}
	removed[0], order[0] = 2, 1
	left.RemovedSlots()[0], left.SurvivorOrder()[0] = 2, 1
	if !slices.Equal(left.RemovedSlots(), []int{0}) || !slices.Equal(left.SurvivorOrder(), []int{0, 1}) {
		t.Fatal("caller slices changed retained edit intent")
	}
}

func TestJSONArraySourceBoundaryKeepsUnrelatedLegacyFieldsOpaque(t *testing.T) {
	path := arrayKey(t, "g", "rows")
	limits := configdocument.Limits{MaximumBytes: 1024, MaximumDepth: 16}
	before := []byte(`{"keep":1,"keep":2,"g":{"rows":[1,2]}}`)
	candidate, err := configdocument.EditJSONArraySource(t.Context(), before, configdocument.JSONC, limits, path, []string{"1", "2"}, nil, []int{1, 0})
	if err != nil || string(candidate) != `{"keep":1,"keep":2,"g":{"rows":[2,1]}}` {
		t.Fatal("syntax-only editing reinterpreted unrelated duplicate fields", string(candidate), err)
	}
	if _, err := configdocument.Parse(t.Context(), before, configdocument.JSONC, limits); err == nil {
		t.Fatal("source adapter weakened canonical native document validation")
	}
	for _, rows := range [][]string{{"1"}, {"2", "1"}, {"1", "3"}, {" 1", "2"}} {
		if rejected, err := configdocument.EditJSONArraySource(t.Context(), before, configdocument.JSONC, limits, path, rows, nil, []int{1, 0}); !errors.Is(err, configdocument.ErrPathConflict) || rejected != nil {
			t.Fatal("unmatched semantic literal positions produced edited bytes", rows, err)
		}
	}
	for _, text := range []string{`{"g":{"rows":[1],"rows":[2]}}`, `{"g":{"rows":[1]},"g":{"rows":[2]}}`} {
		if rejected, err := configdocument.EditJSONArraySource(t.Context(), []byte(text), configdocument.JSONC, limits, path, []string{"1"}, nil, []int{0}); !errors.Is(err, configdocument.ErrPathConflict) || rejected != nil {
			t.Fatal("ambiguous selected path became an array-edit authority", err)
		}
	}
	if rejected, err := configdocument.EditJSONArraySource(t.Context(), before, configdocument.JSONC, configdocument.Limits{MaximumBytes: 4, MaximumDepth: 16}, path, []string{"1", "2"}, nil, []int{1, 0}); !errors.Is(err, configdocument.ErrLimitExceeded) || rejected != nil {
		t.Fatal("raw source adapter bypassed the caller's byte limit", err)
	}
	if rejected, err := configdocument.EditJSONArraySource(t.Context(), before, configdocument.JSONC, configdocument.Limits{MaximumBytes: 1024, MaximumDepth: 2}, path, []string{"1", "2"}, nil, []int{1, 0}); !errors.Is(err, configdocument.ErrLimitExceeded) || rejected != nil {
		t.Fatal("raw source adapter bypassed the caller's depth limit", err)
	}
}
