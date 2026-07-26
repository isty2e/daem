package hookcodec_test

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	"github.com/isty2e/daem/internal/realization/aggregate/hook"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestHookCodecFoldsSubjectSortedContributionsAndPreservesSiblings(t *testing.T) {
	placement, codec, selection := hookCodecFixture(t)
	before, failure := codec.Read(
		aggregate.ExistingDocument([]byte(`{"env":{"TOKEN":"keep"}}`)),
		selection,
	)
	if failure != nil {
		t.Fatalf("Read: %v", failure)
	}

	desired := hookContributionSet(
		t, placement,
		hookContributionSpec{name: "zeta", event: "Stop", command: "make test"},
		hookContributionSpec{name: "alpha", event: "PreToolUse", matcher: "Write", command: "make fmt"},
	)
	intent, err := aggregate.NewProjectionIntent(before.States()[0], &desired)
	if err != nil {
		t.Fatalf("NewProjectionIntent: %v", err)
	}
	plan, err := aggregate.NewPlan(before, []aggregate.ProjectionIntent{intent})
	if err != nil {
		t.Fatalf("NewPlan: %v", err)
	}
	rendered, failure := codec.Render(
		aggregate.ExistingDocument([]byte(`{"env":{"TOKEN":"keep"}}`)),
		plan,
	)
	if failure != nil {
		t.Fatalf("Render: %v", failure)
	}
	content := string(rendered.Document().Content())
	for _, want := range []string{`"env": {`, `"TOKEN": "keep"`, `"hooks": {`, `"PreToolUse": [`, `"Stop": [`} {
		if !strings.Contains(content, want) {
			t.Fatalf("rendered content missing %q:\n%s", want, content)
		}
	}
	if !rendered.Expected().States()[0].Present() {
		t.Fatal("rendered expected projection is absent")
	}

	readBack, failure := codec.Read(rendered.Document(), selection)
	if failure != nil {
		t.Fatalf("Read(rendered): %v", failure)
	}
	if readBack.States()[0].CanonicalProjection() != rendered.Expected().States()[0].CanonicalProjection() {
		t.Fatal("rendered postcondition does not match codec reread")
	}
}

func TestHookCodecPartialAndFinalRemoval(t *testing.T) {
	placement, codec, selection := hookCodecFixture(t)
	all := hookContributionSet(
		t, placement,
		hookContributionSpec{name: "alpha", event: "Stop", command: "alpha"},
		hookContributionSpec{name: "beta", event: "Stop", command: "beta"},
	)
	document := renderHookSet(t, codec, selection, aggregate.AbsentDocument(), all)
	before, failure := codec.Read(document, selection)
	if failure != nil {
		t.Fatalf("Read: %v", failure)
	}

	one := hookContributionSet(t, placement, hookContributionSpec{name: "beta", event: "Stop", command: "beta"})
	partialIntent, err := aggregate.NewProjectionIntent(before.States()[0], &one)
	if err != nil {
		t.Fatalf("NewProjectionIntent(partial): %v", err)
	}
	partialPlan, err := aggregate.NewPlan(before, []aggregate.ProjectionIntent{partialIntent})
	if err != nil {
		t.Fatalf("NewPlan(partial): %v", err)
	}
	partial, failure := codec.Render(document, partialPlan)
	if failure != nil {
		t.Fatalf("Render(partial): %v", failure)
	}
	if strings.Contains(string(partial.Document().Content()), `"command": "alpha"`) ||
		!strings.Contains(string(partial.Document().Content()), `"command": "beta"`) {
		t.Fatalf("partial content = %s", partial.Document().Content())
	}

	partialBefore, failure := codec.Read(partial.Document(), selection)
	if failure != nil {
		t.Fatalf("Read(partial): %v", failure)
	}
	removeIntent, err := aggregate.NewProjectionIntent(partialBefore.States()[0], nil)
	if err != nil {
		t.Fatalf("NewProjectionIntent(remove): %v", err)
	}
	removePlan, err := aggregate.NewPlan(partialBefore, []aggregate.ProjectionIntent{removeIntent})
	if err != nil {
		t.Fatalf("NewPlan(remove): %v", err)
	}
	removed, failure := codec.Render(partial.Document(), removePlan)
	if failure != nil {
		t.Fatalf("Render(remove): %v", failure)
	}
	if removed.Document().Exists() {
		t.Fatalf("final removal retained empty document: %s", removed.Document().Content())
	}
}

func TestHookCodecRejectsDuplicateAndMalformedSelectedShapes(t *testing.T) {
	_, codec, selection := hookCodecFixture(t)
	for _, test := range []struct {
		name   string
		input  string
		reason aggregate.CodecFailureReason
	}{
		{name: "duplicate top-level", input: `{"hooks":{},"hooks":{}}`, reason: aggregate.CodecFailureDuplicateKey},
		{name: "duplicate nested", input: `{"hooks":{"Stop":[]},"meta":{"x":1,"x":2}}`, reason: aggregate.CodecFailureDuplicateKey},
		{name: "empty", input: ``, reason: aggregate.CodecFailureDocumentMalformed},
		{name: "hooks array", input: `{"hooks":[]}`, reason: aggregate.CodecFailureSelectedShapeUnsupported},
		{name: "unknown handler field", input: `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"ok","secret":true}]}]}}`, reason: aggregate.CodecFailureSelectedShapeUnsupported},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, failure := codec.Read(aggregate.ExistingDocument([]byte(test.input)), selection)
			if failure == nil || failure.Reason() != test.reason {
				t.Fatalf("failure = %v, want %q", failure, test.reason)
			}
		})
	}
}

func TestHookCodecRestorePreservesConcurrentUnmanagedSibling(t *testing.T) {
	_, codec, selection := hookCodecFixture(t)
	absent, failure := codec.Read(aggregate.AbsentDocument(), selection)
	if failure != nil {
		t.Fatalf("Read(absent): %v", failure)
	}
	restored, failure := codec.Restore(
		aggregate.ExistingDocument([]byte(`{"external":{"keep":true},"hooks":{"Stop":[]}}`)),
		absent,
	)
	if failure != nil {
		t.Fatalf("Restore: %v", failure)
	}
	content := string(restored.Document().Content())
	if !strings.Contains(content, `"external": {`) || strings.Contains(content, `"hooks"`) {
		t.Fatalf("restored content = %s", content)
	}
	if !restored.Expected().DocumentExisted() || restored.Expected().States()[0].Present() {
		t.Fatalf("restored expected = %#v", restored.Expected())
	}
}

type hookContributionSpec struct {
	name    string
	event   string
	matcher string
	command string
}

func hookCodecFixture(t *testing.T) (aggregate.HookPlacement, aggregate.Codec, aggregate.Selection) {
	t.Helper()
	placement, ok := aggregate.HookPlacementFor(target.TargetCodex, target.ScopeProject)
	if !ok {
		t.Fatal("HookPlacementFor returned false")
	}
	canonical, err := hookcodec.CanonicalHookContribution(commandhook.ContributionInput{
		Event: "Stop", Type: "command", Command: "true",
	})
	if err != nil {
		t.Fatalf("CanonicalHookContribution: %v", err)
	}
	contribution, err := placement.Contribution(canonical)
	if err != nil {
		t.Fatalf("Contribution: %v", err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contribution.Contract()})
	if err != nil {
		t.Fatalf("NewSelection: %v", err)
	}
	codec, ok := aggregatecodec.Catalog().Lookup(placement.CodecContractID())
	if !ok {
		t.Fatal("CodecFor returned false")
	}
	return placement, codec, selection
}

func hookContributionSet(t *testing.T, placement aggregate.HookPlacement, specs ...hookContributionSpec) aggregate.ContributionSet {
	t.Helper()
	items := make([]aggregate.SubjectContribution, 0, len(specs))
	for _, spec := range specs {
		canonical, err := hookcodec.CanonicalHookContribution(commandhook.ContributionInput{
			Event: spec.event, Matcher: spec.matcher, Type: "command", Command: spec.command,
		})
		if err != nil {
			t.Fatalf("CanonicalHookContribution(%s): %v", spec.name, err)
		}
		contribution, err := placement.Contribution(canonical)
		if err != nil {
			t.Fatalf("Contribution(%s): %v", spec.name, err)
		}
		subject, err := topology.NewSubjectID(topology.SubjectProjection, string(placement.ID()), "hook:"+spec.name)
		if err != nil {
			t.Fatalf("NewSubjectID(%s): %v", spec.name, err)
		}
		item, err := aggregate.NewSubjectContribution(subject, contribution)
		if err != nil {
			t.Fatalf("NewSubjectContribution(%s): %v", spec.name, err)
		}
		items = append(items, item)
	}
	set, err := aggregate.NewContributionSet(items)
	if err != nil {
		t.Fatalf("NewContributionSet: %v", err)
	}
	return set
}

func renderHookSet(
	t *testing.T,
	codec aggregate.Codec,
	selection aggregate.Selection,
	document aggregate.Document,
	desired aggregate.ContributionSet,
) aggregate.Document {
	t.Helper()
	before, failure := codec.Read(document, selection)
	if failure != nil {
		t.Fatalf("Read: %v", failure)
	}
	intent, err := aggregate.NewProjectionIntent(before.States()[0], &desired)
	if err != nil {
		t.Fatalf("NewProjectionIntent: %v", err)
	}
	plan, err := aggregate.NewPlan(before, []aggregate.ProjectionIntent{intent})
	if err != nil {
		t.Fatalf("NewPlan: %v", err)
	}
	rendered, failure := codec.Render(document, plan)
	if failure != nil {
		t.Fatalf("Render: %v", failure)
	}
	return rendered.Document()
}
