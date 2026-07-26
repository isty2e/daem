package aggregate

import (
	"fmt"
	"sort"
	"unicode/utf8"
)

// Selection is a canonical set of projections read from one document by one codec.
type Selection struct {
	contracts []ProjectionContract
}

// NewSelection validates and sorts one codec read selection.
func NewSelection(values []ProjectionContract) (Selection, error) {
	if len(values) == 0 {
		return Selection{}, fmt.Errorf("aggregate codec selection is required")
	}
	contracts := make([]ProjectionContract, len(values))
	for index, contract := range values {
		if err := contract.Validate(); err != nil {
			return Selection{}, fmt.Errorf("aggregate codec selection[%d]: %w", index, err)
		}
		contracts[index] = cloneProjectionContract(contract)
	}
	sort.Slice(contracts, func(left int, right int) bool {
		return compareProjectionAddress(contracts[left].address, contracts[right].address) < 0
	})
	first := contracts[0]
	physicalAddresses := make(map[struct {
		document    DocumentAddress
		contentPath ContentPath
	}]ProjectionAddress, len(contracts))
	for index, contract := range contracts {
		if contract.address.document != first.address.document {
			return Selection{}, fmt.Errorf("aggregate codec selection mixes documents")
		}
		if contract.codecContractID != first.codecContractID {
			return Selection{}, fmt.Errorf("aggregate codec selection mixes codec contracts")
		}
		if index > 0 && contract.address == contracts[index-1].address {
			return Selection{}, fmt.Errorf("aggregate codec selection repeats projection address")
		}
		physical := struct {
			document    DocumentAddress
			contentPath ContentPath
		}{document: contract.address.document, contentPath: contract.address.contentPath}
		if existing, duplicate := physicalAddresses[physical]; duplicate {
			return Selection{}, fmt.Errorf(
				"aggregate codec selection has physical projection collision between placements %q and %q",
				existing.placementID,
				contract.address.placementID,
			)
		}
		physicalAddresses[physical] = contract.address
	}
	return Selection{contracts: contracts}, nil
}

func (selection Selection) DocumentAddress() DocumentAddress {
	if len(selection.contracts) == 0 {
		return DocumentAddress{}
	}
	return selection.contracts[0].address.document
}

func (selection Selection) CodecContractID() CodecContractID {
	if len(selection.contracts) == 0 {
		return ""
	}
	return selection.contracts[0].codecContractID
}

func (selection Selection) Contracts() []ProjectionContract {
	result := make([]ProjectionContract, len(selection.contracts))
	for index, contract := range selection.contracts {
		result[index] = cloneProjectionContract(contract)
	}
	return result
}

// ProjectionState is one selected canonical projection observation.
type ProjectionState struct {
	contract            ProjectionContract
	parentPresent       bool
	present             bool
	canonicalProjection string
}

// NewProjectionState constructs one selected codec observation.
func NewProjectionState(
	contract ProjectionContract,
	parentPresent bool,
	present bool,
	canonicalProjection string,
) (ProjectionState, error) {
	state := ProjectionState{
		contract:            cloneProjectionContract(contract),
		parentPresent:       parentPresent,
		present:             present,
		canonicalProjection: canonicalProjection,
	}
	if err := state.Validate(); err != nil {
		return ProjectionState{}, err
	}
	return state, nil
}

// Validate rejects contradictory selected projection evidence.
func (state ProjectionState) Validate() error {
	if err := state.contract.Validate(); err != nil {
		return err
	}
	if !state.present {
		if state.canonicalProjection != "" {
			return fmt.Errorf("absent aggregate projection must not carry canonical content")
		}
		return nil
	}
	if !state.parentPresent {
		return fmt.Errorf("present aggregate projection requires its parent")
	}
	if state.canonicalProjection == "" || !utf8.ValidString(state.canonicalProjection) {
		return fmt.Errorf("present aggregate projection requires canonical UTF-8 content")
	}
	return nil
}

func (state ProjectionState) Contract() ProjectionContract {
	return cloneProjectionContract(state.contract)
}
func (state ProjectionState) ParentPresent() bool         { return state.parentPresent }
func (state ProjectionState) Present() bool               { return state.present }
func (state ProjectionState) CanonicalProjection() string { return state.canonicalProjection }

// Snapshot is fresh selected projection evidence for one document and codec.
type Snapshot struct {
	documentExisted bool
	states          []ProjectionState
}

// NewSnapshot validates exact selection coverage and canonical ordering.
func NewSnapshot(documentExisted bool, selection Selection, states []ProjectionState) (Snapshot, error) {
	if len(selection.contracts) == 0 {
		return Snapshot{}, fmt.Errorf("aggregate snapshot selection is required")
	}
	if len(states) != len(selection.contracts) {
		return Snapshot{}, fmt.Errorf("aggregate snapshot state count = %d, want %d", len(states), len(selection.contracts))
	}
	byAddress := make(map[ProjectionAddress]ProjectionState, len(states))
	for index, state := range states {
		if err := state.Validate(); err != nil {
			return Snapshot{}, fmt.Errorf("aggregate snapshot state[%d]: %w", index, err)
		}
		if state.contract.address.document != selection.DocumentAddress() ||
			state.contract.codecContractID != selection.CodecContractID() {
			return Snapshot{}, fmt.Errorf("aggregate snapshot state[%d] is outside the selection", index)
		}
		if _, duplicate := byAddress[state.contract.address]; duplicate {
			return Snapshot{}, fmt.Errorf("aggregate snapshot repeats projection address")
		}
		byAddress[state.contract.address] = state
	}
	canonical := make([]ProjectionState, 0, len(selection.contracts))
	for _, contract := range selection.contracts {
		state, present := byAddress[contract.address]
		if !present || !state.contract.Equal(contract) {
			return Snapshot{}, fmt.Errorf("aggregate snapshot does not exactly cover its selection")
		}
		if !documentExisted && (state.parentPresent || state.present) {
			return Snapshot{}, fmt.Errorf("absent aggregate document cannot contain selected projections")
		}
		canonical = append(canonical, cloneProjectionState(state))
	}
	return Snapshot{documentExisted: documentExisted, states: canonical}, nil
}

func (snapshot Snapshot) DocumentExisted() bool { return snapshot.documentExisted }
func (snapshot Snapshot) States() []ProjectionState {
	result := make([]ProjectionState, len(snapshot.states))
	for index, state := range snapshot.states {
		result[index] = cloneProjectionState(state)
	}
	return result
}

// State returns the selected state carrying the exact projection contract.
func (snapshot Snapshot) State(contract ProjectionContract) (ProjectionState, bool) {
	for _, state := range snapshot.states {
		if state.contract.Equal(contract) {
			return cloneProjectionState(state), true
		}
	}
	return ProjectionState{}, false
}

func (snapshot Snapshot) Selection() (Selection, error) {
	contracts := make([]ProjectionContract, 0, len(snapshot.states))
	for _, state := range snapshot.states {
		contracts = append(contracts, state.contract)
	}
	return NewSelection(contracts)
}

// Equal compares document presence, selection contracts, and canonical selected state.
func (snapshot Snapshot) Equal(other Snapshot) bool {
	if snapshot.documentExisted != other.documentExisted || len(snapshot.states) != len(other.states) {
		return false
	}
	for index, state := range snapshot.states {
		if !projectionStatesEqual(state, other.states[index]) {
			return false
		}
	}
	return len(snapshot.states) != 0
}

// ProjectionIntent pairs fresh before evidence with desired contributions.
// A nil desired set means remove the selected projection.
type ProjectionIntent struct {
	before  ProjectionState
	desired *ContributionSet
}

// NewProjectionIntent constructs one address-correlated before/after intent.
func NewProjectionIntent(before ProjectionState, desired *ContributionSet) (ProjectionIntent, error) {
	if err := before.Validate(); err != nil {
		return ProjectionIntent{}, err
	}
	if desired == nil {
		if !before.present {
			return ProjectionIntent{}, fmt.Errorf("aggregate remove intent requires a present before projection")
		}
		return ProjectionIntent{before: cloneProjectionState(before)}, nil
	}
	canonicalDesired, err := NewContributionSet(desired.items)
	if err != nil {
		return ProjectionIntent{}, fmt.Errorf("aggregate desired contribution set: %w", err)
	}
	if !before.contract.Equal(canonicalDesired.Contract()) {
		return ProjectionIntent{}, fmt.Errorf("aggregate intent before and desired contracts differ")
	}
	copy := canonicalDesired
	return ProjectionIntent{before: cloneProjectionState(before), desired: &copy}, nil
}

func (intent ProjectionIntent) Before() ProjectionState { return cloneProjectionState(intent.before) }

func (intent ProjectionIntent) Desired() (ContributionSet, bool) {
	if intent.desired == nil {
		return ContributionSet{}, false
	}
	return ContributionSet{items: intent.desired.Contributions()}, true
}

// Plan is one canonical batch over a single document and codec contract.
type Plan struct {
	before  Snapshot
	intents []ProjectionIntent
}

// NewPlan validates exact before-state coverage and canonicalizes intent order.
func NewPlan(before Snapshot, intents []ProjectionIntent) (Plan, error) {
	selection, err := before.Selection()
	if err != nil {
		return Plan{}, err
	}
	if len(intents) == 0 || len(intents) != len(before.states) {
		return Plan{}, fmt.Errorf("aggregate plan must cover every selected projection exactly once")
	}
	byAddress := make(map[ProjectionAddress]ProjectionIntent, len(intents))
	for index, intent := range intents {
		canonicalIntent, err := NewProjectionIntent(intent.before, intent.desired)
		if err != nil {
			return Plan{}, fmt.Errorf("aggregate plan intent[%d]: %w", index, err)
		}
		address := canonicalIntent.before.contract.address
		if _, duplicate := byAddress[address]; duplicate {
			return Plan{}, fmt.Errorf("aggregate plan repeats projection address")
		}
		byAddress[address] = canonicalIntent
	}
	canonical := make([]ProjectionIntent, 0, len(selection.contracts))
	for index, contract := range selection.contracts {
		intent, present := byAddress[contract.address]
		if !present || !intent.before.contract.Equal(contract) ||
			!projectionStatesEqual(intent.before, before.states[index]) {
			return Plan{}, fmt.Errorf("aggregate plan before evidence differs from its snapshot")
		}
		canonical = append(canonical, cloneProjectionIntent(intent))
	}
	return Plan{before: cloneSnapshot(before), intents: canonical}, nil
}

func (plan Plan) Before() Snapshot { return cloneSnapshot(plan.before) }
func (plan Plan) Intents() []ProjectionIntent {
	result := make([]ProjectionIntent, len(plan.intents))
	for index, intent := range plan.intents {
		result[index] = cloneProjectionIntent(intent)
	}
	return result
}

// RenderedDocument is a pure candidate plus its selected expected projection state.
type RenderedDocument struct {
	document Document
	expected Snapshot
}

// NewRenderedDocument constructs a plan-correlated, non-authoritative codec candidate.
func NewRenderedDocument(document Document, plan Plan, expected Snapshot) (RenderedDocument, error) {
	if err := document.Validate(); err != nil {
		return RenderedDocument{}, err
	}
	canonicalPlan, err := NewPlan(plan.before, plan.intents)
	if err != nil {
		return RenderedDocument{}, fmt.Errorf("rendered document plan: %w", err)
	}
	if document.exists != expected.documentExisted {
		return RenderedDocument{}, fmt.Errorf("rendered document presence differs from expected snapshot")
	}
	if err := validateExpectedProjectionStates(canonicalPlan, expected); err != nil {
		return RenderedDocument{}, err
	}
	return RenderedDocument{document: cloneDocument(document), expected: cloneSnapshot(expected)}, nil
}

// NewRestoredDocument constructs a non-authoritative candidate for one exact recovery baseline.
func NewRestoredDocument(document Document, baseline Snapshot) (RenderedDocument, error) {
	if err := document.Validate(); err != nil {
		return RenderedDocument{}, err
	}
	selection, err := baseline.Selection()
	if err != nil {
		return RenderedDocument{}, fmt.Errorf("restored document baseline: %w", err)
	}
	canonicalBaseline, err := NewSnapshot(baseline.documentExisted, selection, baseline.states)
	if err != nil {
		return RenderedDocument{}, fmt.Errorf("restored document baseline: %w", err)
	}
	states := canonicalBaseline.States()
	for index, state := range states {
		states[index], err = NewProjectionState(
			state.contract,
			document.exists,
			state.present,
			state.canonicalProjection,
		)
		if err != nil {
			return RenderedDocument{}, fmt.Errorf("restored document expected state[%d]: %w", index, err)
		}
	}
	expected, err := NewSnapshot(document.exists, selection, states)
	if err != nil {
		return RenderedDocument{}, fmt.Errorf("restored document expected snapshot: %w", err)
	}
	return RenderedDocument{document: cloneDocument(document), expected: expected}, nil
}

// NewRestoredDocumentWithExpected constructs a recovery candidate whose
// selected values match baseline while parent/document presence reflects the
// current document after preserving concurrent unmanaged siblings.
func NewRestoredDocumentWithExpected(
	document Document,
	baseline Snapshot,
	expected Snapshot,
) (RenderedDocument, error) {
	if err := document.Validate(); err != nil {
		return RenderedDocument{}, err
	}
	baselineSelection, err := baseline.Selection()
	if err != nil {
		return RenderedDocument{}, fmt.Errorf("restored document baseline: %w", err)
	}
	expectedSelection, err := expected.Selection()
	if err != nil {
		return RenderedDocument{}, fmt.Errorf("restored document expected snapshot: %w", err)
	}
	if !selectionsEqual(baselineSelection, expectedSelection) {
		return RenderedDocument{}, fmt.Errorf("restored document expected selection differs from baseline")
	}
	if document.exists != expected.documentExisted {
		return RenderedDocument{}, fmt.Errorf("restored document presence differs from expected snapshot")
	}
	baselineStates := baseline.States()
	expectedStates := expected.States()
	for index := range baselineStates {
		if !baselineStates[index].Contract().Equal(expectedStates[index].Contract()) ||
			baselineStates[index].Present() != expectedStates[index].Present() ||
			baselineStates[index].CanonicalProjection() != expectedStates[index].CanonicalProjection() {
			return RenderedDocument{}, fmt.Errorf(
				"restored document expected projection[%d] differs from baseline",
				index,
			)
		}
	}
	return RenderedDocument{document: cloneDocument(document), expected: cloneSnapshot(expected)}, nil
}

func (rendered RenderedDocument) Document() Document { return cloneDocument(rendered.document) }
func (rendered RenderedDocument) Expected() Snapshot { return cloneSnapshot(rendered.expected) }

func cloneSnapshot(snapshot Snapshot) Snapshot {
	result := Snapshot{documentExisted: snapshot.documentExisted, states: make([]ProjectionState, len(snapshot.states))}
	for index, state := range snapshot.states {
		result.states[index] = cloneProjectionState(state)
	}
	return result
}

func cloneProjectionIntent(intent ProjectionIntent) ProjectionIntent {
	result := ProjectionIntent{before: cloneProjectionState(intent.before)}
	if intent.desired != nil {
		copy := ContributionSet{items: intent.desired.Contributions()}
		result.desired = &copy
	}
	return result
}

func projectionStatesEqual(left ProjectionState, right ProjectionState) bool {
	return left.contract.Equal(right.contract) &&
		left.parentPresent == right.parentPresent &&
		left.present == right.present &&
		left.canonicalProjection == right.canonicalProjection
}

func selectionsEqual(left Selection, right Selection) bool {
	if len(left.contracts) != len(right.contracts) {
		return false
	}
	for index := range left.contracts {
		if !left.contracts[index].Equal(right.contracts[index]) {
			return false
		}
	}
	return true
}

func validateExpectedProjectionStates(plan Plan, expected Snapshot) error {
	selection, err := expected.Selection()
	if err != nil {
		return fmt.Errorf("rendered document expected snapshot: %w", err)
	}
	canonicalExpected, err := NewSnapshot(expected.documentExisted, selection, expected.states)
	if err != nil {
		return fmt.Errorf("rendered document expected snapshot: %w", err)
	}
	expected = canonicalExpected
	if len(expected.states) != len(plan.intents) {
		return fmt.Errorf("rendered document expected snapshot does not cover the plan")
	}
	byAddress := make(map[ProjectionAddress]ProjectionState, len(expected.states))
	for _, state := range expected.states {
		if err := state.Validate(); err != nil {
			return fmt.Errorf("rendered document expected snapshot: %w", err)
		}
		if _, duplicate := byAddress[state.contract.address]; duplicate {
			return fmt.Errorf("rendered document expected snapshot repeats projection address")
		}
		byAddress[state.contract.address] = state
	}
	for _, intent := range plan.intents {
		state, present := byAddress[intent.before.contract.address]
		if !present || !state.contract.Equal(intent.before.contract) {
			return fmt.Errorf("rendered document expected snapshot differs from the plan contract")
		}
		if intent.desired == nil && state.present {
			return fmt.Errorf("rendered document retains a projection planned for removal")
		}
		if intent.desired != nil && !state.present {
			return fmt.Errorf("rendered document omits a desired projection")
		}
	}
	return nil
}
