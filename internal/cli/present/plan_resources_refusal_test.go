package clipresent

import (
	"bytes"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func TestResourcePlanNamesOnlyOtherConfigurationBlockers(t *testing.T) {
	for _, test := range []struct {
		name    string
		names   []string
		blocked []bool
		causes  []string
	}{
		{name: "self only", names: []string{"filesystem"}, blocked: []bool{true}, causes: []string{""}},
		{name: "current sibling", names: []string{"context7", "filesystem"}, blocked: []bool{false, true}, causes: []string{"filesystem", ""}},
		{name: "mutual conflicts", names: []string{"context7", "filesystem"}, blocked: []bool{true, true}, causes: []string{"filesystem", "context7"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := configurationBlockPresentationPlan(t, test.names, test.blocked)
			var output bytes.Buffer
			PrintStatusPlanWithOptions(&output, HumanPlanInput{Reconciliation: plan}, HumanOptions{})
			for index, name := range test.names {
				block := resourcePlanOutputBlock(t, output.String(), name)
				if test.blocked[index] && !strings.Contains(block, "blocked: unmanaged output exists") {
					t.Fatalf("resource %q lost its own blocker: %s", name, block)
				}
				var annotation string
				for line := range strings.Lines(block) {
					if strings.Contains(line, "configuration:") || strings.Contains(line, "configuration document:") {
						annotation = line
					}
				}
				if test.causes[index] == "" {
					if annotation != "" {
						t.Fatalf("resource %q repeats its own cause: %s", name, annotation)
					}
					continue
				}
				if !strings.Contains(annotation, `blocked by "mcp-server/`+test.causes[index]+`"`) ||
					strings.Contains(annotation, `"mcp-server/`+name+`"`) ||
					!strings.Contains(annotation, "apply will be refused until resolved") {
					t.Fatalf("resource %q has misleading shared cause: %q", name, annotation)
				}
			}
		})
	}
}

func TestResourcePlanRetainsDocumentWideBlockWithoutSubjectCause(t *testing.T) {
	plan := configurationBlockPresentationPlan(t, []string{"context7"}, []bool{false})
	var output bytes.Buffer
	PrintStatusPlanWithOptions(&output, HumanPlanInput{Reconciliation: plan}, HumanOptions{})
	if !strings.Contains(output.String(), "configuration document: blocked (aggregate lock blocked); apply will be refused until resolved") {
		t.Fatalf("document-wide reason was hidden: %s", output.String())
	}
}

func TestConfigurationBlockDeduplicatesDisplayedCausesWithoutUsingDisplayIdentityForSelf(t *testing.T) {
	current, err := topologymcp.ProjectionSubject(target.TargetClaudeCode, target.ScopeProject, "context7")
	if err != nil {
		t.Fatal(err)
	}
	projectCause, err := topologymcp.ProjectionSubject(target.TargetClaudeCode, target.ScopeProject, "filesystem")
	if err != nil {
		t.Fatal(err)
	}
	globalCause, err := topologymcp.ProjectionSubject(target.TargetClaudeCode, target.ScopeGlobal, "filesystem")
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	printConfigurationBlock(&output, configurationBlock{
		subjectCount: 3, causes: []topology.SubjectID{current, projectCause, globalCause, projectCause},
	}, current)
	want := "    shared configuration: blocked by \"mcp-server/filesystem\"; apply will be refused until resolved\n"
	if output.String() != want {
		t.Fatalf("displayed causes = %q, want %q", output.String(), want)
	}

	output.Reset()
	printConfigurationBlock(&output, configurationBlock{
		subjectCount: 2, causes: []topology.SubjectID{projectCause, globalCause},
	}, projectCause)
	if output.String() != want {
		t.Fatalf("a different subject with the same resource reference disappeared: %q", output.String())
	}
}

func TestDelegatePlanLimitationsFollowDispositionNotAttemptScheduling(t *testing.T) {
	for _, test := range []struct {
		name         string
		dispositions []reconcile.DelegateDisposition
		context      reconcile.OperationContext
		wantFooter   bool
	}{
		{name: "empty", context: reconcile.ContextApply},
		{name: "all blocked", dispositions: []reconcile.DelegateDisposition{reconcile.DelegateBlocked, reconcile.DelegateBlocked}, context: reconcile.ContextApply},
		{name: "scheduled", dispositions: []reconcile.DelegateDisposition{reconcile.DelegateScheduled}, context: reconcile.ContextApply, wantFooter: true},
		{name: "mixed", dispositions: []reconcile.DelegateDisposition{reconcile.DelegateBlocked, reconcile.DelegateScheduled}, context: reconcile.ContextApply, wantFooter: true},
		{name: "dry-run", dispositions: []reconcile.DelegateDisposition{reconcile.DelegateSkipped}, context: reconcile.ContextDryRun, wantFooter: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var actions []reconcile.DelegateAction
			for index, disposition := range test.dispositions {
				base := delegatePresentationAction(t)
				name := []string{"context7", "filesystem"}[index]
				subject, err := topologymcp.ProjectionSubject(target.TargetClaudeCode, target.ScopeProject, name)
				if err != nil {
					t.Fatal(err)
				}
				var risks []reconcile.DelegateRisk
				if disposition == reconcile.DelegateBlocked {
					risks = []reconcile.DelegateRisk{{Code: reconcile.DelegateRiskMissingRunner, Severity: reconcile.DelegateRiskBlock, Subject: "npx"}}
				} else if disposition == reconcile.DelegateSkipped {
					risks = []reconcile.DelegateRisk{{Code: reconcile.DelegateRiskDryRunDisclosure, Severity: reconcile.DelegateRiskInfo, Subject: "npx"}}
				}
				action, err := reconcile.NewDelegateAction(reconcile.DelegateActionInput{
					Subject: subject, Target: base.Target(), Scope: base.Scope(), Plan: base.Plan(), Disposition: disposition, Risks: risks,
				})
				if err != nil {
					t.Fatal(err)
				}
				actions = append(actions, action)
			}
			plan, err := reconcile.NewResult(reconcile.ResultInput{Context: test.context, Delegates: actions})
			if err != nil {
				t.Fatal(err)
			}
			for _, verbose := range []bool{false, true} {
				var grouped, standalone bytes.Buffer
				options := HumanOptions{Verbose: verbose}
				PrintActionPlanWithOptions(&grouped, "apply", HumanPlanInput{Reconciliation: plan}, options)
				PrintDelegateActionsWithOptions(&standalone, actions, options)
				for _, text := range []string{grouped.String(), standalone.String()} {
					count := strings.Count(text, "package, cache, server, auth, and future readiness are not guaranteed")
					if count > 1 || (count == 1) != test.wantFooter {
						t.Fatalf("verbose=%t footer count=%d want=%t: %s", verbose, count, test.wantFooter, text)
					}
					if len(actions) != 0 && !strings.Contains(text, `command="npx" args=["-y","@upstash/context7-mcp"] env_bindings=[] environment=inherit`) {
						t.Fatalf("verbose=%t lost command disclosure: %s", verbose, text)
					}
				}
			}
		})
	}
}

func TestBlockedDelegatePlanDoesNotRepeatItsDisposition(t *testing.T) {
	base := delegatePresentationAction(t)
	action, err := reconcile.NewDelegateAction(reconcile.DelegateActionInput{
		Subject: base.Subject(), Target: base.Target(), Scope: base.Scope(), Plan: base.Plan(),
		Disposition: reconcile.DelegateBlocked,
		Risks: []reconcile.DelegateRisk{{
			Code: reconcile.DelegateRiskPreconditionBlocked, Severity: reconcile.DelegateRiskBlock,
			Subject: "projection:" + base.Subject().String(),
		}},
		Dependencies: []reconcile.DelegateDependency{{Kind: reconcile.DelegateDependencyProjection, Subject: base.Subject()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	PrintDelegateActionsWithOptions(&output, []reconcile.DelegateAction{action}, HumanOptions{})
	if !strings.Contains(output.String(), "command plan: blocked (configuration prerequisite)\n") {
		t.Fatalf("blocked prerequisite summary is redundant: %s", output.String())
	}
}

func configurationBlockPresentationPlan(t *testing.T, names []string, blocked []bool) reconcile.Result {
	t.Helper()
	contract := mcpProjectionPlan(t).Aggregates()[0].Contracts()[0]
	var subjects []reconcile.AggregateSubjectDecisionInput
	for index, name := range names {
		subject, err := topologymcp.ProjectionSubject(target.TargetClaudeCode, target.ScopeProject, name)
		if err != nil {
			t.Fatal(err)
		}
		kind, reason := reconcile.AggregateNoOp, reconcile.ReasonAlreadyCurrent
		if blocked[index] {
			kind, reason = reconcile.AggregateBlocked, reconcile.ReasonUnmanagedOutputExists
		}
		subjects = append(subjects, reconcile.AggregateSubjectDecisionInput{Subject: subject, Contract: contract, Kind: kind, Reason: reason})
	}
	document, err := reconcile.NewAggregateDecision(reconcile.AggregateDecisionInput{
		Kind: reconcile.AggregateBlocked, Reason: reconcile.ReasonAggregateLockBlocked,
		DocumentAddress: contract.Address().Document(), CodecContractID: contract.CodecContractID(),
		Projections: []reconcile.AggregateProjectionDecisionInput{{
			Kind: reconcile.AggregateBlocked, Reason: reconcile.ReasonAggregateLockBlocked, Contract: contract, Subjects: subjects,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return mustReconciliationPlan(t, nil, []reconcile.AggregateDecision{document})
}

func resourcePlanOutputBlock(t *testing.T, text, name string) string {
	t.Helper()
	_, block, present := strings.Cut(text, `  resource="mcp-server/`+name+`" target=claude-code scope=project`)
	if !present {
		t.Fatalf("resource %q is absent: %s", name, text)
	}
	block, _, _ = strings.Cut(block, "\n  resource=")
	return block
}
