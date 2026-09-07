package execute

import (
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal"
)

func TestJournalCleanupPathsRequireCanonicalAbsoluteRecoveryRoot(t *testing.T) {
	root := t.TempDir()
	separator := string(filepath.Separator)
	tests := []struct {
		name string
		path string
	}{
		{name: "empty"},
		{name: "relative", path: "recovery"},
		{
			name: "noncanonical",
			path: filepath.Join(root, "parent") +
				separator + ".." + separator + "recovery",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := (JournalCleanupPaths{RecoveryDir: test.path}).validate()
			if err == nil {
				t.Fatalf("JournalCleanupPaths.validate accepted %q", test.path)
			}
		})
	}

	canonical := filepath.Join(t.TempDir(), "recovery")
	if err := (JournalCleanupPaths{RecoveryDir: canonical}).validate(); err != nil {
		t.Fatalf("JournalCleanupPaths.validate(%q): %v", canonical, err)
	}
}

func TestJournalCleanupBoundaryExposesOnlyRecoveryAuthority(t *testing.T) {
	assertStructFields := func(value any, want []string) {
		t.Helper()
		kind := reflect.TypeOf(value)
		got := make([]string, kind.NumField())
		for index := range kind.NumField() {
			got[index] = kind.Field(index).Name
		}
		if !slices.Equal(got, want) {
			t.Fatalf("%s fields = %v, want %v", kind.Name(), got, want)
		}
	}

	assertStructFields(JournalCleanupPaths{}, []string{"RecoveryDir"})
	assertStructFields(
		JournalCleanupOptions{},
		[]string{"ValidateBeforeEffects", "Filesystem"},
	)
}

func TestJournalCleanupExecutionAbortsAfterPreEffectValidation(t *testing.T) {
	execution, err := newJournalCleanupExecutionForSteps(
		journalCleanupStepsFor(true, true),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := execution.validateBeforeEffects(nil); err != nil {
		t.Fatal(err)
	}
	if err := execution.abortBeforePreparation(); err != nil {
		t.Fatalf("abortBeforePreparation: %v", err)
	}
}

func TestJournalCleanupExecutionConsumesPreparedResidueSchedule(t *testing.T) {
	steps := journalCleanupStepsFor(true, true)
	execution, err := newJournalCleanupExecutionForSteps(steps)
	if err != nil {
		t.Fatal(err)
	}
	if err := execution.validateBeforeEffects(nil); err != nil {
		t.Fatalf("validateBeforeEffects: %v", err)
	}
	for _, step := range steps[1:] {
		if err := execution.AdmitRetirementStep(step.retirementStep); err != nil {
			t.Fatalf("AdmitRetirementStep(%d): %v", step.retirementStep, err)
		}
		if err := execution.SettleRetirementStep(step.retirementStep, true); err != nil {
			t.Fatalf("SettleRetirementStep(%d): %v", step.retirementStep, err)
		}
	}
	closeCalls := 0
	if err := execution.close(func() error {
		closeCalls++
		return nil
	}); err != nil {
		t.Fatalf("close: %v", err)
	}
	if closeCalls != 1 {
		t.Fatalf("close calls = %d, want 1", closeCalls)
	}
}

func TestJournalCleanupExecutionFailureStopsLaterRetirementSteps(t *testing.T) {
	steps := journalCleanupStepsFor(false, true)
	execution, err := newJournalCleanupExecutionForSteps(steps)
	if err != nil {
		t.Fatal(err)
	}
	if err := execution.validateBeforeEffects(nil); err != nil {
		t.Fatalf("validateBeforeEffects: %v", err)
	}
	for _, step := range steps[1:4] {
		if err := execution.AdmitRetirementStep(step.retirementStep); err != nil {
			t.Fatal(err)
		}
		if err := execution.SettleRetirementStep(step.retirementStep, true); err != nil {
			t.Fatal(err)
		}
	}
	failed := steps[4]
	if failed.retirementStep != journal.RetirementStepCleanupResidue {
		t.Fatalf("failure step = %d, want cleanup residue", failed.retirementStep)
	}
	if err := execution.AdmitRetirementStep(failed.retirementStep); err != nil {
		t.Fatal(err)
	}
	if err := execution.SettleRetirementStep(failed.retirementStep, false); err != nil {
		t.Fatal(err)
	}
	if err := execution.AdmitRetirementStep(journal.RetirementStepRetireControl); err == nil {
		t.Fatal("later retirement step was admitted after failure")
	}
	if err := execution.close(func() error { return nil }); err != nil {
		t.Fatalf("close failure branch: %v", err)
	}
}

func TestJournalCleanupExecutionMismatchDoesNotConsumeExpectedStep(t *testing.T) {
	steps := journalCleanupStepsFor(false, false)
	execution, err := newJournalCleanupExecutionForSteps(steps)
	if err != nil {
		t.Fatal(err)
	}
	if err := execution.validateBeforeEffects(nil); err != nil {
		t.Fatal(err)
	}
	if err := execution.AdmitRetirementStep(journal.RetirementStepRetireControl); err == nil {
		t.Fatal("mismatched retirement step was admitted")
	}
	for _, step := range steps[1:] {
		if err := execution.AdmitRetirementStep(step.retirementStep); err != nil {
			t.Fatalf("correct step %d after mismatch: %v", step.retirementStep, err)
		}
		if err := execution.SettleRetirementStep(step.retirementStep, true); err != nil {
			t.Fatal(err)
		}
	}
	closeFailure := errors.New("close failure")
	if err := execution.close(func() error { return closeFailure }); !errors.Is(err, closeFailure) {
		t.Fatalf("close error = %v, want close failure", err)
	}
}
