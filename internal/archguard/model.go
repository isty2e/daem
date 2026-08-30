package archguard

// Report contains semantic topology guardrail findings.
//
// Violations are blocking. Shadow is report-only compiler/State Barrier
// evidence and never participates in HasFailures.
type Report struct {
	Violations []GuardrailFinding
	Shadow     []GuardrailFinding
}

// HasFailures reports whether semantic topology violations were found.
func (report Report) HasFailures() bool {
	return len(report.Violations) != 0
}

// HasShadowFindings reports whether report-only compiler-shadow findings exist.
func (report Report) HasShadowFindings() bool {
	return len(report.Shadow) != 0
}

// PackageRecord is the subset of go list -json package data used by archguard.
type PackageRecord struct {
	ImportPath   string   `json:"ImportPath"`
	Name         string   `json:"Name"`
	Dir          string   `json:"Dir"`
	Imports      []string `json:"Imports"`
	GoFiles      []string `json:"GoFiles"`
	CgoFiles     []string `json:"CgoFiles"`
	TestGoFiles  []string `json:"TestGoFiles"`
	XTestGoFiles []string `json:"XTestGoFiles"`

	FileContents map[string]string `json:"-"`
}

// GuardrailFinding describes one topology guardrail finding.
type GuardrailFinding struct {
	Rule        string
	PackagePath string
	ImportPath  string
	Path        string
	Reason      string
	Detail      string
}

type importRule struct {
	rule             string
	subject          func(string) bool
	forbiddenImports []forbiddenImport
}

type forbiddenImport struct {
	name  string
	paths []string
}
