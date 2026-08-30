package archguard

import (
	"strings"
	"testing"
)

func TestHasFailuresIgnoresShadow(t *testing.T) {
	report := AnalyzeReport([]PackageRecord{{
		ImportPath: "example.com/project/internal/hostsurface/catalog",
		GoFiles:    []string{"compile.go", "compile_windows.go"},
	}})
	if !report.HasShadowFindings() {
		t.Fatal("expected compiler-shadow finding")
	}
	if report.HasFailures() {
		t.Fatalf("HasFailures = true for shadow-only report:\n%s", FormatAnalysisReport(report))
	}
	if !strings.Contains(FormatShadowReport(report), "compiler-os-specialization") {
		t.Fatalf("shadow report = %q, want compiler-os-specialization", FormatShadowReport(report))
	}
	if strings.Contains(FormatAnalysisReport(report), "compiler-shadow") {
		t.Fatalf("blocking report leaked shadow text:\n%s", FormatAnalysisReport(report))
	}
}

func TestFormatShadowReportIsDeterministic(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/hostsurface",
			Imports:    []string{"example.com/project/internal/operationplan", "example.com/project/internal/workflow/apply"},
		},
		{
			ImportPath: "example.com/project/internal/operationplan",
			Imports:    []string{"example.com/project/internal/hostsurface"},
		},
	}
	first := FormatShadowReport(AnalyzeReport(records))
	second := FormatShadowReport(AnalyzeReport(records))
	if first != second {
		t.Fatalf("first report = %q\nsecond report = %q", first, second)
	}
}

func TestCompilerShadowRejectsForbiddenFixtures(t *testing.T) {
	cases := []struct {
		name   string
		record PackageRecord
		want   string
	}{
		{
			name: "hostsurface imports workflow",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/hostsurface/catalog",
				Imports:    []string{"example.com/project/internal/workflow/list"},
			},
			want: "compiler-workflow-import: internal/hostsurface/catalog -> internal/workflow/list",
		},
		{
			name: "hostsurface imports effect",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/hostsurface",
				Imports:    []string{"example.com/project/internal/effect/execute"},
			},
			want: "compiler-hostsurface-forbidden-import: internal/hostsurface -> internal/effect/execute",
		},
		{
			name: "hostsurface imports observe adapter",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/hostsurface/catalog",
				Imports:    []string{"example.com/project/internal/assurance/observe/mcp"},
			},
			want: "compiler-hostsurface-forbidden-import: internal/hostsurface/catalog -> internal/assurance/observe/mcp",
		},
		{
			name: "compilers import each other",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/hostsurface",
				Imports:    []string{"example.com/project/internal/operationplan"},
			},
			want: "compiler-orthogonality-import: internal/hostsurface -> internal/operationplan",
		},
		{
			name: "operationplan imports hostsurface",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/operationplan",
				Imports:    []string{"example.com/project/internal/hostsurface/catalog"},
			},
			want: "compiler-orthogonality-import: internal/operationplan -> internal/hostsurface/catalog",
		},
		{
			name: "operationplan imports execute",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/operationplan",
				Imports:    []string{"example.com/project/internal/effect/execute"},
			},
			want: "compiler-operationplan-forbidden-import: internal/operationplan -> internal/effect/execute",
		},
		{
			name: "topology imports hostsurface",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/topology/mcp",
				Imports:    []string{"example.com/project/internal/hostsurface/catalog"},
			},
			want: "compiler-owner-catalog-import: internal/topology/mcp -> internal/hostsurface/catalog",
		},
		{
			name: "recoverygate imports hostsurface",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/recoverygate",
				Imports:    []string{"example.com/project/internal/hostsurface"},
			},
			want: "compiler-state-barrier-hostsurface-import: internal/recoverygate -> internal/hostsurface",
		},
		{
			name: "hostsurface GOOS file",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/hostsurface/catalog",
				GoFiles:    []string{"compile.go", "compile_windows.go"},
			},
			want: "compiler-os-specialization: internal/hostsurface/catalog/compile_windows.go",
		},
		{
			name: "operationplan cgo",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/operationplan",
				CgoFiles:   []string{"native.go"},
			},
			want: "compiler-os-specialization: internal/operationplan",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			report := FormatShadowReport(AnalyzeReport([]PackageRecord{testCase.record}))
			if !strings.Contains(report, testCase.want) {
				t.Fatalf("report = %q, want %q", report, testCase.want)
			}
		})
	}
}

func TestCompilerShadowAllowsNearNeighbors(t *testing.T) {
	report := FormatShadowReport(AnalyzeReport([]PackageRecord{
		{
			ImportPath: "example.com/project/internal/hostsurface/catalog",
			Imports: []string{
				"example.com/project/internal/hostsurface",
				"example.com/project/internal/realization/aggregate",
				"example.com/project/internal/realization/profile",
				"example.com/project/internal/target",
				"example.com/project/internal/topology/mcp",
			},
			GoFiles: []string{"compile.go", "lookup.go"},
		},
		{
			ImportPath: "example.com/project/internal/operationplan",
			Imports:    []string{"example.com/project/internal/effect/mutation"},
			GoFiles:    []string{"builder.go", "envelope.go"},
		},
		{
			ImportPath: "example.com/project/internal/recoverygate",
			Imports: []string{
				"example.com/project/internal/operationplan",
				"example.com/project/internal/effect/fileset",
			},
			GoFiles: []string{"authority.go", "state_dir_authority_unix.go"},
		},
		{
			ImportPath: "example.com/project/internal/workflow/apply",
			Imports: []string{
				"example.com/project/internal/hostsurface/catalog",
				"example.com/project/internal/operationplan",
				"example.com/project/internal/recoverygate",
			},
		},
		{
			ImportPath: "example.com/project/internal/assurance/observe/mcp",
			Imports:    []string{"example.com/project/internal/hostsurface/catalog"},
		},
		{
			ImportPath: "example.com/project/internal/adopt/mcp",
			Imports:    []string{"example.com/project/internal/hostsurface/catalog"},
		},
	}))
	if report != "archguard: no compiler-shadow findings reported\n" {
		t.Fatalf("near-neighbor report = %q", report)
	}
}

func TestCompilerShadowPerturbationLocality(t *testing.T) {
	t.Run("new OS adapter stays in physical packages", func(t *testing.T) {
		allowed := FormatShadowReport(AnalyzeReport([]PackageRecord{{
			ImportPath: "example.com/project/internal/effect/storage/commit",
			GoFiles:    []string{"commit.go", "commit_windows.go"},
		}}))
		if allowed != "archguard: no compiler-shadow findings reported\n" {
			t.Fatalf("storage adapter report = %q", allowed)
		}
		blocked := FormatShadowReport(AnalyzeReport([]PackageRecord{{
			ImportPath: "example.com/project/internal/hostsurface/catalog",
			GoFiles:    []string{"compile.go", "compile_windows.go"},
		}}))
		if !strings.Contains(blocked, "compiler-os-specialization") {
			t.Fatalf("hostsurface OS file report = %q", blocked)
		}
	})

	t.Run("extra hostsurface catalog package remains I/O-free", func(t *testing.T) {
		allowed := FormatShadowReport(AnalyzeReport([]PackageRecord{{
			ImportPath: "example.com/project/internal/hostsurface/catalog/extra",
			Imports:    []string{"example.com/project/internal/realization/profile"},
		}}))
		if allowed != "archguard: no compiler-shadow findings reported\n" {
			t.Fatalf("extra catalog report = %q", allowed)
		}
		blocked := FormatShadowReport(AnalyzeReport([]PackageRecord{{
			ImportPath: "example.com/project/internal/hostsurface/catalog/extra",
			Imports:    []string{"example.com/project/internal/recoverygate"},
		}}))
		if !strings.Contains(blocked, "compiler-hostsurface-forbidden-import") {
			t.Fatalf("extra catalog recoverygate report = %q", blocked)
		}
	})

	t.Run("new realization package does not import barrier or compiler", func(t *testing.T) {
		allowed := FormatShadowReport(AnalyzeReport([]PackageRecord{{
			ImportPath: "example.com/project/internal/realization/aggregate/newform",
			Imports:    []string{"example.com/project/internal/target"},
		}}))
		if allowed != "archguard: no compiler-shadow findings reported\n" {
			t.Fatalf("new realization report = %q", allowed)
		}
		blockedBarrier := FormatShadowReport(AnalyzeReport([]PackageRecord{{
			ImportPath: "example.com/project/internal/realization/aggregate/newform",
			Imports:    []string{"example.com/project/internal/recoverygate"},
		}}))
		if !strings.Contains(blockedBarrier, "facet-owner-state-barrier-import") {
			t.Fatalf("realization barrier report = %q", blockedBarrier)
		}
		blockedCompiler := FormatShadowReport(AnalyzeReport([]PackageRecord{{
			ImportPath: "example.com/project/internal/realization/aggregate/newform",
			Imports:    []string{"example.com/project/internal/hostsurface/catalog"},
		}}))
		if !strings.Contains(blockedCompiler, "compiler-owner-catalog-import") {
			t.Fatalf("realization compiler report = %q", blockedCompiler)
		}
	})

	t.Run("new observe purpose does not import State Barrier", func(t *testing.T) {
		allowed := FormatShadowReport(AnalyzeReport([]PackageRecord{{
			ImportPath: "example.com/project/internal/assurance/observe/newpurpose",
			Imports:    []string{"example.com/project/internal/hostsurface/catalog"},
		}}))
		if allowed != "archguard: no compiler-shadow findings reported\n" {
			t.Fatalf("new observe report = %q", allowed)
		}
		blocked := FormatShadowReport(AnalyzeReport([]PackageRecord{{
			ImportPath: "example.com/project/internal/assurance/observe/newpurpose",
			Imports:    []string{"example.com/project/internal/recoverygate"},
		}}))
		if !strings.Contains(blocked, "observe-state-barrier-import") {
			t.Fatalf("observe barrier report = %q", blocked)
		}
	})

	t.Run("recovery hardening does not import hostsurface", func(t *testing.T) {
		allowed := FormatShadowReport(AnalyzeReport([]PackageRecord{{
			ImportPath: "example.com/project/internal/effect/fileset",
			Imports:    []string{"example.com/project/internal/effect/mutation/rootedpath"},
			GoFiles:    []string{"census.go", "census_unix.go"},
		}}))
		if allowed != "archguard: no compiler-shadow findings reported\n" {
			t.Fatalf("fileset report = %q", allowed)
		}
		blocked := FormatShadowReport(AnalyzeReport([]PackageRecord{{
			ImportPath: "example.com/project/internal/effect/fileset",
			Imports:    []string{"example.com/project/internal/hostsurface/catalog"},
		}}))
		if !strings.Contains(blocked, "fileset-hostsurface-import") {
			t.Fatalf("fileset hostsurface report = %q", blocked)
		}
	})
}
