package hookcodec_test

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/hookdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	"github.com/isty2e/daem/internal/realization/aggregate/hook"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	testscale "github.com/isty2e/daem/test/scale"
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

func TestHookCodecObservesContributionOccupancyWithoutInventingIdentity(t *testing.T) {
	placement, codec, selection := hookCodecFixture(t)
	alpha := hookContributionSpec{name: "alpha", event: "Stop", command: "alpha"}
	beta := hookContributionSpec{name: "beta", event: "Stop", command: "beta"}
	desired := hookContributionSet(t, placement, alpha, beta)
	document := renderHookSet(
		t,
		codec,
		selection,
		aggregate.AbsentDocument(),
		hookContributionSet(t, placement, alpha),
	)
	snapshot, failure := codec.Read(document, selection)
	if failure != nil {
		t.Fatalf("Read: %v", failure)
	}
	occupancy, err := codec.ClassifyContributionOccupancy(snapshot.States()[0], desired)
	if err != nil {
		t.Fatalf("ClassifyContributionOccupancy: %v", err)
	}
	items := desired.Contributions()
	if state, covered := occupancy.State(items[0].SubjectID()); !covered || state != aggregate.ContributionPresent {
		t.Fatalf("alpha occupancy = %q covered=%t, want present", state, covered)
	}
	if state, covered := occupancy.State(items[1].SubjectID()); !covered || state != aggregate.ContributionAbsent {
		t.Fatalf("beta occupancy = %q covered=%t, want absent", state, covered)
	}

	duplicateDesired := hookContributionSet(
		t,
		placement,
		hookContributionSpec{name: "first", event: "Stop", command: "same"},
		hookContributionSpec{name: "second", event: "Stop", command: "same"},
	)
	duplicateDocument := renderHookSet(
		t,
		codec,
		selection,
		aggregate.AbsentDocument(),
		hookContributionSet(
			t,
			placement,
			hookContributionSpec{name: "physical", event: "Stop", command: "same"},
		),
	)
	duplicateSnapshot, failure := codec.Read(duplicateDocument, selection)
	if failure != nil {
		t.Fatalf("Read(duplicate): %v", failure)
	}
	duplicateOccupancy, err := codec.ClassifyContributionOccupancy(
		duplicateSnapshot.States()[0],
		duplicateDesired,
	)
	if err != nil {
		t.Fatalf("ClassifyContributionOccupancy(duplicate): %v", err)
	}
	for _, item := range duplicateDesired.Contributions() {
		if state, covered := duplicateOccupancy.State(item.SubjectID()); !covered || state != aggregate.ContributionAmbiguous {
			t.Fatalf("duplicate occupancy for %q = %q covered=%t, want ambiguous", item.SubjectID(), state, covered)
		}
	}
}

func TestHookCodecOccupancyDoesNotDuplicateSharedMatcherAllocation(t *testing.T) {
	testscale.Require(t)

	placement, codec, selection := hookCodecFixture(t)
	matcher := strings.Repeat("m", 64<<10)
	const handlerCount = 1024
	handlers := make([]string, handlerCount)
	for index := range handlers {
		handlers[index] = fmt.Sprintf(
			`{"type":"command","command":"command-%d"}`,
			index,
		)
	}
	document := aggregate.ExistingDocument([]byte(
		`{"hooks":{"Stop":[{"matcher":` + strconv.Quote(matcher) +
			`,"hooks":[` + strings.Join(handlers, ",") + `]}]}}`,
	))
	snapshot, failure := codec.Read(document, selection)
	if failure != nil {
		t.Fatalf("Read: %v", failure)
	}
	desired := hookContributionSet(t, placement, hookContributionSpec{
		name: "match", event: "Stop", matcher: matcher, command: "command-512",
	})

	assertPresent := func() {
		occupancy, err := codec.ClassifyContributionOccupancy(snapshot.States()[0], desired)
		if err != nil {
			t.Fatalf("ClassifyContributionOccupancy: %v", err)
		}
		subject := desired.Contributions()[0].SubjectID()
		if state, covered := occupancy.State(subject); !covered || state != aggregate.ContributionPresent {
			t.Fatalf("occupancy = %q covered=%t, want present", state, covered)
		}
	}
	assertPresent()

	benchmark := testing.Benchmark(func(b *testing.B) {
		for range b.N {
			if _, err := codec.ClassifyContributionOccupancy(snapshot.States()[0], desired); err != nil {
				b.Fatal(err)
			}
		}
	})
	if allocated := benchmark.AllocedBytesPerOp(); allocated >= 16<<20 {
		t.Fatalf("occupancy allocated %d bytes/op, want less than 16 MiB", allocated)
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
		{name: "excessive depth", input: strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66), reason: aggregate.CodecFailureDocumentMalformed},
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

func TestHookCodecRejectsOversizedHostDocument(t *testing.T) {
	_, codec, selection := hookCodecFixture(t)
	_, failure := codec.Read(
		aggregate.ExistingDocument(make([]byte, hookdocument.MaximumBytes+1)),
		selection,
	)
	if failure == nil || failure.Reason() != aggregate.CodecFailureDocumentMalformed {
		t.Fatalf("oversized codec failure = %v, want document_malformed", failure)
	}
}

func TestHookCodecRejectsProjectionWhoseCanonicalFormExceedsLimit(t *testing.T) {
	_, codec, selection := hookCodecFixture(t)
	matcher := strings.Repeat("<", int(hookdocument.MaximumBytes)/6+1024)
	content := []byte(
		`{"hooks":{"Stop":[{"matcher":` + strconv.Quote(matcher) +
			`,"hooks":[{"type":"command","command":"true"}]}]}}`,
	)
	if int64(len(content)) >= hookdocument.MaximumBytes {
		t.Fatalf("probe input bytes = %d, want below %d", len(content), hookdocument.MaximumBytes)
	}

	_, failure := codec.Read(aggregate.ExistingDocument(content), selection)
	if failure == nil || failure.Reason() != aggregate.CodecFailureSelectedShapeUnsupported {
		t.Fatalf("canonical expansion failure = %v, want selected_shape_unsupported", failure)
	}
}

func TestHookCodecRejectsUnrenderableLockedContributionAndSet(t *testing.T) {
	if _, err := hookcodec.CanonicalHookContribution(commandhook.ContributionInput{
		Event: strings.Repeat("e", hookdocument.MaximumEventBytes+1),
		Type:  "command", Command: "true",
	}); !errors.Is(err, hookdocument.ErrStructuralBudgetExceeded) {
		t.Fatalf("oversized event error = %v, want structural budget error", err)
	}
	if _, err := hookcodec.CanonicalHookContribution(commandhook.ContributionInput{
		Event: "Stop", Matcher: strings.Repeat("<", int(hookdocument.MaximumBytes)/6+1024),
		Type: "command", Command: "true",
	}); !errors.Is(err, hookdocument.ErrTooLarge) {
		t.Fatalf("unrenderable contribution error = %v, want document size error", err)
	}

	placement, codec, selection := hookCodecFixture(t)
	items := make([]aggregate.SubjectContribution, 0, hookdocument.MaximumEvents+1)
	for index := range hookdocument.MaximumEvents + 1 {
		canonical, err := hookcodec.CanonicalHookContribution(commandhook.ContributionInput{
			Event: fmt.Sprintf("Event%03d", index), Type: "command", Command: "true",
		})
		if err != nil {
			t.Fatal(err)
		}
		contribution, err := placement.Contribution(canonical)
		if err != nil {
			t.Fatal(err)
		}
		subject, err := topology.NewSubjectID(
			topology.SubjectProjection,
			string(placement.ID()),
			fmt.Sprintf("hook:event-%03d", index),
		)
		if err != nil {
			t.Fatal(err)
		}
		item, err := aggregate.NewSubjectContribution(subject, contribution)
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, item)
	}
	desired, err := aggregate.NewContributionSet(items)
	if err != nil {
		t.Fatal(err)
	}
	if err := codec.ValidateContributions(desired); !errors.Is(err, hookdocument.ErrStructuralBudgetExceeded) {
		t.Fatalf("ValidateContributions error = %v, want structural budget error", err)
	}

	before, failure := codec.Read(aggregate.AbsentDocument(), selection)
	if failure != nil {
		t.Fatal(failure)
	}
	intent, err := aggregate.NewProjectionIntent(before.States()[0], &desired)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := aggregate.NewPlan(before, []aggregate.ProjectionIntent{intent})
	if err != nil {
		t.Fatal(err)
	}
	if _, failure := codec.Render(aggregate.AbsentDocument(), plan); failure == nil ||
		failure.Reason() != aggregate.CodecFailureCanonicalInvalid {
		t.Fatalf("Render failure = %v, want canonical_contribution_invalid", failure)
	}
}

func TestHookCodecRejectsRenderedAndRestoredDocumentsBeyondLimit(t *testing.T) {
	placement, codec, selection := hookCodecFixture(t)
	prefix := `{"padding":"`
	suffix := `"}`
	nearLimit := aggregate.ExistingDocument([]byte(
		prefix + strings.Repeat("a", int(hookdocument.MaximumBytes)-len(prefix)-len(suffix)) + suffix,
	))
	before, failure := codec.Read(nearLimit, selection)
	if failure != nil {
		t.Fatalf("Read(near-limit): %v", failure)
	}
	desired := hookContributionSet(t, placement, hookContributionSpec{name: "stop", event: "Stop", command: "true"})
	intent, err := aggregate.NewProjectionIntent(before.States()[0], &desired)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := aggregate.NewPlan(before, []aggregate.ProjectionIntent{intent})
	if err != nil {
		t.Fatal(err)
	}
	if _, failure := codec.Render(nearLimit, plan); failure == nil || failure.Reason() != aggregate.CodecFailureCanonicalInvalid {
		t.Fatalf("near-limit Render failure = %v, want canonical_contribution_invalid", failure)
	}

	baselineDocument := renderHookSet(t, codec, selection, aggregate.AbsentDocument(), desired)
	baseline, failure := codec.Read(baselineDocument, selection)
	if failure != nil {
		t.Fatalf("Read(baseline): %v", failure)
	}
	if _, failure := codec.Restore(nearLimit, baseline); failure == nil || failure.Reason() != aggregate.CodecFailureCanonicalInvalid {
		t.Fatalf("near-limit Restore failure = %v, want canonical_contribution_invalid", failure)
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
