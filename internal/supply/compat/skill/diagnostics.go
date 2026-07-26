package skillcompat

import "fmt"

// Axis identifies the compatibility dimension that produced a diagnostic.
type Axis string

const (
	AxisArtifact     Axis = "artifact"
	AxisDiscovery    Axis = "discovery"
	AxisFrontmatter  Axis = "frontmatter"
	AxisIdentity     Axis = "identity"
	AxisSelection    Axis = "selection"
	AxisControlField Axis = "control-field"
	AxisCollision    Axis = "collision"
)

// Severity identifies whether a compatibility diagnostic blocks loading.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Diagnostic describes one target-specific skill compatibility finding.
type Diagnostic struct {
	Severity Severity
	Axis     Axis
	Code     string
	Message  string
}

// Blocking reports whether the diagnostic should stop lock/apply.
func (diagnostic Diagnostic) Blocking() bool {
	return diagnostic.Severity == SeverityError
}

func errorDiagnostic(axis Axis, code string, format string, args ...any) Diagnostic {
	return Diagnostic{
		Severity: SeverityError,
		Axis:     axis,
		Code:     code,
		Message:  fmt.Sprintf(format, args...),
	}
}

func warningDiagnostic(axis Axis, code string, format string, args ...any) Diagnostic {
	return Diagnostic{
		Severity: SeverityWarning,
		Axis:     axis,
		Code:     code,
		Message:  fmt.Sprintf(format, args...),
	}
}
