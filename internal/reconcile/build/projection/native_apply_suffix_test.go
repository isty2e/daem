package projection

import (
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestNativeSuffixCompletesPositivePrefixFromFreshValues(t *testing.T) {
	for _, test := range []struct {
		name, before string
		previous     []nativePrefixKey
		desired      []nativePrefixKey
		prefix       string
	}{
		{"positive updates and retirement", `{"a":1,"z":1,"foreign":true}`, []nativePrefixKey{{"settings", "a", "1"}, {"settings", "z", "1"}}, []nativePrefixKey{{"settings", "a", "2"}, {"settings", "b", "3"}}, ""},
		{"retiring only", `{"z":1,"foreign":true}`, []nativePrefixKey{{"settings", "z", "1"}}, nil, ""},
		{"absent retiring key", `{"foreign":true}`, []nativePrefixKey{{"settings", "z", "1"}}, nil, ""},
		{"no retirement", `{"a":1}`, []nativePrefixKey{{"settings", "a", "1"}}, []nativePrefixKey{{"settings", "a", "2"}}, ""},
		{"empty object ancestor", `{"a":{},"z":1,"foreign":true}`, []nativePrefixKey{{"settings", "a", "{}"}, {"settings", "z", "1"}}, []nativePrefixKey{{"settings", "/a/b", "true"}}, `{"a":{"b":true},"z":1,"foreign":true}`},
		{"scalar ancestor", `{"a":1,"z":1,"foreign":true}`, []nativePrefixKey{{"settings", "a", "1"}, {"settings", "z", "1"}}, []nativePrefixKey{{"settings", "/a/b", "true"}}, `{"a":{"b":true},"z":1,"foreign":true}`},
		{"array ancestor", `{"a":[1,2],"z":1}`, []nativePrefixKey{{"settings", "a", "[1,2]"}, {"settings", "z", "1"}}, []nativePrefixKey{{"settings", "/a/b", "true"}}, `{"a":{"b":true},"z":1}`},
		{"branching descendants", `{"a":{},"z":1}`, []nativePrefixKey{{"settings", "a", "{}"}, {"settings", "z", "1"}}, []nativePrefixKey{{"settings", "/a/b", "true"}, {"settings", "/a/c", "2"}}, `{"a":{"b":true,"c":2},"z":1}`},
		{"deep descendant", `{"a":{},"z":1}`, []nativePrefixKey{{"settings", "a", "{}"}, {"settings", "z", "1"}}, []nativePrefixKey{{"settings", "/a/b/c", "true"}}, `{"a":{"b":{"c":true}},"z":1}`},
		{"literal lookalikes", `{"a":{},"z":1}`, []nativePrefixKey{{"settings", "a", "{}"}, {"settings", "z", "1"}}, []nativePrefixKey{{"settings", "a/b", "true"}, {"settings", "ab", "2"}}, `{"a":{},"a/b":true,"ab":2,"z":1}`},
		{"escaped ancestor", `{"a/b":{},"z":1}`, []nativePrefixKey{{"settings", "a/b", "{}"}, {"settings", "z", "1"}}, []nativePrefixKey{{"settings", "/a~1b/c", "true"}}, `{"a/b":{"c":true},"z":1}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			decisions, err := BuildAggregateDecisions(nativePrefixInput(t, test.before, test.previous, test.desired))
			if err != nil {
				t.Fatal(err)
			}
			complete := onlyAggregateDecision(t, decisions)
			if complete.IsBlocked() {
				t.Fatalf("fixture blocked: %s: %s", complete.Reason(), complete.Detail())
			}
			prefix, err := BuildPositiveNativePrefix(t.Context(), decisions)
			if err != nil {
				t.Fatal(err)
			}
			live := complete.BeforeDocument()
			if len(prefix) != 0 {
				live = onlyAggregateDecision(t, prefix).Rendered().Document()
			}
			if test.prefix != "" {
				assertNativePrefixJSON(t, live, test.prefix)
			}
			suffix, err := BuildNativeApplySuffix(t.Context(), decisions)
			if err != nil {
				t.Fatal(err)
			}
			if len(suffix) != 0 {
				residual := onlyAggregateDecision(t, suffix)
				if !residual.BeforeDocument().Equal(complete.BeforeDocument()) {
					t.Fatal("suffix builder fabricated an observed post-prefix Before")
				}
				operation, _ := residual.NativeOperationContract()
				selection, err := residual.CodecPlan().Before().Selection()
				if err != nil {
					t.Fatal(err)
				}
				fresh, failure := operation.Codec().Read(live, selection)
				if failure != nil {
					t.Fatal(failure)
				}
				var intents []aggregate.ProjectionIntent
				for _, projection := range residual.Projections() {
					if _, present := projection.Desired(); present {
						t.Fatal("positive key entered the retirement subset")
					}
					before, _ := fresh.State(projection.Contract())
					intent, err := aggregate.NewProjectionIntent(before, nil)
					if err != nil {
						t.Fatal(err)
					}
					intents = append(intents, intent)
				}
				plan, err := aggregate.NewPlan(fresh, intents)
				if err != nil {
					t.Fatal(err)
				}
				after, failure := operation.Codec().Render(live, plan)
				if failure != nil {
					t.Fatal(failure)
				}
				live = after.Document()
			}
			assertNativePrefixJSON(t, live, string(complete.Rendered().Document().Content()))
		})
	}
}
