package operationplan

import (
	"fmt"
	"testing"
)

func TestPinChangeStructurePreservesConservativeDemand(t *testing.T) {
	t.Parallel()
	for _, global := range []bool{false, true} {
		for _, bindingDescribed := range []bool{false, true} {
			t.Run(fmt.Sprintf("global=%t/binding=%t", global, bindingDescribed), func(t *testing.T) {
				t.Parallel()
				var builder EffectStructureBuilder
				structure, err := builder.Compile(builder.ForwardPhase("pin-phase", PinChangeEffectNode(&builder, "pin", global, bindingDescribed)))
				if err != nil {
					t.Fatal(err)
				}
				bound, err := structure.legacyUpperBound()
				if err != nil {
					t.Fatal(err)
				}

				want := effectDemand{ensureCalls: 1, descendantValidations: 6, descendantFileCommits: 3}
				if global {
					want.descendantValidations, want.descendantFileCommits = 9, 1
				}
				if !bindingDescribed {
					want.descendantBindings = 1
				}
				if bound != want {
					t.Fatalf("conservative demand = %+v, want %+v", bound, want)
				}
			})
		}
	}
}

func TestHostRoutePinDemandRetainsExistingAdmission(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		routes  []RouteWork
		want    statefileDemand
		wantErr string
	}{
		{name: "empty"},
		{name: "passive", routes: []RouteWork{{Global: true}}},
		{
			name: "mixed_scopes_and_other_work",
			routes: []RouteWork{
				{PinChange: true, InvokesHost: true},
				{PinChange: true, InvokesHost: true, Global: true},
				{InvokesHost: true},
				{PinChange: true, InvokesHost: true},
				{Promotion: true},
				{PinChange: true, InvokesHost: true, Global: true},
				{InvokesHost: true, Global: true},
			},
			want: statefileDemand{validations: 51, commits: 17},
		},
		{
			name:    "pin_without_invocation",
			routes:  []RouteWork{{PinChange: true}},
			wantErr: "operationplan: pin change requires an invocation without install promotion",
		},
		{
			name:    "pin_with_promotion",
			routes:  []RouteWork{{PinChange: true, InvokesHost: true, Promotion: true}},
			wantErr: "operationplan: pin change requires an invocation without install promotion",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := hostRouteStatefileDemand(test.routes)
			if test.wantErr != "" {
				if err == nil || err.Error() != test.wantErr {
					t.Fatalf("demand error = %v, want %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("demand = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestPinChangeCursorPreservesSettlementOrder(t *testing.T) {
	t.Parallel()
	for _, global := range []bool{false, true} {
		for _, binding := range []string{"initial_bind", "initial_validate", "retained"} {
			for _, complete := range []bool{false, true} {
				t.Run(fmt.Sprintf("global=%t/%s/complete=%t", global, binding, complete), func(t *testing.T) {
					t.Parallel()
					var builder EffectStructureBuilder
					structure, err := builder.Compile(builder.ForwardPhase("pin-phase", PinChangeEffectNode(&builder, "pin", global, binding == "retained")))
					if err != nil {
						t.Fatal(err)
					}
					cursor := structure.Begin()
					consume := func(ref string, kind EffectStepKind) {
						t.Helper()
						if err := cursor.Consume(ref, kind); err != nil {
							t.Fatal(err)
						}
					}
					selectAlternative := func(ref string, alternative int) {
						t.Helper()
						if err := cursor.SelectAlternative(ref, alternative); err != nil {
							t.Fatal(err)
						}
					}
					outcome := func(ref string) {
						t.Helper()
						selectAlternative(ref, 0)
						consume(ref+"/success", EffectStepNoOp)
					}
					checked := func(ref string, kind EffectStepKind) {
						t.Helper()
						consume(ref, kind)
						outcome(ref + "/outcome")
					}
					registry := func(ref string) {
						t.Helper()
						checked(ref+"/declarations-before", EffectStepObservation)
						checked(ref+"/root-before", EffectStepObservation)
						checked(ref+"/statefile-before/validate", EffectStepValidateDescendant)
						checked(ref+"/registry", EffectStepPersistence)
						checked(ref+"/statefile-after/validate", EffectStepValidateDescendant)
						checked(ref+"/visibility", EffectStepObservation)
						checked(ref+"/root-after", EffectStepObservation)
						checked(ref+"/declarations-after", EffectStepObservation)
					}

					consume("pin/forward", EffectStepForwardEffect)
					outcome("pin/forward/outcome")
					switch binding {
					case "initial_bind":
						selectAlternative("pin/statefile/initial-authority", 0)
						consume("pin/statefile/bind", EffectStepBindDescendant)
					case "initial_validate":
						selectAlternative("pin/statefile/initial-authority", 1)
						consume("pin/statefile/validate-existing", EffectStepValidateDescendant)
					case "retained":
						consume("pin/statefile/ensure-validate", EffectStepValidateDescendant)
					}
					outcome("pin/statefile/ensure-outcome")
					checked("pin/pin/binding", EffectStepObservation)
					if global {
						registry("pin/pin/reserve")
					} else {
						checked("pin/pin/reserve/publish", EffectStepPublishDescendant)
					}
					checked("pin/pin/pre-host/validate", EffectStepValidateDescendant)
					consume("pin/pin/host", EffectStepExternal)
					checked("pin/pin/post-host/validate", EffectStepValidateDescendant)
					for _, stage := range []string{"observe", "classify", "record", "project-root"} {
						checked("pin/pin/"+stage, EffectStepObservation)
					}

					if complete {
						selectAlternative("pin/pin/settlement", 0)
						if global {
							registry("pin/pin/complete")
						} else {
							checked("pin/pin/pre-completion/validate", EffectStepValidateDescendant)
							checked("pin/pin/complete/publish", EffectStepPublishDescendant)
						}
					} else {
						selectAlternative("pin/pin/settlement", 1)
						consume("pin/pin/retain", EffectStepNoOp)
					}
					checked("pin/pin/pre-attempt/validate", EffectStepValidateDescendant)
					checked("pin/pin/attempt/publish", EffectStepPublishDescendant)
					checked("pin/pin/post-attempt/validate", EffectStepValidateDescendant)
					checked("pin/pin/final-project-root", EffectStepObservation)
					checked("pin/pin/final-declarations", EffectStepObservation)
					outcome("pin/pin/outcome")
					if err := cursor.FinishSuccess(); err != nil {
						t.Fatal(err)
					}
				})
			}
		}
	}
}

func TestPinChangeCursorRequiresFailFastHandoff(t *testing.T) {
	t.Parallel()
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprintf("global=%t", global), func(t *testing.T) {
			t.Parallel()
			var builder EffectStructureBuilder
			structure, err := builder.Compile(builder.ForwardPhase("pin-phase", PinChangeEffectNode(&builder, "pin", global, false)))
			if err != nil {
				t.Fatal(err)
			}
			cursor := structure.Begin()
			checkpoint, err := cursor.ConsumeForwardEffect("pin/forward")
			if err != nil || checkpoint != ForwardEffectEstablishStateDir {
				t.Fatalf("forward checkpoint = %v, error = %v", checkpoint, err)
			}
			if err := cursor.SelectAlternative("pin/forward/outcome", 1); err != nil {
				t.Fatal(err)
			}
			if err := cursor.Consume("pin/forward/outcome/failure", EffectStepTerminal); err != nil {
				t.Fatal(err)
			}

			if err := cursor.Consume("pin/pin/host", EffectStepExternal); err == nil {
				t.Fatal("failed forward admission permitted a native pin attempt")
			}
			if err := cursor.FinishHandoff(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
