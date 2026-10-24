package pi_test

import (
	"bytes"
	"testing"

	piconfig "github.com/isty2e/daem/internal/realization/configrelation/pi"
)

func TestPackagePermutationRestoresOnlyItsExactArray(t *testing.T) {
	beforeBytes := []byte("{\r\n\"packages\": [ {\"source\":\"npm:b@1\",\"opaque\":{\"n\":1e999}}, \"npm:foreign@1\", \"npm:\\u0061@1\" ],\r\n\"theme\":\"original\"}")
	before := parseProjection(t, beforeBytes)
	afterBytes, changed, err := before.PermutePackageRows([]int{2, 1, 0})
	if err != nil || !changed {
		t.Fatalf("fixture permutation: %v", err)
	}

	after := parseProjection(t, bytes.ReplaceAll(afterBytes, []byte(`"original"`), []byte(`"desired"`)))
	permutation, err := piconfig.NewPackagePermutation(before, after)
	if err != nil {
		t.Fatal(err)
	}

	currentBytes := bytes.ReplaceAll(afterBytes, []byte(`"theme":"original"`), []byte(`"theme":"fresh","outside":{"keep":true}`))
	current := parseProjection(t, currentBytes)
	if !permutation.MatchesBefore(before) || !permutation.MatchesExpected(current) {
		t.Fatal("outside root fields affected package evidence")
	}

	restored, changed, err := permutation.Restore(current)
	if err != nil || !changed || !parseProjection(t, restored).PackageArrayEqual(before) {
		t.Fatalf("inverse permutation did not restore exact rows/slots: %v", err)
	}

	want := bytes.ReplaceAll(beforeBytes, []byte(`"theme":"original"`), []byte(`"theme":"fresh","outside":{"keep":true}`))
	if !bytes.Equal(restored, want) {
		t.Fatal("inverse permutation replaced unrelated root bytes")
	}

	again, changed, err := permutation.Restore(parseProjection(t, restored))
	if err != nil || changed || !bytes.Equal(again, restored) {
		t.Fatal("already-before recovery was not an exact no-op")
	}

	clear(beforeBytes)
	if !permutation.MatchesBefore(parseProjection(t, restored)) {
		t.Fatal("caller byte mutation changed retained permutation evidence")
	}
}

func TestPackagePermutationRefusesMembershipAndLiteralInterference(t *testing.T) {
	before := parseProjection(t, []byte(`{"packages":["b", {"source":"a","opaque":true}]}`))
	afterBytes, _, err := before.PermutePackageRows([]int{1, 0})
	if err != nil {
		t.Fatal(err)
	}

	after := parseProjection(t, afterBytes)
	permutation, err := piconfig.NewPackagePermutation(before, after)
	if err != nil {
		t.Fatal(err)
	}

	for _, text := range []string{
		`{"packages":["b"]}`,
		`{"packages":["new","b", {"source":"a","opaque":true}]}`,
		`{"packages":[{"source":"a","opaque":false}, "b"]}`,
		`{"packages":[{"source":"a","opaque":null}, "b"]}`,
		`{"packages":[{"source":"c","opaque":true}, "b"]}`,
		`{"packages":[{"source":"\u0061","opaque":true}, "b"]}`,
		`{"packages":[{"source":"a","opaque":true},"b"]}`,
		`{"packages":[]}`, `{}`,
	} {
		current := parseProjection(t, []byte(text))
		if output, changed, err := permutation.Restore(current); err == nil || changed || output != nil {
			t.Fatal("interference produced a whole-array reset candidate")
		}
		if _, err := piconfig.NewPackagePermutation(before, current); err == nil {
			t.Fatal("non-permutation before/after evidence was admitted")
		}
	}

	var zero piconfig.PackagePermutation
	if zero.MatchesBefore(before) || zero.MatchesExpected(after) {
		t.Fatal("zero permutation became current evidence")
	}
	if output, changed, err := zero.Restore(after); err == nil || changed || output != nil {
		t.Fatal("zero permutation produced a candidate")
	}
}

func TestPackagePermutationKeepsDuplicateLiteralRowsAndEmptyIdentity(t *testing.T) {
	for _, test := range []struct {
		content string
		order   []int
	}{
		{`{"packages":["same", "other", "same"]}`, []int{1, 2, 0}},
		{`{"packages":[ ]}`, nil},
	} {
		before := parseProjection(t, []byte(test.content))
		afterBytes, _, err := before.PermutePackageRows(test.order)
		if err != nil {
			t.Fatal(err)
		}
		permutation, err := piconfig.NewPackagePermutation(before, parseProjection(t, afterBytes))
		if err != nil {
			t.Fatal(err)
		}
		restored, _, err := permutation.Restore(parseProjection(t, afterBytes))
		if err != nil || !bytes.Equal(restored, []byte(test.content)) {
			t.Fatal("duplicate/empty exact row permutation was not lossless")
		}
	}

	missing := parseProjection(t, []byte(`{}`))
	if _, err := piconfig.NewPackagePermutation(missing, missing); err == nil {
		t.Fatal("missing package arrays became a mutation permutation")
	}
}
