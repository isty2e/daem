package findings

import (
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
)

// Severity is the closed diagnostic grade: observed problem class, not attempt
// coverage. Doctor checks use CheckStatus instead.
type Severity string

const (
	SeverityOK    Severity = "ok"
	SeverityWarn  Severity = "warn"
	SeverityError Severity = "error"
)

// CheckStatus is the closed doctor-check result. A check has exactly one
// status; ok/warn/error are grades, skipped is non-attempt, and unsupported is
// a capability contract that cannot be honored.
type CheckStatus string

const (
	CheckOK          CheckStatus = "ok"
	CheckWarn        CheckStatus = "warn"
	CheckError       CheckStatus = "error"
	CheckSkipped     CheckStatus = "skipped"
	CheckUnsupported CheckStatus = "unsupported"
)

type Check struct {
	Status        CheckStatus
	Name          string
	Detail        string
	Repairability string
	RepairActions []string
	ManualReasons []string
	NextStep      string
}

func OKCheck(name string, detail string) Check {
	return Check{Status: CheckOK, Name: name, Detail: detail}
}

func WarnCheck(name string, detail string) Check {
	return Check{Status: CheckWarn, Name: name, Detail: detail}
}

func ErrorCheck(name string, detail string) Check {
	return Check{Status: CheckError, Name: name, Detail: detail}
}

func SkippedCheck(name string, detail string) Check {
	return Check{Status: CheckSkipped, Name: name, Detail: detail}
}

func UnsupportedCheck(name string, detail string) Check {
	return Check{Status: CheckUnsupported, Name: name, Detail: detail}
}

func HasCheckErrors(checks []Check) bool {
	for _, check := range checks {
		if check.Status == CheckError {
			return true
		}
	}

	return false
}

type Diagnostic struct {
	Severity      Severity
	Code          string
	EntityID      entity.ID
	Target        target.Target
	Scope         target.Scope
	Event         string
	Command       string
	Detail        string
	Repairability string
	RepairActions []string
	ManualReasons []string
	NextStep      string
}

func HasDiagnosticErrors(diagnostics []Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == SeverityError {
			return true
		}
	}

	return false
}
