package clipresent

import (
	"bytes"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func nativeHumanDecision(t *testing.T, name, path, destination string, selected target.Target, scope target.Scope) reconcile.Decision {
	t.Helper()
	id, err := entity.New(entity.KindHostConfig, name)
	if err != nil {
		t.Fatal(err)
	}
	key, err := output.ParseKeyPath(path)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.HostConfigKeySubject(id, selected, scope, key)
	if err != nil {
		t.Fatal(err)
	}
	root, err := output.Parse(destination)
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	syntax := "toml"
	if selected == target.TargetPi {
		syntax = "json"
	}
	codec, _ := aggregate.HostConfigCodecForSyntax(syntax)

	contract, err := aggregate.NewHostConfigProjectionContract(selected, scope, root, footprint, codec)
	if err != nil {
		t.Fatal(err)
	}
	physical, err := reconcile.NewAggregateDecision(reconcile.AggregateDecisionInput{
		Kind: reconcile.AggregateBlocked, Reason: reconcile.ReasonAggregateLockBlocked,
		DocumentAddress: contract.Address().Document(), CodecContractID: codec,
		Projections: []reconcile.AggregateProjectionDecisionInput{{
			Kind: reconcile.AggregateBlocked, Reason: reconcile.ReasonAggregateLockBlocked, Contract: contract,
			Subjects: []reconcile.AggregateSubjectDecisionInput{{Subject: subject, Contract: contract, Kind: reconcile.AggregateBlocked, Reason: reconcile.ReasonUnmanagedOutputExists}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return mustReconciliationPlan(t, nil, []reconcile.AggregateDecision{physical}).Decisions()[0]
}

func TestNativeHumanGroupsKeepCanonicalIdentity(t *testing.T) {
	first := nativeHumanDecision(t, "preferences", "/a", ".codex/config.toml", target.TargetCodex, target.ScopeProject)
	same := nativeHumanDecision(t, "preferences", "/b", ".codex/config.toml", target.TargetCodex, target.ScopeProject)
	otherFile := nativeHumanDecision(t, "preferences", "/c", ".codex/other.toml", target.TargetCodex, target.ScopeProject)
	otherScope := nativeHumanDecision(t, "preferences", "/a", "@codex-home/config.toml", target.TargetCodex, target.ScopeGlobal)
	otherTarget := nativeHumanDecision(t, "preferences", "/a", ".pi/settings.json", target.TargetPi, target.ScopeProject)
	otherDeclaration := nativeHumanDecision(t, "other", "/d", ".codex/config.toml", target.TargetCodex, target.ScopeProject)
	input := []reconcile.Decision{first, otherFile, otherScope, otherTarget, otherDeclaration, same}
	groups := groupHumanDecisions(input, HumanOptions{})
	if len(groups) != 5 || len(groups[0].decisions) != 2 || len(input) != 6 {
		t.Fatalf("canonical document/declaration identity was merged or split: %#v", groups)
	}
	for index := range groups {
		if !groups[index].native {
			t.Fatal("a native group lost its variant")
		}
	}
	verbose := groupHumanDecisions(input, HumanOptions{Verbose: true})
	if len(verbose) != len(input) {
		t.Fatal("verbose decisions were compacted")
	}
	for _, group := range verbose {
		if group.native || len(group.decisions) != 1 {
			t.Fatal("verbose detail must stay independent")
		}
	}
}

func TestNativeHumanGroupsDoNotUseDisplayIdentity(t *testing.T) {
	firstName := strings.Repeat("x", maximumHumanErrorDetailRunes+10) + "a"
	secondName := strings.Repeat("x", maximumHumanErrorDetailRunes+10) + "b"
	first := nativeHumanDecision(t, firstName, "/a", ".codex/config.toml", target.TargetCodex, target.ScopeProject)
	second := nativeHumanDecision(t, secondName, "/b", ".codex/config.toml", target.TargetCodex, target.ScopeProject)
	groups := groupHumanDecisions([]reconcile.Decision{first, second}, HumanOptions{})
	if len(groups) != 2 {
		t.Fatal("truncated display text merged different declarations")
	}
}

func TestNativeGroupBlockKeepsCrossDeclarationAndMCPIdentity(t *testing.T) {
	first, _ := nativeHumanDecision(t, "preferences", "/a~1b", ".codex/config.toml", target.TargetCodex, target.ScopeProject).Aggregate()
	second, _ := nativeHumanDecision(t, "other", "/~0name", ".codex/config.toml", target.TargetCodex, target.ScopeProject).Aggregate()
	mcp, err := topologymcp.ProjectionSubject(target.TargetCodex, target.ScopeProject, "context7")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	printNativeGroupBlock(&out, configurationBlock{causes: []topology.SubjectID{first.Subject(), second.Subject(), mcp}}, first)
	for _, value := range []string{`key "a/b"`, `"host_config/other" key "~name"`, `"mcp-server/context7"`} {
		if !strings.Contains(out.String(), value) {
			t.Fatalf("lost cause %s: %s", value, out.String())
		}
	}
}

func TestNativeGroupBlockRetainsDocumentWideReason(t *testing.T) {
	row, _ := nativeHumanDecision(t, "preferences", "/a", ".codex/config.toml", target.TargetCodex, target.ScopeProject).Aggregate()
	var out bytes.Buffer
	printNativeGroupBlock(&out, configurationBlock{reason: reconcile.ReasonAggregateLockBlocked}, row)
	if !strings.Contains(out.String(), "document: blocked (aggregate lock blocked); apply will be refused until resolved") || strings.Contains(out.String(), "blocked by") {
		t.Fatalf("document-level refusal was invented or hidden: %s", out.String())
	}
}
