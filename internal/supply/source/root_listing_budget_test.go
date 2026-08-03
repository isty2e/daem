package source

import (
	"errors"
	"testing"
)

func TestRootListingLimitsRejectInvalidPolicies(t *testing.T) {
	t.Parallel()
	for _, values := range [][4]int64{
		{0, 1, 1, 1},
		{1, 0, 1, 1},
		{1, 1, 0, 1},
		{1, 1, 1, 0},
		{1, 1, 1, 2},
		{defaultRootListingRoots + 1, 1, 1, 1},
		{1, defaultRootListingEntries + 1, 1, 1},
		{1, 1, defaultRootListingNameBytes + 1, 1},
		{1, 1, defaultRootListingEntryNameBytes + 1, defaultRootListingEntryNameBytes + 1},
	} {
		if _, err := newRootListingLimits(values[0], values[1], values[2], values[3]); err == nil {
			t.Fatalf("newRootListingLimits%v returned nil error", values)
		}
	}
}

func TestRootListingBudgetAcceptsEveryExactLimit(t *testing.T) {
	t.Parallel()
	limits, err := newRootListingLimits(2, 3, 6, 3)
	if err != nil {
		t.Fatal(err)
	}
	budget, err := newRootListingBudgetWithLimits(limits)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.CheckRootCount(0); err != nil {
		t.Fatalf("CheckRootCount(0) returned error: %v", err)
	}
	if err := budget.CheckRootCount(1); err != nil {
		t.Fatalf("CheckRootCount(1) returned error: %v", err)
	}

	if err := budget.CheckRootCount(2); err != nil {
		t.Fatalf("CheckRootCount at exact limit returned error: %v", err)
	}
	for _, size := range []int{1, 2, 3} {
		if err := budget.AdmitEntryName(size); err != nil {
			t.Fatalf("AdmitEntryName(%d) at exact limits returned error: %v", size, err)
		}
	}
	if err := budget.Err(); err != nil {
		t.Fatalf("Err at exact limits = %v", err)
	}
}

func TestRootListingBudgetRejectsFirstUnitBeyondEachLimit(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		limits  rootListingLimits
		consume func(*RootListingBudget) error
	}{
		{
			name:   "roots",
			limits: mustRootListingLimits(t, 1, 2, 4, 2),
			consume: func(budget *RootListingBudget) error {
				return budget.CheckRootCount(2)
			},
		},
		{
			name:   "entries",
			limits: mustRootListingLimits(t, 1, 1, 4, 2),
			consume: func(budget *RootListingBudget) error {
				if err := budget.AdmitEntryName(1); err != nil {
					return err
				}
				return budget.AdmitEntryName(1)
			},
		},
		{
			name:   "cumulative name bytes",
			limits: mustRootListingLimits(t, 1, 2, 2, 2),
			consume: func(budget *RootListingBudget) error {
				if err := budget.AdmitEntryName(2); err != nil {
					return err
				}
				return budget.AdmitEntryName(1)
			},
		},
		{
			name:   "one name",
			limits: mustRootListingLimits(t, 1, 1, 3, 2),
			consume: func(budget *RootListingBudget) error {
				return budget.AdmitEntryName(3)
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			budget, err := newRootListingBudgetWithLimits(testCase.limits)
			if err != nil {
				t.Fatal(err)
			}
			err = testCase.consume(budget)
			var limitErr *RootListingLimitError
			if !errors.As(err, &limitErr) || !errors.Is(err, ErrRootListingLimitExceeded) {
				t.Fatalf("overflow error = %v, want RootListingLimitError", err)
			}
			if limitErr.limits != testCase.limits {
				t.Fatalf("error limits = %#v, want %#v", limitErr.limits, testCase.limits)
			}
			if budget.Err().Error() != err.Error() {
				t.Fatalf("stable budget error = %q, want %q", budget.Err(), err)
			}
		})
	}
}

func mustRootListingLimits(
	t *testing.T,
	maximumRoots int64,
	maximumEntries int64,
	maximumNameBytes int64,
	maximumEntryNameBytes int64,
) rootListingLimits {
	t.Helper()
	limits, err := newRootListingLimits(
		maximumRoots,
		maximumEntries,
		maximumNameBytes,
		maximumEntryNameBytes,
	)
	if err != nil {
		t.Fatal(err)
	}
	return limits
}
