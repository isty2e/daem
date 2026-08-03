package gitcli

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/source"
)

func TestReadGitTreeDirectoriesCountsAllRecordsAndReturnsDirectories(t *testing.T) {
	t.Parallel()
	budget := source.NewRootListingBudget()
	input := "040000 tree aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\talpha\x00" +
		"100644 blob bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\tREADME\x00" +
		"160000 commit cccccccccccccccccccccccccccccccccccccccc\tmodule\x00" +
		"040000 tree dddddddddddddddddddddddddddddddddddddddd\tbeta\x00"

	names, err := readGitTreeDirectories(strings.NewReader(input), budget)
	if err != nil {
		t.Fatalf("readGitTreeDirectories returned error: %v", err)
	}
	if strings.Join(names, ",") != "alpha,beta" {
		t.Fatalf("names = %#v, want alpha,beta", names)
	}
}

func TestReadGitTreeDirectoriesAppliesExactAndPlusOneNameLimit(t *testing.T) {
	t.Parallel()
	exactName := strings.Repeat("a", 4_096)
	input := "040000 tree aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\t" + exactName + "\x00"
	exactBudget := source.NewRootListingBudget()
	names, err := readGitTreeDirectories(strings.NewReader(input), exactBudget)
	if err != nil || len(names) != 1 || names[0] != exactName {
		t.Fatalf("exact name result = %#v/%v", names, err)
	}

	overflowInput := "040000 tree aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\t" + exactName + "x\x00"
	overflowBudget := source.NewRootListingBudget()
	_, err = readGitTreeDirectories(strings.NewReader(overflowInput), overflowBudget)
	if !errors.Is(err, source.ErrRootListingLimitExceeded) {
		t.Fatalf("plus-one name error = %v, want source listing limit", err)
	}
}

func TestReadGitTreeDirectoriesBoundsManyTinyRecords(t *testing.T) {
	t.Parallel()
	const records = 100_000
	var input strings.Builder
	for index := range records {
		_, _ = fmt.Fprintf(
			&input,
			"100644 blob aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\tn%05d\x00",
			index,
		)
	}
	exactBudget := source.NewRootListingBudget()
	if names, err := readGitTreeDirectories(strings.NewReader(input.String()), exactBudget); err != nil || len(names) != 0 {
		t.Fatalf("exact tiny-record result = %#v/%v", names, err)
	}

	_, _ = fmt.Fprintf(
		&input,
		"100644 blob aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\toverflow\x00",
	)
	overflowBudget := source.NewRootListingBudget()
	_, err := readGitTreeDirectories(strings.NewReader(input.String()), overflowBudget)
	if !errors.Is(err, source.ErrRootListingLimitExceeded) {
		t.Fatalf("tiny-record overflow error = %v, want source listing limit", err)
	}
}

func TestReadGitTreeDirectoriesRejectsMalformedAndOversizedRecords(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		input     string
		wantLimit bool
	}{
		{name: "missing NUL", input: "040000 tree aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\talpha"},
		{name: "missing tab", input: "040000 tree aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa alpha\x00"},
		{name: "unknown kind", input: "040000 tag aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\talpha\x00"},
		{name: "oversized record", input: strings.Repeat("x", 5_000), wantLimit: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			budget := source.NewRootListingBudget()
			_, err := readGitTreeDirectories(strings.NewReader(testCase.input), budget)
			if err == nil {
				t.Fatal("readGitTreeDirectories returned nil error")
			}
			if testCase.wantLimit && !errors.Is(err, source.ErrRootListingLimitExceeded) {
				t.Fatalf("error = %v, want source listing limit", err)
			}
			if !testCase.wantLimit && errors.Is(err, source.ErrRootListingLimitExceeded) {
				t.Fatalf("error = %v, want malformed-record error", err)
			}
		})
	}
}

func prefillRootListingEntries(t *testing.T, budget *source.RootListingBudget, count int) {
	t.Helper()
	for range count {
		if err := budget.AdmitEntryName(1); err != nil {
			t.Fatalf("prefill root listing budget: %v", err)
		}
	}
}
