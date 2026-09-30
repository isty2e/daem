package apply

import (
	"fmt"
	"testing"

	"github.com/isty2e/daem/internal/operationplan"
)

func TestPinRouteRetainsDescribedBindingForFollowingWork(t *testing.T) {
	t.Parallel()
	for _, global := range []bool{false, true} {
		for _, bindingDescribed := range []bool{false, true} {
			t.Run(fmt.Sprintf("global=%t/binding=%t", global, bindingDescribed), func(t *testing.T) {
				t.Parallel()
				var builder operationplan.EffectStructureBuilder
				statefile := applyStatefileSchedule{builder: &builder, bound: bindingDescribed}
				compileApplyPinRouteSchedule(&builder, &statefile, applyRouteScheduleFact{
					ref:  "pin",
					work: operationplan.RouteWork{PinChange: true, InvokesHost: true, Global: global},
				})

				following, err := builder.Compile(statefile.checkedEnsure("following"))
				if err != nil {
					t.Fatal(err)
				}
				cursor := applyScheduleTestCursor{t: t, cursor: following.Begin()}
				cursor.consume("following/ensure-validate", operationplan.EffectStepValidateDescendant)
				cursor.failFastChoice("following/ensure-outcome")
				cursor.finish()
			})
		}
	}
}

func TestApplyPinScheduleSharesProviderAndFinalBinding(t *testing.T) {
	t.Parallel()
	for _, providerPin := range []bool{false, true} {
		for _, global := range []bool{false, true} {
			t.Run(fmt.Sprintf("provider-pin=%t/global=%t", providerPin, global), func(t *testing.T) {
				t.Parallel()
				input := syntheticApplyScheduleInput(t, 0)
				input.providerRoutes = []applyRouteScheduleFact{{
					ref:  "provider",
					work: operationplan.RouteWork{InvokesHost: true, PinChange: providerPin},
				}}
				input.finalRoutes = []applyRouteScheduleFact{
					{ref: "first-pin", work: operationplan.RouteWork{InvokesHost: true, PinChange: true, Global: global}},
					{ref: "second-pin", work: operationplan.RouteWork{InvokesHost: true, PinChange: true, Global: !global}},
				}
				schedule := mustCompileSyntheticApplySchedule(t, input)
				full, err := schedule.full.LegacyDemand()
				if err != nil {
					t.Fatal(err)
				}
				final, err := schedule.final.LegacyDemand()
				if err != nil {
					t.Fatal(err)
				}
				if full.DescendantBindings() != 1 || final.DescendantBindings() != 0 {
					t.Fatalf("full/final bindings = %d/%d, want 1/0", full.DescendantBindings(), final.DescendantBindings())
				}
			})
		}
	}
}
