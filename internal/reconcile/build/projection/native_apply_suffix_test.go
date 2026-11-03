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
	}{
		{"positive updates and retirement", `{"a":1,"z":1,"foreign":true}`, []nativePrefixKey{{"settings", "a", "1"}, {"settings", "z", "1"}}, []nativePrefixKey{{"settings", "a", "2"}, {"settings", "b", "3"}}},
		{"retiring only", `{"z":1,"foreign":true}`, []nativePrefixKey{{"settings", "z", "1"}}, nil},
		{"absent retiring key", `{"foreign":true}`, []nativePrefixKey{{"settings", "z", "1"}}, nil},
		{"no retirement", `{"a":1}`, []nativePrefixKey{{"settings", "a", "1"}}, []nativePrefixKey{{"settings", "a", "2"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			decisions, err := BuildAggregateDecisions(nativePrefixInput(t, test.before, test.previous, test.desired))
			if err != nil {
				t.Fatal(err)
			}
			complete := onlyAggregateDecision(t, decisions)
			prefix, err := BuildPositiveNativePrefix(t.Context(), decisions)
			if err != nil {
				t.Fatal(err)
			}
			live := complete.BeforeDocument()
			if len(prefix) != 0 {
				live = onlyAggregateDecision(t, prefix).Rendered().Document()
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
