package clipresent

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/findings"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/workflow/migrate"
)

func TestCountLabelUsesExplicitUnitForms(t *testing.T) {
	for _, test := range []struct {
		count    int
		singular string
		plural   string
		want     string
	}{
		{count: 0, singular: "output", plural: "outputs", want: "0 outputs"},
		{count: 1, singular: "output", plural: "outputs", want: "1 output"},
		{count: 2, singular: "output", plural: "outputs", want: "2 outputs"},
		{count: 0, singular: "lock entry", plural: "lock entries", want: "0 lock entries"},
		{count: 1, singular: "lock entry", plural: "lock entries", want: "1 lock entry"},
		{count: 2, singular: "lock entry", plural: "lock entries", want: "2 lock entries"},
		{count: 1, singular: "history-only diagnostic", plural: "history-only diagnostics", want: "1 history-only diagnostic"},
	} {
		t.Run(test.want, func(t *testing.T) {
			if got := CountLabel(test.count, test.singular, test.plural); got != test.want {
				t.Fatalf("count label = %q, want %q", got, test.want)
			}
		})
	}
}

func TestHumanCountLabelsPreserveImportResourceUnit(t *testing.T) {
	for _, test := range []struct {
		count int
		want  string
	}{
		{count: 0, want: "import: 0 resources\n"},
		{count: 1, want: "import: 1 resource\n"},
		{count: 2, want: "import: 2 resources\n"},
	} {
		t.Run(strconv.Itoa(test.count), func(t *testing.T) {
			var output bytes.Buffer
			PrintImportPlanWithOptions(&output, ImportPlan{Label: "import", ResourceCount: test.count}, HumanOptions{})
			if !strings.HasPrefix(output.String(), test.want) {
				t.Fatalf("import output = %q, want resource count %q", output.String(), test.want)
			}
		})
	}
}

func TestHumanCountLabelsDescribeProjectionOutputsInBothModes(t *testing.T) {
	for _, test := range []struct {
		name string
		plan reconcile.Result
		want string
	}{
		{name: "empty", want: "0 output entries"},
		{name: "aggregate contribution", plan: mcpProjectionPlan(t), want: "1 output entry"},
	} {
		for _, verbose := range []bool{false, true} {
			t.Run(test.name+"/verbose="+strconv.FormatBool(verbose), func(t *testing.T) {
				var output bytes.Buffer
				PrintActionPlanWithOptions(&output, "status", HumanPlanInput{Reconciliation: test.plan}, HumanOptions{Verbose: verbose})
				header, _, _ := strings.Cut(output.String(), "\n")
				prefix := "status: " + test.want
				if header != prefix && !strings.HasPrefix(header, prefix+" (") {
					t.Fatalf("header = %q, want output count %q", header, prefix)
				}
			})
		}
	}
}

func TestHumanCountLabelsInflectDoctorAndMigrationCounts(t *testing.T) {
	for _, test := range []struct {
		count    int
		checks   string
		retained string
		claims   string
	}{
		{count: 0, checks: "0 checks", retained: "0 managed paths, 0 aggregates", claims: "0 output claims, 0 global carrier claims"},
		{count: 1, checks: "1 check", retained: "1 managed path, 1 aggregate", claims: "1 output claim, 1 global carrier claim"},
		{count: 2, checks: "2 checks", retained: "2 managed paths, 2 aggregates", claims: "2 output claims, 2 global carrier claims"},
	} {
		t.Run(strconv.Itoa(test.count), func(t *testing.T) {
			checks := make([]findings.Check, test.count)
			for index := range checks {
				checks[index] = findings.OKCheck(fmt.Sprintf("check-%d", index), "observed")
			}
			var doctor bytes.Buffer
			PrintDoctorChecksWithOptions(&doctor, checks, HumanOptions{})
			if !strings.HasPrefix(doctor.String(), "doctor: "+test.checks+" (") {
				t.Fatalf("doctor output = %q, want count %q", doctor.String(), test.checks)
			}

			var migration bytes.Buffer
			PrintStateMigration(&migration, "dry-run", migrate.StateDisclosure{
				Action: "migrate", ManagedPaths: test.count, ManagedAggregates: test.count,
				OutputClaims: test.count, CarrierClaims: test.count,
			})
			for _, line := range []string{"  retained: " + test.retained, "  authority transfer: " + test.claims} {
				if !strings.Contains(migration.String(), line+"\n") {
					t.Fatalf("migration output = %q, want count line %q", migration.String(), line)
				}
			}
		})
	}
}

func TestHumanCountLabelsCountOnlyVisibleMCPEntries(t *testing.T) {
	for _, count := range []int{0, 1, 2} {
		for _, verbose := range []bool{false, true} {
			t.Run(strconv.Itoa(count)+"/verbose="+strconv.FormatBool(verbose), func(t *testing.T) {
				statuses := []MCPStatus{{
					Subject: MCPSubject{Kind: "projection", Namespace: "claude-project-mcp", Name: "current"},
					Target:  "claude-code", Scope: "project",
					Projection: []MCPStatusDimension{{Dimension: "project_projection", State: "projected"}},
				}}
				for index := range count {
					statuses = append(statuses, MCPStatus{
						Subject: MCPSubject{Kind: "projection", Namespace: "claude-project-mcp", Name: fmt.Sprintf("missing-%d", index)},
						Target:  "claude-code", Scope: "project",
						Projection: []MCPStatusDimension{{Dimension: "project_projection", State: "missing"}},
					})
				}
				var output bytes.Buffer
				PrintMCPStatusesWithOptions(&output, statuses, HumanOptions{Verbose: verbose})
				visible := count
				if verbose {
					visible++
				}
				if visible == 0 {
					if output.Len() != 0 {
						t.Fatalf("no exceptional entries, output = %q", output.String())
					}
					return
				}
				unit := "entries"
				if visible == 1 {
					unit = "entry"
				}
				want := fmt.Sprintf("mcp status: %d %s\n", visible, unit)
				if !strings.HasPrefix(output.String(), want) || strings.Count(output.String(), "  - subject=") != visible {
					t.Fatalf("MCP output = %q, want %d visible rows and header %q", output.String(), visible, want)
				}
			})
		}
	}
}

func TestHumanCountLabelsInflectNestedDisclosures(t *testing.T) {
	decision := presentRelationOrderDecision(t, target.TargetPi, hostrelation.RuntimePrecedence)
	changes := decision.PrecedenceChanges()
	if len(changes) < 2 {
		t.Fatalf("fixture has %d precedence changes, want at least two", len(changes))
	}
	for _, test := range []struct {
		count      int
		files      string
		precedence string
		collection string
		rendering  string
	}{
		{count: 0, files: "0 files"},
		{count: 1, files: "1 file", precedence: "1 managed/foreign precedence change", collection: "1 managed-path decision", rendering: "1 textual diff"},
		{count: 2, files: "2 files", precedence: "2 managed/foreign precedence changes", collection: "2 managed-path decisions", rendering: "2 textual diffs"},
	} {
		t.Run(strconv.Itoa(test.count), func(t *testing.T) {
			var precedence bytes.Buffer
			printRelationOrderRiskChanges(&precedence, decision.Target(), decision.ClassID(), changes[:test.count], "includes")
			if test.count == 0 {
				if precedence.Len() != 0 {
					t.Fatalf("empty precedence disclosure = %q", precedence.String())
				}
			} else if !strings.Contains(precedence.String(), "includes "+test.precedence+":\n") || strings.Count(precedence.String(), "      - managed=") != test.count {
				t.Fatalf("precedence output = %q, want %q and %d rows", precedence.String(), test.precedence, test.count)
			}

			diffs := make([]DryRunDiff, test.count)
			for index := range diffs {
				diffs[index] = DryRunDiff{
					ResourceID: fmt.Sprintf("instructions/fixture-%d", index), Targets: []string{"codex"},
					Scope: "project", Destination: fmt.Sprintf("fixture-%d.md", index),
					CurrentContent: []byte("old\n"), DesiredContent: []byte("new\n"),
				}
			}
			var output bytes.Buffer
			if err := printDryRunDiffs(context.Background(), &output, DryRunDiffReport{
				Diffs: diffs, UninspectedManagedPathCount: test.count,
			}, 0); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(output.String(), "diff: "+test.files+"\n") {
				t.Fatalf("diff header = %q, want file count %q", output.String(), test.files)
			}
			if test.count == 0 {
				if strings.Contains(output.String(), "omitted") {
					t.Fatalf("empty diff report includes omissions: %q", output.String())
				}
				return
			}
			for _, countPhrase := range []string{
				"diff collection omitted " + test.collection + " ",
				"diff rendering omitted " + test.rendering + " ",
			} {
				if !strings.Contains(output.String(), countPhrase) {
					t.Fatalf("diff output = %q, want count phrase %q", output.String(), countPhrase)
				}
			}
		})
	}
}
