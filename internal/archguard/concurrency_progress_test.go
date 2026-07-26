package archguard

import (
	"strings"
	"testing"
)

func TestAnalyzeRecordsReportsConcurrencyProgressForbiddenImports(t *testing.T) {
	cases := []struct {
		name   string
		record PackageRecord
		want   []string
	}{
		{
			name: "source cache imports workflow",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/supply/source/cache",
				Imports:    []string{"example.com/project/internal/workflow/lock"},
			},
			want: []string{
				"source-cache-boundary-import: internal/supply/source/cache -> internal/workflow/lock",
			},
		},
		{
			name: "source cache imports lock build",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/supply/source/cache",
				Imports:    []string{"example.com/project/internal/realization/lock/build"},
			},
			want: []string{
				"source-cache-boundary-import: internal/supply/source/cache -> internal/realization/lock/build",
			},
		},
		{
			name: "source resolution imports resource semantics",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/supply/source/resolution",
				Imports:    []string{"example.com/project/internal/resource/skill"},
			},
			want: []string{
				"source-semantic-import: internal/supply/source/resolution -> internal/resource/skill",
			},
		},
		{
			name: "source backend imports workflow",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/supply/source/backend/gitcli",
				Imports:    []string{"example.com/project/internal/workflow/lock"},
			},
			want: []string{
				"source-semantic-import: internal/supply/source/backend/gitcli -> internal/workflow/lock",
			},
		},
		{
			name: "source package imports present",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/supply/source",
				Imports:    []string{"example.com/project/internal/cli/present"},
			},
			want: []string{
				"source-semantic-import: internal/supply/source -> internal/cli/present",
			},
		},
		{
			name: "source package imports cli",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/supply/source",
				Imports:    []string{"example.com/project/internal/cli"},
			},
			want: []string{
				"source-semantic-import: internal/supply/source -> internal/cli",
				"internal package imports CLI: internal/supply/source -> internal/cli",
			},
		},
		{
			name: "lock build imports source backend",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/realization/lock/build",
				Imports:    []string{"example.com/project/internal/supply/source/backend/gitcli"},
			},
			want: []string{
				"lock-build-source-import: internal/realization/lock/build -> internal/supply/source/backend/gitcli",
			},
		},
		{
			name: "lock build imports source resolution",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/realization/lock/build",
				Imports:    []string{"example.com/project/internal/supply/source/resolution"},
			},
			want: []string{
				"lock-build-source-import: internal/realization/lock/build -> internal/supply/source/resolution",
			},
		},
		{
			name: "lock build imports workflow",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/realization/lock/build",
				Imports:    []string{"example.com/project/internal/workflow/lock"},
			},
			want: []string{
				"lock-build-boundary-import: internal/realization/lock/build -> internal/workflow/lock",
			},
		},
		{
			name: "execute imports present",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/effect/execute",
				Imports:    []string{"example.com/project/internal/cli/present"},
			},
			want: []string{
				"journal or execute package imports forbidden phase: present: internal/effect/execute -> internal/cli/present",
			},
		},
		{
			name: "execute imports workflow",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/effect/execute",
				Imports:    []string{"example.com/project/internal/workflow/apply"},
			},
			want: []string{
				"journal or execute package imports forbidden phase: workflow: internal/effect/execute -> internal/workflow/apply",
			},
		},
		{
			name: "workflow imports present",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/workflow/lock",
				Imports:    []string{"example.com/project/internal/cli/present"},
			},
			want: []string{
				"workflow-present-import: internal/workflow/lock -> internal/cli/present",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := FormatReport(AnalyzeRecords([]PackageRecord{tc.record}))
			for _, want := range tc.want {
				if !strings.Contains(report, want) {
					t.Fatalf("report = %q, want %q", report, want)
				}
			}
		})
	}
}

func TestAnalyzeRecordsReportsForbiddenProgressBasisShapes(t *testing.T) {
	records := []PackageRecord{
		{ImportPath: "example.com/project/internal/workflow/progress"},
		{ImportPath: "example.com/project/internal/task"},
		{ImportPath: "example.com/project/internal/tasks/source"},
		{ImportPath: "example.com/project/internal/operation"},
		{ImportPath: "example.com/project/internal/operations/source"},
		{ImportPath: "example.com/project/internal/progress"},
		{ImportPath: "example.com/project/internal/resource/lockable"},
		{ImportPath: "example.com/project/internal/lockable"},
		{ImportPath: "example.com/project/internal/realization/lock/build", GoFiles: []string{"source_tasks.go"}},
	}

	report := FormatReport(AnalyzeRecords(records))
	for _, want := range []string{
		"forbidden-progress-basis-shape: internal/workflow/progress",
		"forbidden-progress-basis-shape: internal/task",
		"forbidden-progress-basis-shape: internal/tasks/source",
		"forbidden-progress-basis-shape: internal/operation",
		"forbidden-progress-basis-shape: internal/operations/source",
		"forbidden-progress-basis-shape: internal/progress",
		"forbidden-progress-basis-shape: internal/resource/lockable",
		"forbidden-progress-basis-shape: internal/lockable",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report = %q, want %q", report, want)
		}
	}
	if strings.Contains(report, "forbidden-progress-basis-shape: internal/realization/lock/build") {
		t.Fatalf("report = %q, did not want phase-local lock/build tasks reported", report)
	}
}

func TestAnalyzeRecordsAllowsAcceptedConcurrencyProgressEdges(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/supply/source/resolution",
			Imports: []string{
				"example.com/project/internal/supply/source",
				"example.com/project/internal/supply/source/backend/gitcli",
				"example.com/project/internal/supply/source/cache",
			},
		},
		{
			ImportPath: "example.com/project/internal/realization/lock/build",
			Imports: []string{
				"example.com/project/internal/supply/source",
			},
		},
		{
			ImportPath: "example.com/project/internal/workflow/lock",
			Imports: []string{
				"example.com/project/internal/realization/lock/build",
				"example.com/project/internal/supply/source/resolution",
			},
		},
		{
			ImportPath: "example.com/project/internal/cli/present",
			Imports: []string{
				"example.com/project/internal/realization/lock/build",
			},
		},
	}

	report := FormatReport(AnalyzeRecords(records))
	for _, unwanted := range []string{
		ruleSourceCacheBoundaryImport,
		ruleSourceSemanticImport,
		ruleLockBuildSourceImport,
		ruleWorkflowPresentImport,
	} {
		if strings.Contains(report, unwanted) {
			t.Fatalf("report = %q, did not want accepted edge reported by %s", report, unwanted)
		}
	}
}

func TestAnalyzeRecordsReportsCoreTerminalSideEffects(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/supply/source/resolution",
			GoFiles:    []string{"resolution.go"},
			FileContents: map[string]string{
				"resolution.go": `package resolution

import "fmt"

func resolve() {
	fmt.Println("resolving")
}
`,
			},
		},
		{
			ImportPath: "example.com/project/internal/realization/lock/build",
			GoFiles:    []string{"build.go"},
			FileContents: map[string]string{
				"build.go": `package build

import logger "log"

func build() {
	logger.Printf("building")
}
`,
			},
		},
		{
			ImportPath: "example.com/project/internal/effect/execute",
			GoFiles:    []string{"apply.go"},
			FileContents: map[string]string{
				"apply.go": `package execute

import (
	"fmt"
	"os"
)

func apply() {
	fmt.Fprintf(os.Stderr, "applying")
}
`,
			},
		},
	}

	report := FormatReport(AnalyzeRecords(records))
	for _, want := range []string{
		"core-terminal-side-effect: internal/supply/source/resolution/resolution.go",
		"direct terminal output through fmt.Println",
		"core-terminal-side-effect: internal/realization/lock/build/build.go",
		"direct terminal output through log.Printf",
		"core-terminal-side-effect: internal/effect/execute/apply.go",
		"direct terminal handle use through os.Stderr",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report = %q, want %q", report, want)
		}
	}
}

func TestAnalyzeRecordsAllowsNonTerminalFormattingAndPresentationRendering(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/realization/lock/build",
			GoFiles:    []string{"build.go"},
			FileContents: map[string]string{
				"build.go": `package build

import (
	"fmt"
	"io"
)

func build(output io.Writer) error {
	fmt.Fprintf(output, "%s", "building")
	_ = fmt.Sprintf("%s", "building")
	return fmt.Errorf("build failed")
}
`,
			},
		},
		{
			ImportPath: "example.com/project/internal/cli/present",
			GoFiles:    []string{"progress.go"},
			FileContents: map[string]string{
				"progress.go": `package lockpresent

import "fmt"

func render() {
	fmt.Println("rendering")
}
`,
			},
		},
		{
			ImportPath:  "example.com/project/internal/supply/source/resolution",
			TestGoFiles: []string{"resolution_test.go"},
			FileContents: map[string]string{
				"resolution_test.go": `package resolution

import "log"

func testLog() {
	log.Print("debug")
}
`,
			},
		},
	}

	report := FormatReport(AnalyzeRecords(records))
	if strings.Contains(report, ruleCoreTerminalSideEffect) {
		t.Fatalf("report = %q, did not want non-terminal formatting, presentation rendering, or test logging reported", report)
	}
}
