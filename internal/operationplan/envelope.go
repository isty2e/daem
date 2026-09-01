package operationplan

import "fmt"

// EnvelopeKind identifies which operation compiled the envelope.
type EnvelopeKind uint8

const (
	// EnvelopeNone is the explicit no-effect envelope.
	EnvelopeNone EnvelopeKind = iota
	// EnvelopeApply is the apply forward-effect envelope.
	EnvelopeApply
	// EnvelopeRefresh is the refresh attempt-persistence envelope.
	EnvelopeRefresh
)

// ObligationKind is the closed semantic-work taxonomy. Physical path, entry,
// byte, and census lowering remain with the State Barrier.
type ObligationKind uint8

const (
	ObligationExecuteVisibility ObligationKind = iota + 1
	ObligationProviderAction
	ObligationHostInvocation
	ObligationStateDirPromotion
	ObligationCarrierRemoval
	ObligationRelationOrderClass
	ObligationDelegateAttempt
	ObligationDelegatePersistence
	ObligationStateDirEnsure
	ObligationBarrierValidation
	ObligationStatefileValidation
	ObligationStatefileCommit
)

// Obligation is one typed, bounded unit of authorized work.
type Obligation struct {
	kind  ObligationKind
	count int
}

// Kind returns the closed obligation taxonomy member.
func (obligation Obligation) Kind() ObligationKind { return obligation.kind }

// Count returns the reserved cardinality of this obligation.
func (obligation Obligation) Count() int { return obligation.count }

// RouteWork is one already-classified host-route or relation action.
// InvokesHost takes precedence over Promotion on the same item.
type RouteWork struct {
	InvokesHost bool
	Global      bool
	Promotion   bool
}

// CarrierWork is one already-classified carrier-absence action.
type CarrierWork struct {
	InvokesHost     bool
	MutatesDirect   bool
	VerifiesPending bool
}

func (work CarrierWork) qualifies() bool {
	return work.InvokesHost || work.MutatesDirect || work.VerifiesPending
}

// OrderClassWork is one admitted relation-order class after lock matching.
type OrderClassWork struct {
	RequiresMutation bool
}

// DelegateWork is one already-classified delegate action.
type DelegateWork struct {
	SchedulesAttempt bool
	Blocked          bool
}

// ApplyWork is the I/O-free apply envelope input. ExecuteGates is Effect-owned
// visibility-gate demand; remaining slices are workflow-normalized facts.
type ApplyWork struct {
	ExecuteGates    int
	ProviderActions []RouteWork
	FinalRoutes     []RouteWork
	CarrierRemovals []CarrierWork
	OrderClasses    []OrderClassWork
	Delegates       []DelegateWork
	StatefilePath   string
}

// Demand is the multidimensional semantic reservation projection. The State
// Barrier lowers it with selected path shape and platform facts.
type Demand struct {
	ensureCalls             int
	barrierValidationCalls  int
	stateDirValidationCalls int
	descendantPath          string
	descendantBindings      int
	descendantValidations   int
	descendantFileCommits   int
}

// EnsureCalls returns reserved first-incarnation establishment count.
func (demand Demand) EnsureCalls() int { return demand.ensureCalls }

// BarrierValidationCalls returns reserved complete barrier checks.
func (demand Demand) BarrierValidationCalls() int { return demand.barrierValidationCalls }

// StateDirValidationCalls returns reserved StateDir-only identity checks.
func (demand Demand) StateDirValidationCalls() int { return demand.stateDirValidationCalls }

// DescendantPath returns the selected descendant persistence path, if any.
func (demand Demand) DescendantPath() string { return demand.descendantPath }

// DescendantBindings returns reserved descendant authority bindings.
func (demand Demand) DescendantBindings() int { return demand.descendantBindings }

// DescendantValidations returns reserved descendant identity validations.
func (demand Demand) DescendantValidations() int { return demand.descendantValidations }

// DescendantFileCommits returns reserved descendant file publications.
func (demand Demand) DescendantFileCommits() int { return demand.descendantFileCommits }

// Empty reports whether the demand authorizes no StateDir or descendant work.
func (demand Demand) Empty() bool {
	return demand.ensureCalls == 0 &&
		demand.barrierValidationCalls == 0 &&
		demand.stateDirValidationCalls == 0 &&
		demand.descendantPath == "" &&
		demand.descendantBindings == 0 &&
		demand.descendantValidations == 0 &&
		demand.descendantFileCommits == 0
}

// NewDemand reconstructs semantic demand for direct consumer test fixtures.
// Production workflows currently use CompileApply or CompileRefresh.
func NewDemand(
	ensureCalls int,
	barrierValidationCalls int,
	stateDirValidationCalls int,
	descendantPath string,
	descendantValidations int,
	descendantFileCommits int,
) Demand {
	descendantBindings := 0
	if descendantPath != "" {
		descendantBindings = 1
	}
	return Demand{
		ensureCalls:             ensureCalls,
		barrierValidationCalls:  barrierValidationCalls,
		stateDirValidationCalls: stateDirValidationCalls,
		descendantPath:          descendantPath,
		descendantBindings:      descendantBindings,
		descendantValidations:   descendantValidations,
		descendantFileCommits:   descendantFileCommits,
	}
}

// Envelope is the closed ordered obligation set and derived semantic demand
// for one operation. It grants no capability and executes no effect.
type Envelope struct {
	kind        EnvelopeKind
	obligations []Obligation
	demand      Demand
}

// Kind returns the compiled envelope class.
func (envelope Envelope) Kind() EnvelopeKind { return envelope.kind }

// Obligations returns a copy of the ordered obligation list.
func (envelope Envelope) Obligations() []Obligation {
	return append([]Obligation(nil), envelope.obligations...)
}

// Demand returns the derived semantic reservation projection.
func (envelope Envelope) Demand() Demand { return envelope.demand }

// CompileApply compiles the apply forward-effect envelope from normalized work.
func CompileApply(work ApplyWork) (Envelope, error) {
	if work.ExecuteGates < 0 {
		return Envelope{}, fmt.Errorf("operationplan: execute visibility gates must not be negative")
	}
	builder := envelopeBuilder{kind: EnvelopeApply}
	if work.ExecuteGates != 0 {
		builder.add(ObligationExecuteVisibility, work.ExecuteGates)
	}

	hostCalls, err := finalHostStateDirCalls(work.FinalRoutes)
	if err != nil {
		return Envelope{}, err
	}
	carrierCalls, err := qualifyingCarrierCount(work.CarrierRemovals)
	if err != nil {
		return Envelope{}, err
	}
	orderCalls, err := relationOrderClassCount(work.OrderClasses, relationOrderMayReclassify(work))
	if err != nil {
		return Envelope{}, err
	}
	persistDelegates := delegatePersistenceRequired(work.Delegates)

	finalEffectCalls := work.ExecuteGates
	finalEffectCalls, err = checkedAdd(finalEffectCalls, hostCalls)
	if err != nil {
		return Envelope{}, err
	}
	finalEffectCalls, err = checkedAdd(finalEffectCalls, carrierCalls)
	if err != nil {
		return Envelope{}, err
	}
	finalEffectCalls, err = checkedAdd(finalEffectCalls, orderCalls)
	if err != nil {
		return Envelope{}, err
	}

	extraStateDir := 0
	if persistDelegates {
		finalEffectCalls, err = checkedAdd(finalEffectCalls, 1)
		if err != nil {
			return Envelope{}, err
		}
		builder.add(ObligationDelegatePersistence, 1)
	} else if len(work.Delegates) != 0 {
		extraStateDir, err = checkedMul(len(work.Delegates), 2)
		if err != nil {
			return Envelope{}, err
		}
		builder.add(ObligationDelegateAttempt, extraStateDir)
	}

	providerCalls := len(work.ProviderActions)
	if providerCalls != 0 {
		builder.add(ObligationProviderAction, providerCalls)
	}
	if invocations := hostInvocations(work.FinalRoutes); invocations != 0 {
		builder.add(ObligationHostInvocation, invocations)
	}
	if carrierCalls != 0 {
		builder.add(ObligationCarrierRemoval, carrierCalls)
	}
	if orderCalls != 0 {
		builder.add(ObligationRelationOrderClass, orderCalls)
	}

	ensureCalls := 0
	if providerCalls != 0 {
		ensureCalls++
	}
	if finalEffectCalls != 0 {
		ensureCalls++
	}
	stateDirEffectCalls, err := checkedAdd(providerCalls, finalEffectCalls)
	if err != nil {
		return Envelope{}, err
	}
	stateDirEffectCalls -= ensureCalls
	stateDirOnlyCalls, err := checkedAdd(extraStateDir, stateDirEffectCalls)
	if err != nil {
		return Envelope{}, err
	}
	barrierCalls := 0
	if providerCalls != 0 {
		barrierCalls = 3
	}
	if ensureCalls != 0 {
		builder.add(ObligationStateDirEnsure, ensureCalls)
	}
	if barrierCalls != 0 {
		builder.add(ObligationBarrierValidation, barrierCalls)
	}
	if anyFinalPromotion(work.FinalRoutes) {
		builder.add(ObligationStateDirPromotion, 1)
	}

	statefile, err := applyStatefileDemand(work)
	if err != nil {
		return Envelope{}, err
	}
	if statefile.validations != 0 {
		builder.add(ObligationStatefileValidation, statefile.validations)
	}
	if statefile.commits != 0 {
		builder.add(ObligationStatefileCommit, statefile.commits)
	}

	descendantPath := ""
	descendantBindings := 0
	if !statefile.empty() {
		descendantPath = work.StatefilePath
		descendantBindings = 1
	}
	builder.demand = Demand{
		ensureCalls:             ensureCalls,
		barrierValidationCalls:  barrierCalls,
		stateDirValidationCalls: stateDirOnlyCalls,
		descendantPath:          descendantPath,
		descendantBindings:      descendantBindings,
		descendantValidations:   statefile.validations,
		descendantFileCommits:   statefile.commits,
	}
	return builder.compile(), nil
}

// CompileRefresh compiles the refresh attempt-persistence envelope.
func CompileRefresh(statefilePath string) Envelope {
	builder := envelopeBuilder{kind: EnvelopeRefresh}
	builder.add(ObligationStateDirEnsure, 1)
	builder.add(ObligationBarrierValidation, 4)
	builder.add(ObligationStatefileValidation, 2)
	builder.add(ObligationStatefileCommit, 1)
	builder.demand = Demand{
		ensureCalls:            1,
		barrierValidationCalls: 4,
		descendantPath:         statefilePath,
		descendantBindings:     1,
		descendantValidations:  2,
		descendantFileCommits:  1,
	}
	return builder.compile()
}

// CompileNone returns the explicit no-effect envelope.
func CompileNone() Envelope {
	return Envelope{kind: EnvelopeNone}
}

type envelopeBuilder struct {
	kind        EnvelopeKind
	obligations []Obligation
	demand      Demand
}

func (builder *envelopeBuilder) add(kind ObligationKind, count int) {
	if count == 0 {
		return
	}
	builder.obligations = append(builder.obligations, Obligation{kind: kind, count: count})
}

func (builder envelopeBuilder) compile() Envelope {
	return Envelope{
		kind:        builder.kind,
		obligations: append([]Obligation(nil), builder.obligations...),
		demand:      builder.demand,
	}
}

type statefileDemand struct {
	validations int
	commits     int
}

func (demand statefileDemand) empty() bool {
	return demand.validations == 0 && demand.commits == 0
}

func (demand *statefileDemand) add(validations int, commits int) error {
	var err error
	demand.validations, err = checkedAdd(demand.validations, validations)
	if err != nil {
		return err
	}
	demand.commits, err = checkedAdd(demand.commits, commits)
	return err
}

func applyStatefileDemand(work ApplyWork) (statefileDemand, error) {
	var demand statefileDemand
	for _, routes := range [][]RouteWork{work.ProviderActions, work.FinalRoutes} {
		part, err := hostRouteStatefileDemand(routes)
		if err != nil {
			return statefileDemand{}, err
		}
		if err := demand.add(part.validations, part.commits); err != nil {
			return statefileDemand{}, err
		}
	}
	carrier, err := carrierRemovalStatefileDemand(work.CarrierRemovals)
	if err != nil {
		return statefileDemand{}, err
	}
	if err := demand.add(carrier.validations, carrier.commits); err != nil {
		return statefileDemand{}, err
	}
	delegate, err := delegateStatefileDemand(work.Delegates)
	if err != nil {
		return statefileDemand{}, err
	}
	if err := demand.add(delegate.validations, delegate.commits); err != nil {
		return statefileDemand{}, err
	}
	return demand, nil
}

func hostRouteStatefileDemand(routes []RouteWork) (statefileDemand, error) {
	var demand statefileDemand
	for _, route := range routes {
		switch {
		case route.InvokesHost:
			validations := 7
			if route.Global {
				validations = 10
			}
			if err := demand.add(validations, 4); err != nil {
				return statefileDemand{}, err
			}
		case route.Promotion:
			if err := demand.add(4, 1); err != nil {
				return statefileDemand{}, err
			}
		}
	}
	return demand, nil
}

func carrierRemovalStatefileDemand(actions []CarrierWork) (statefileDemand, error) {
	var demand statefileDemand
	for _, action := range actions {
		if !action.qualifies() {
			continue
		}
		if err := demand.add(8, 3); err != nil {
			return statefileDemand{}, err
		}
	}
	return demand, nil
}

func delegateStatefileDemand(actions []DelegateWork) (statefileDemand, error) {
	if !delegatePersistenceRequired(actions) {
		return statefileDemand{}, nil
	}
	validations, err := checkedAdd(len(actions), len(actions))
	if err != nil {
		return statefileDemand{}, err
	}
	validations, err = checkedAdd(validations, 3)
	if err != nil {
		return statefileDemand{}, err
	}
	return statefileDemand{validations: validations, commits: 1}, nil
}

func finalHostStateDirCalls(routes []RouteWork) (int, error) {
	invocations := 0
	promotion := false
	for _, route := range routes {
		switch {
		case route.InvokesHost:
			var err error
			invocations, err = checkedAdd(invocations, 1)
			if err != nil {
				return 0, err
			}
		case route.Promotion:
			promotion = true
		}
	}
	if promotion {
		return checkedAdd(invocations, 1)
	}
	return invocations, nil
}

func hostInvocations(routes []RouteWork) int {
	count := 0
	for _, route := range routes {
		if route.InvokesHost {
			count++
		}
	}
	return count
}

func anyFinalPromotion(routes []RouteWork) bool {
	for _, route := range routes {
		if !route.InvokesHost && route.Promotion {
			return true
		}
	}
	return false
}

func qualifyingCarrierCount(actions []CarrierWork) (int, error) {
	count := 0
	for _, action := range actions {
		if !action.qualifies() {
			continue
		}
		var err error
		count, err = checkedAdd(count, 1)
		if err != nil {
			return 0, err
		}
	}
	return count, nil
}

func relationOrderMayReclassify(work ApplyWork) bool {
	for _, routes := range [][]RouteWork{work.ProviderActions, work.FinalRoutes} {
		for _, route := range routes {
			if route.InvokesHost {
				return true
			}
		}
	}
	for _, action := range work.CarrierRemovals {
		if action.qualifies() {
			return true
		}
	}
	return false
}

// MayReclassifyRelationOrder reports whether host, carrier, or pending-removal
// work can change relation-order class membership before execution.
func (work ApplyWork) MayReclassifyRelationOrder() bool {
	return relationOrderMayReclassify(work)
}

func relationOrderClassCount(classes []OrderClassWork, mayReclassify bool) (int, error) {
	count := 0
	for _, class := range classes {
		if !mayReclassify && !class.RequiresMutation {
			continue
		}
		var err error
		count, err = checkedAdd(count, 1)
		if err != nil {
			return 0, err
		}
	}
	return count, nil
}

func delegatePersistenceRequired(actions []DelegateWork) bool {
	for _, action := range actions {
		if action.SchedulesAttempt || action.Blocked {
			return true
		}
	}
	return false
}

func checkedAdd(left int, right int) (int, error) {
	if left < 0 || right < 0 || left > int(^uint(0)>>1)-right {
		return 0, fmt.Errorf("operationplan: effect demand overflows")
	}
	return left + right, nil
}

func checkedMul(value int, multiplier int) (int, error) {
	if value < 0 || multiplier < 0 || value != 0 && multiplier > int(^uint(0)>>1)/value {
		return 0, fmt.Errorf("operationplan: effect demand overflows")
	}
	return value * multiplier, nil
}
