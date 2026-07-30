package archguard

// Report classifies topology guardrail findings.
type Report struct {
	Violations                []GuardrailFinding
	DensityReviewRequirements []GuardrailFinding
	DensityWatchpoints        []GuardrailFinding
	DensityWarnings           []GuardrailFinding
	PackageDensity            []PackageDensity
}

// HasFailures reports whether semantic violations or unreviewed extreme density
// should fail the baseline. Ordinary density watchpoints remain non-blocking.
func (report Report) HasFailures() bool {
	return len(report.Violations) != 0 || len(report.DensityReviewRequirements) != 0
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

	FileLineCounts map[string]int    `json:"-"`
	FileContents   map[string]string `json:"-"`
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

// PackageDensity records density inventory for one package.
type PackageDensity struct {
	PackagePath        string
	ProductionFiles    int
	TestFiles          int
	MaxProductionPath  string
	MaxProductionLines int
	MaxTestPath        string
	MaxTestLines       int
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
