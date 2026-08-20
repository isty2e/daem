package findings

import (
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	targetpkg "github.com/isty2e/daem/internal/target"
)

func TestHasCheckErrorsUsesStatusNotText(t *testing.T) {
	checks := []Check{
		OKCheck("manifest", "error-looking detail is not authoritative"),
		WarnCheck("git", "warning"),
		SkippedCheck("skill_observation", "not attempted"),
		UnsupportedCheck("cache", "capability cannot be honored"),
	}
	if HasCheckErrors(checks) {
		t.Fatalf("non-error check status must not be treated as a check error")
	}

	checks = append(checks, ErrorCheck("paths", "failed"))
	if !HasCheckErrors(checks) {
		t.Fatalf("expected error status to be reported")
	}
}

func TestHasDiagnosticErrorsUsesSeverity(t *testing.T) {
	diagnostics := []Diagnostic{
		{
			Severity: SeverityWarn,
			Code:     "skill.compat.manual",
			EntityID: diagnosticEntityID(t, "demo"),
			Target:   targetpkg.TargetCodex,
		},
	}
	if HasDiagnosticErrors(diagnostics) {
		t.Fatalf("warning diagnostic must not be treated as an error")
	}

	diagnostics = append(diagnostics, Diagnostic{Severity: SeverityError, Code: "skill.compat.manual"})
	if !HasDiagnosticErrors(diagnostics) {
		t.Fatalf("expected error diagnostic to be reported")
	}
}

func diagnosticEntityID(t *testing.T, name string) entity.ID {
	t.Helper()
	id, err := entity.New(entity.KindSkill, name)
	if err != nil {
		t.Fatalf("entity.New returned error: %v", err)
	}
	return id
}
