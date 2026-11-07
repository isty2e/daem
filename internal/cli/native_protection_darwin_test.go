package cli

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

func TestNativeProtectionDiagnosticNamesTheUncertainDocumentNotAnUnavailableForeignRoot(t *testing.T) {
	probe, err := mutation.ObserveDirectoryEntryAuthorityBounded(filepath.Join(t.TempDir(), "\u017future", "settings.json"), physicalwork.MaximumPhysicalPathDepth, physicalwork.NewCaptureBudget(nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, provisional := probe.Provisional(); !provisional {
		t.Skip("this filesystem can represent the missing spelling exactly")
	}
	failure, _ := nativeProtectionFailure(t, false, ".codex/\u017future/settings.json")
	var protection *journalnamespace.ProtectionError
	if !errors.As(failure, &protection) || protection.Kind() != journalnamespace.ProtectionUnverified || protection.Uncertainty() != journalnamespace.ProtectionDocumentIndeterminate || errors.Is(failure, configuration.ErrReserved) {
		t.Fatal("normalization-sensitive document became a reserved hit or unavailable root", failure)
	}
	diagnostic, reason := applyFailurePrimary(applyFailurePlanning, failure, applyworkflow.CommandResult{})
	if reason != applyworkflow.FailureReasonApplyRefused || !strings.Contains(diagnostic.Message(), "document's future path") || strings.Contains(diagnostic.Message(), "home could not") || len(diagnostic.Guidance()) != 1 || !strings.Contains(diagnostic.Guidance()[0].Message(), "filesystem spelling") {
		t.Fatal("normalization diagnostic blamed the already-resolved host location", diagnostic.HumanText())
	}
}
