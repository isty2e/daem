package operationplan

import "testing"

func TestEffectNamespacePreservesIndependentSegmentExecution(t *testing.T) {
	t.Parallel()
	var source EffectStructureBuilder
	segment := source.ForwardPhase("direct", EffectSequence(
		source.Repeat(2, source.Step("observe", EffectStepObservation)),
		source.Choice(
			"run",
			source.Trigger("cleanup", source.Step("effect", EffectStepForwardEffect)),
			source.Step("skip", EffectStepNoOp),
		),
		source.Conditional("cleanup", source.Step("finish", EffectStepCleanup)),
	))
	original, err := source.Compile(segment)
	if err != nil {
		t.Fatal(err)
	}
	var builder EffectStructureBuilder
	combined, err := builder.Compile(EffectSequence(builder.Namespace("prefix", segment), builder.Namespace("suffix", segment)))
	if err != nil {
		t.Fatal(err)
	}
	cursor := combined.Begin()
	for _, prefix := range []string{"prefix", "suffix"} {
		for range 2 {
			if err := cursor.Consume(prefix+"/observe", EffectStepObservation); err != nil {
				t.Fatal(err)
			}
		}
		alternative := 0
		if prefix == "suffix" {
			alternative = 1
		}
		if err := cursor.SelectAlternative(prefix+"/run", alternative); err != nil {
			t.Fatal(err)
		}
		if alternative == 0 {
			if err := cursor.Consume(prefix+"/effect", EffectStepForwardEffect); err != nil {
				t.Fatal(err)
			}
			if err := cursor.Consume(prefix+"/finish", EffectStepCleanup); err != nil {
				t.Fatal(err)
			}
		} else if err := cursor.Consume(prefix+"/skip", EffectStepNoOp); err != nil {
			t.Fatal(err)
		}
	}
	if err := cursor.FinishSuccess(); err != nil {
		t.Fatal("one segment's trigger leaked into its peer", err)
	}
	demand, err := combined.LegacyDemand()
	if err != nil || demand.EnsureCalls() != 2 || demand.StateDirValidationCalls() != 0 {
		t.Fatal("namespacing changed the two phases' demand", err)
	}
	retained, err := source.Compile(segment)
	if err != nil || !retained.Equal(original) {
		t.Fatal("namespacing mutated the source segment", err)
	}
	if _, err := builder.Compile(EffectSequence(builder.Namespace("same", segment), builder.Namespace("same", segment))); err == nil {
		t.Fatal("namespacing disabled duplicate-reference detection")
	}
}

func TestEffectNamespaceRejectsInvalidReference(t *testing.T) {
	t.Parallel()
	var builder EffectStructureBuilder
	node := builder.Namespace(" ", builder.Step("observe", EffectStepObservation))
	if _, err := builder.Compile(node); err == nil {
		t.Fatal("invalid namespace produced a usable structure")
	}
}
