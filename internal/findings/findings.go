package findings

import (
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
)

type Severity string

const (
	SeverityOK    Severity = "ok"
	SeverityWarn  Severity = "warn"
	SeverityError Severity = "error"
)

type Check struct {
	Severity      Severity
	Name          string
	Detail        string
	Repairability string
	RepairActions []string
	ManualReasons []string
	NextStep      string
}

func OKCheck(name string, detail string) Check {
	return Check{Severity: SeverityOK, Name: name, Detail: detail}
}

func WarnCheck(name string, detail string) Check {
	return Check{Severity: SeverityWarn, Name: name, Detail: detail}
}

func ErrorCheck(name string, detail string) Check {
	return Check{Severity: SeverityError, Name: name, Detail: detail}
}

func HasCheckErrors(checks []Check) bool {
	for _, check := range checks {
		if check.Severity == SeverityError {
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
