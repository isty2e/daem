package operationplan

import "testing"

func TestCompileNoneIsEmpty(t *testing.T) {
	t.Parallel()
	envelope := CompileNone()
	if envelope.Kind() != EnvelopeNone {
		t.Fatalf("kind = %d, want none", envelope.Kind())
	}
	if !envelope.Demand().Empty() {
		t.Fatalf("none demand = %#v, want empty", envelope.Demand())
	}
}

func TestCompileApplyNoOpIsEmpty(t *testing.T) {
	t.Parallel()
	envelope, err := CompileApply(ApplyWork{})
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Kind() != EnvelopeApply {
		t.Fatalf("kind = %d, want apply", envelope.Kind())
	}
	if !envelope.Demand().Empty() {
		t.Fatalf("no-op demand = %#v, want empty", envelope.Demand())
	}
}

func TestCompileApplyExecuteGatesReserveOneEnsure(t *testing.T) {
	t.Parallel()
	envelope, err := CompileApply(ApplyWork{ExecuteGates: 5, StatefilePath: "/state.json"})
	if err != nil {
		t.Fatal(err)
	}
	demand := envelope.Demand()
	if demand.EnsureCalls() != 1 || demand.BarrierValidationCalls() != 0 {
		t.Fatalf("ensure/barrier = %d/%d, want 1/0", demand.EnsureCalls(), demand.BarrierValidationCalls())
	}
	if demand.StateDirValidationCalls() != 4 {
		t.Fatalf("StateDir validations = %d, want 4", demand.StateDirValidationCalls())
	}
	if demand.DescendantPath() != "" || demand.DescendantValidations() != 0 {
		t.Fatalf("descendant = %q/%d, want empty", demand.DescendantPath(), demand.DescendantValidations())
	}
}

func TestCompileApplyProviderActionsReserveBarrierEnvelope(t *testing.T) {
	t.Parallel()
	envelope, err := CompileApply(ApplyWork{
		ProviderActions: []RouteWork{{}, {}},
		StatefilePath:   "/state.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	demand := envelope.Demand()
	if demand.EnsureCalls() != 1 || demand.BarrierValidationCalls() != 3 {
		t.Fatalf("ensure/barrier = %d/%d, want 1/3", demand.EnsureCalls(), demand.BarrierValidationCalls())
	}
	if demand.StateDirValidationCalls() != 1 {
		t.Fatalf("StateDir validations = %d, want 1", demand.StateDirValidationCalls())
	}
}

func TestCompileApplyGlobalCarrierSettlementReservesStateDirEffects(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		work ApplyWork
	}{
		{name: "retirement", work: ApplyWork{GlobalCarrierRetirement: true}},
		{name: "adoption", work: ApplyWork{GlobalCarrierAdoption: true}},
		{name: "retirement-and-adoption", work: ApplyWork{
			GlobalCarrierRetirement: true,
			GlobalCarrierAdoption:   true,
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			envelope, err := CompileApply(test.work)
			if err != nil {
				t.Fatal(err)
			}
			demand := envelope.Demand()
			wantValidations := 1
			if test.work.GlobalCarrierRetirement && test.work.GlobalCarrierAdoption {
				wantValidations = 3
			}
			if demand.EnsureCalls() != 1 || demand.StateDirValidationCalls() != wantValidations {
				t.Fatalf(
					"ensure/StateDir = %d/%d, want 1/%d",
					demand.EnsureCalls(),
					demand.StateDirValidationCalls(),
					wantValidations,
				)
			}
			if demand.DescendantPath() != "" || demand.DescendantBindings() != 0 ||
				demand.DescendantValidations() != 0 || demand.DescendantFileCommits() != 0 {
				t.Fatalf("global-only settlement reserved descendant work: %#v", demand)
			}
		})
	}
}

func TestCompileApplyProviderAndFinalEffectsReserveTwoEnsures(t *testing.T) {
	t.Parallel()
	envelope, err := CompileApply(ApplyWork{
		ExecuteGates:    3,
		ProviderActions: []RouteWork{{InvokesHost: true}},
		FinalRoutes:     []RouteWork{{InvokesHost: true, Global: true}},
		StatefilePath:   "/state.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	demand := envelope.Demand()
	if demand.EnsureCalls() != 2 || demand.BarrierValidationCalls() != 3 {
		t.Fatalf("ensure/barrier = %d/%d, want 2/3", demand.EnsureCalls(), demand.BarrierValidationCalls())
	}
	if demand.DescendantPath() != "/state.json" {
		t.Fatalf("descendant path = %q", demand.DescendantPath())
	}
	// provider project invoke 7/4 + final global invoke 10/4
	if demand.DescendantValidations() != 17 || demand.DescendantFileCommits() != 8 {
		t.Fatalf("statefile = %d/%d, want 17/8", demand.DescendantValidations(), demand.DescendantFileCommits())
	}
}

func TestCompileApplyPromotionIsPerActionForStatefileAndAtMostOneForStateDir(t *testing.T) {
	t.Parallel()
	envelope, err := CompileApply(ApplyWork{
		FinalRoutes: []RouteWork{
			{Promotion: true},
			{Promotion: true},
			{InvokesHost: true},
		},
		StatefilePath: "/state.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	demand := envelope.Demand()
	// StateDir host calls: 1 invocation + 1 promotion bit.
	// ExecuteGates 0 + hostCalls 2 -> ensure 1, StateDir 1.
	if demand.EnsureCalls() != 1 || demand.StateDirValidationCalls() != 1 {
		t.Fatalf("ensure/StateDir = %d/%d, want 1/1", demand.EnsureCalls(), demand.StateDirValidationCalls())
	}
	// Statefile: two promotions 4/1 each plus one project invoke 7/4.
	if demand.DescendantValidations() != 15 || demand.DescendantFileCommits() != 6 {
		t.Fatalf("statefile = %d/%d, want 15/6", demand.DescendantValidations(), demand.DescendantFileCommits())
	}
}

func TestCompileApplyRelationOrderUsesClassLevelMaximum(t *testing.T) {
	t.Parallel()
	exact := []OrderClassWork{{}}
	mutating := []OrderClassWork{{RequiresMutation: true}}

	noChange, err := CompileApply(ApplyWork{OrderClasses: exact})
	if err != nil {
		t.Fatal(err)
	}
	if !noChange.Demand().Empty() {
		t.Fatalf("exact class without reclassify reserved %#v", noChange.Demand())
	}

	withHost, err := CompileApply(ApplyWork{
		OrderClasses:  exact,
		FinalRoutes:   []RouteWork{{InvokesHost: true}},
		StatefilePath: "/state.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	demand := withHost.Demand()
	if demand.EnsureCalls() != 1 || demand.StateDirValidationCalls() != 1 {
		t.Fatalf("exact+host ensure/StateDir = %d/%d, want 1/1", demand.EnsureCalls(), demand.StateDirValidationCalls())
	}

	mutated, err := CompileApply(ApplyWork{OrderClasses: mutating})
	if err != nil {
		t.Fatal(err)
	}
	if mutated.Demand().EnsureCalls() != 1 || mutated.Demand().StateDirValidationCalls() != 0 {
		t.Fatalf("one mutating class ensure/StateDir = %d/%d, want 1/0", mutated.Demand().EnsureCalls(), mutated.Demand().StateDirValidationCalls())
	}
}

func TestCompileApplyCarrierRemovalAndDelegatePersistence(t *testing.T) {
	t.Parallel()
	envelope, err := CompileApply(ApplyWork{
		CarrierRemovals: []CarrierWork{
			{VerifiesPending: true},
			{},
			{InvokesHost: true},
		},
		Delegates: []DelegateWork{
			{SchedulesAttempt: true},
			{Blocked: true},
		},
		StatefilePath: "/state.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	demand := envelope.Demand()
	// carrierCalls 2 + persist delegate 1 = 3, ensure 1, StateDir 2
	if demand.EnsureCalls() != 1 || demand.StateDirValidationCalls() != 2 {
		t.Fatalf("ensure/StateDir = %d/%d, want 1/2", demand.EnsureCalls(), demand.StateDirValidationCalls())
	}
	// two qualifying carriers 8/3 each; delegates 2*2+3 validations, 1 commit
	if demand.DescendantValidations() != 23 || demand.DescendantFileCommits() != 7 {
		t.Fatalf("statefile = %d/%d, want 23/7", demand.DescendantValidations(), demand.DescendantFileCommits())
	}
}

func TestCompileApplyDelegateWithoutPersistenceChargesStateDirOnly(t *testing.T) {
	t.Parallel()
	envelope, err := CompileApply(ApplyWork{
		Delegates:     []DelegateWork{{}, {}},
		StatefilePath: "/state.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	demand := envelope.Demand()
	if demand.EnsureCalls() != 0 || demand.StateDirValidationCalls() != 4 {
		t.Fatalf("ensure/StateDir = %d/%d, want 0/4", demand.EnsureCalls(), demand.StateDirValidationCalls())
	}
	if demand.DescendantValidations() != 0 || demand.DescendantPath() != "" {
		t.Fatalf("unexpected descendant %#v", demand)
	}
}

func TestCompileApplyRejectsNegativeExecuteGates(t *testing.T) {
	t.Parallel()
	_, err := CompileApply(ApplyWork{ExecuteGates: -1})
	if err == nil {
		t.Fatal("expected negative execute-gates error")
	}
}

func TestCompileRefreshMatchesCurrentWriteReservation(t *testing.T) {
	t.Parallel()
	demand := CompileRefresh("/state.json").Demand()
	if demand.EnsureCalls() != 1 || demand.BarrierValidationCalls() != 4 {
		t.Fatalf("ensure/barrier = %d/%d, want 1/4", demand.EnsureCalls(), demand.BarrierValidationCalls())
	}
	if demand.StateDirValidationCalls() != 0 {
		t.Fatalf("StateDir validations = %d, want 0", demand.StateDirValidationCalls())
	}
	if demand.DescendantPath() != "/state.json" ||
		demand.DescendantValidations() != 2 ||
		demand.DescendantFileCommits() != 1 {
		t.Fatalf("descendant = %q %d/%d, want /state.json 2/1",
			demand.DescendantPath(), demand.DescendantValidations(), demand.DescendantFileCommits())
	}
}

func TestCompileApplyOverflowsClosed(t *testing.T) {
	t.Parallel()
	_, err := CompileApply(ApplyWork{
		ExecuteGates: int(^uint(0) >> 1),
		FinalRoutes:  []RouteWork{{InvokesHost: true}},
	})
	if err == nil {
		t.Fatal("expected overflow")
	}
}

func TestNewDemandReconstructsCompiledCounters(t *testing.T) {
	t.Parallel()
	demand := NewDemand(1, 4, 2, "/state.json", 2, 1)
	if demand.EnsureCalls() != 1 ||
		demand.BarrierValidationCalls() != 4 ||
		demand.StateDirValidationCalls() != 2 ||
		demand.DescendantPath() != "/state.json" ||
		demand.DescendantValidations() != 2 ||
		demand.DescendantFileCommits() != 1 {
		t.Fatalf("demand = %#v", demand)
	}
	if demand.Empty() {
		t.Fatal("non-empty demand reported empty")
	}
}
