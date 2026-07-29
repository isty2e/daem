package mcpcodec

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/aggregate"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

type mcpProjectionCodec struct {
	contractID aggregate.CodecContractID
}

type mcpSelectedProjection struct {
	contract aggregate.ProjectionContract
	serverID string
}

// For returns the MCP codec implementing contractID.
func For(contractID aggregate.CodecContractID) (aggregate.Codec, bool) {
	if !MCPCodecContractImplemented(contractID) {
		return nil, false
	}
	return mcpProjectionCodec{contractID: contractID}, true
}

// ValidateContribution rejects forged or non-renderable locked MCP content.
func (codec mcpProjectionCodec) ValidateContribution(contribution aggregate.ManagedContribution) error {
	if err := contribution.Validate(); err != nil {
		return err
	}
	if contribution.CodecContractID() != codec.ContractID() {
		return fmt.Errorf(
			"MCP contribution codec contract %q does not match codec %q",
			contribution.CodecContractID(),
			codec.ContractID(),
		)
	}
	operations, ok := ImplementedMCPPlacementOperationsForPlacement(
		aggregate.MCPPlacementID(contribution.Address().PlacementID()),
	)
	if !ok {
		return fmt.Errorf(
			"MCP contribution placement %q is not implemented",
			contribution.Address().PlacementID(),
		)
	}
	if operations.Placement().CodecContractID() != contribution.CodecContractID() {
		return fmt.Errorf(
			"MCP contribution placement %q does not implement codec contract %q",
			contribution.Address().PlacementID(),
			contribution.CodecContractID(),
		)
	}
	serverID, ok := operations.ServerIDFromContentPath(contribution.Address().ContentPath())
	if !ok {
		return fmt.Errorf("MCP contribution content path %q is outside its placement", contribution.ContentPath())
	}
	id, err := entity.New(entity.KindMCPServer, serverID)
	if err != nil {
		return fmt.Errorf("MCP contribution server identity: %w", err)
	}
	subject, err := topologymcp.ProjectionSubject(contribution.Target(), contribution.Scope(), id.Name())
	if err != nil {
		return err
	}
	item, err := aggregate.NewSubjectContribution(subject, contribution)
	if err != nil {
		return err
	}
	desired, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{item})
	if err != nil {
		return err
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contribution.Contract()})
	if err != nil {
		return err
	}
	beforeState, err := aggregate.NewProjectionState(contribution.Contract(), false, false, "")
	if err != nil {
		return err
	}
	before, err := aggregate.NewSnapshot(false, selection, []aggregate.ProjectionState{beforeState})
	if err != nil {
		return err
	}
	intent, err := aggregate.NewProjectionIntent(beforeState, &desired)
	if err != nil {
		return err
	}
	codecPlan, err := aggregate.NewPlan(before, []aggregate.ProjectionIntent{intent})
	if err != nil {
		return err
	}
	if _, failure := codec.Render(aggregate.AbsentDocument(), codecPlan); failure != nil {
		return failure
	}
	return nil
}

func (codec mcpProjectionCodec) ContractID() aggregate.CodecContractID {
	return codec.contractID
}

func (codec mcpProjectionCodec) Read(document aggregate.Document, selection aggregate.Selection) (aggregate.Snapshot, *aggregate.CodecFailure) {
	operations, selected, failure := codec.selectedProjections(document, selection)
	if failure != nil {
		return aggregate.Snapshot{}, failure
	}
	serverIDs := mcpSelectedServerIDs(selected)
	observation, err := operations.ObserveCanonicalEntries(document.Content(), serverIDs)
	if err != nil {
		return aggregate.Snapshot{}, mcpCodecFailure(err, aggregate.CodecFailureEquivalenceUndefined, "")
	}
	snapshot, err := mcpSnapshotFromObservation(document.Exists(), selection, selected, observation)
	if err != nil {
		return aggregate.Snapshot{}, mcpCodecFailure(err, aggregate.CodecFailureCanonicalInvalid, "")
	}
	return snapshot, nil
}

func (codec mcpProjectionCodec) Render(document aggregate.Document, plan aggregate.Plan) (aggregate.RenderedDocument, *aggregate.CodecFailure) {
	selection, err := plan.Before().Selection()
	if err != nil {
		return aggregate.RenderedDocument{}, mcpCodecFailure(err, aggregate.CodecFailureCanonicalInvalid, "")
	}
	operations, selected, failure := codec.selectedProjections(document, selection)
	if failure != nil {
		return aggregate.RenderedDocument{}, failure
	}
	if document.Exists() != plan.Before().DocumentExisted() {
		return aggregate.RenderedDocument{}, mcpCodecFailure(
			fmt.Errorf("MCP plan document presence differs from candidate"),
			aggregate.CodecFailureCanonicalInvalid,
			"",
		)
	}
	mutations, failure := mcpMutationsForPlan(selected, plan)
	if failure != nil {
		return aggregate.RenderedDocument{}, failure
	}
	content, err := operations.FoldMutations(document.Content(), mutations)
	if err != nil {
		return aggregate.RenderedDocument{}, mcpCodecFailure(err, aggregate.CodecFailureCanonicalInvalid, "")
	}
	if err := operations.VerifyMutations(content, mutations); err != nil {
		return aggregate.RenderedDocument{}, mcpCodecFailure(err, aggregate.CodecFailureCanonicalInvalid, "")
	}
	candidate := aggregate.ExistingDocument(content)
	observation, err := operations.ObserveCanonicalEntries(content, mcpSelectedServerIDs(selected))
	if err != nil {
		return aggregate.RenderedDocument{}, mcpCodecFailure(err, aggregate.CodecFailureCanonicalInvalid, "")
	}
	expected, err := mcpSnapshotFromObservation(true, selection, selected, observation)
	if err != nil {
		return aggregate.RenderedDocument{}, mcpCodecFailure(err, aggregate.CodecFailureCanonicalInvalid, "")
	}
	rendered, err := aggregate.NewRenderedDocument(candidate, plan, expected)
	if err != nil {
		return aggregate.RenderedDocument{}, mcpCodecFailure(err, aggregate.CodecFailureCanonicalInvalid, "")
	}
	return rendered, nil
}

func (codec mcpProjectionCodec) Restore(document aggregate.Document, baseline aggregate.Snapshot) (aggregate.RenderedDocument, *aggregate.CodecFailure) {
	selection, err := baseline.Selection()
	if err != nil {
		return aggregate.RenderedDocument{}, mcpCodecFailure(err, aggregate.CodecFailureCanonicalInvalid, "")
	}
	operations, selected, failure := codec.selectedProjections(document, selection)
	if failure != nil {
		return aggregate.RenderedDocument{}, failure
	}
	baselineStates := baseline.States()
	parentExistedBefore := baselineStates[0].ParentPresent()
	mutations := make([]MCPProjectionMutation, 0, len(selected))
	for index, projection := range selected {
		state := baselineStates[index]
		if state.ParentPresent() != parentExistedBefore {
			return aggregate.RenderedDocument{}, mcpCodecFailure(
				fmt.Errorf("MCP recovery baseline has inconsistent parent presence"),
				aggregate.CodecFailureCanonicalInvalid,
				projection.contract.Address().ContentPath(),
			)
		}
		var mutation MCPProjectionMutation
		if state.Present() {
			mutation, err = NewMCPProjectionUpsert(
				projection.serverID,
				[]byte(state.CanonicalProjection()),
			)
		} else {
			mutation, err = NewMCPProjectionRemoval(projection.serverID)
		}
		if err != nil {
			return aggregate.RenderedDocument{}, mcpCodecFailure(
				err,
				aggregate.CodecFailureCanonicalInvalid,
				projection.contract.Address().ContentPath(),
			)
		}
		mutations = append(mutations, mutation)
	}
	content, keepDocument, err := operations.RestoreMutations(
		document.Content(),
		mutations,
		parentExistedBefore,
	)
	if err != nil {
		return aggregate.RenderedDocument{}, mcpCodecFailure(err, aggregate.CodecFailureCanonicalInvalid, "")
	}
	candidate := aggregate.AbsentDocument()
	if keepDocument {
		candidate = aggregate.ExistingDocument(content)
	}
	observation, err := operations.ObserveCanonicalEntries(
		candidate.Content(),
		mcpSelectedServerIDs(selected),
	)
	if err != nil {
		return aggregate.RenderedDocument{}, mcpCodecFailure(err, aggregate.CodecFailureCanonicalInvalid, "")
	}
	expected, err := mcpSnapshotFromObservation(
		candidate.Exists(),
		selection,
		selected,
		observation,
	)
	if err != nil {
		return aggregate.RenderedDocument{}, mcpCodecFailure(err, aggregate.CodecFailureCanonicalInvalid, "")
	}
	restored, err := aggregate.NewRestoredDocumentWithExpected(candidate, baseline, expected)
	if err != nil {
		return aggregate.RenderedDocument{}, mcpCodecFailure(err, aggregate.CodecFailureCanonicalInvalid, "")
	}
	return restored, nil
}

func (codec mcpProjectionCodec) selectedProjections(
	document aggregate.Document,
	selection aggregate.Selection,
) (MCPPlacementOperations, []mcpSelectedProjection, *aggregate.CodecFailure) {
	if err := document.Validate(); err != nil || selection.CodecContractID() != codec.ContractID() {
		return MCPPlacementOperations{}, nil, mcpCodecFailure(
			err,
			aggregate.CodecFailureCanonicalInvalid,
			"",
		)
	}
	contracts := selection.Contracts()
	if len(contracts) == 0 {
		return MCPPlacementOperations{}, nil, mcpCodecFailure(
			fmt.Errorf("MCP selection is empty"),
			aggregate.CodecFailureCanonicalInvalid,
			"",
		)
	}
	placementID := aggregate.MCPPlacementID(contracts[0].Address().PlacementID())
	operations, ok := ImplementedMCPPlacementOperationsForPlacement(placementID)
	if !ok || operations.Placement().CodecContractID() != codec.ContractID() {
		return MCPPlacementOperations{}, nil, mcpCodecFailure(
			fmt.Errorf("MCP selection placement does not implement codec contract"),
			aggregate.CodecFailureSelectedShapeUnsupported,
			contracts[0].Address().ContentPath(),
		)
	}
	placement := operations.Placement()
	selected := make([]mcpSelectedProjection, 0, len(contracts))
	for _, contract := range contracts {
		address := contract.Address()
		serverID, ok := operations.ServerIDFromContentPath(address.ContentPath())
		if !ok {
			return MCPPlacementOperations{}, nil, mcpCodecFailure(
				fmt.Errorf("MCP content path is outside its placement"),
				aggregate.CodecFailureSelectedShapeUnsupported,
				address.ContentPath(),
			)
		}
		if address.Document().Target() != placement.Target() ||
			address.Document().Scope() != placement.Scope() ||
			address.Document().AggregateRoot() != placement.ConfigPath() ||
			address.PlacementID() != string(placement.ID()) ||
			address.MergeUnit() != aggregate.MergeUnit(placement.MergeUnit()) ||
			contract.Cardinality() != aggregate.ContributionExclusive ||
			contract.SiblingRetention() != aggregate.SiblingRetention(placement.SiblingRetention()) ||
			contract.SiblingPreservation() != aggregate.PreserveSiblingsSemantic ||
			contract.Equivalence() != aggregate.EquivalenceCanonicalSemantic ||
			!slices.Equal(contract.ComparedFields(), placement.ComparedFields()) {
			return MCPPlacementOperations{}, nil, mcpCodecFailure(
				fmt.Errorf("MCP projection contract differs from its placement"),
				aggregate.CodecFailurePreservationUndefined,
				address.ContentPath(),
			)
		}
		selected = append(selected, mcpSelectedProjection{contract: contract, serverID: serverID})
	}
	return operations, selected, nil
}

func mcpMutationsForPlan(
	selected []mcpSelectedProjection,
	plan aggregate.Plan,
) ([]MCPProjectionMutation, *aggregate.CodecFailure) {
	intents := plan.Intents()
	if len(intents) != len(selected) {
		return nil, mcpCodecFailure(
			fmt.Errorf("MCP plan coverage differs from its selection"),
			aggregate.CodecFailureCanonicalInvalid,
			"",
		)
	}
	mutations := make([]MCPProjectionMutation, 0, len(intents))
	for index, intent := range intents {
		projection := selected[index]
		before := intent.Before()
		if !before.Contract().Equal(projection.contract) {
			return nil, mcpCodecFailure(
				fmt.Errorf("MCP intent contract differs from selection"),
				aggregate.CodecFailureCanonicalInvalid,
				projection.contract.Address().ContentPath(),
			)
		}
		desired, desiredPresent := intent.Desired()
		var (
			mutation MCPProjectionMutation
			err      error
		)
		switch {
		case !desiredPresent:
			mutation, err = NewMCPProjectionRemoval(projection.serverID)
		case len(desired.Contributions()) != 1:
			err = fmt.Errorf("exclusive MCP projection requires exactly one contribution")
		default:
			item := desired.Contributions()[0]
			if !item.Contribution().Contract().Equal(projection.contract) {
				err = fmt.Errorf("MCP desired contribution contract differs from selection")
				break
			}
			canonical := []byte(item.Contribution().CanonicalContribution())
			if before.Present() {
				mutation, err = NewMCPProjectionUpsert(projection.serverID, canonical)
			} else {
				mutation, err = NewMCPProjectionInsert(projection.serverID, canonical)
			}
		}
		if err != nil {
			return nil, mcpCodecFailure(
				err,
				aggregate.CodecFailureCanonicalInvalid,
				projection.contract.Address().ContentPath(),
			)
		}
		mutations = append(mutations, mutation)
	}
	return mutations, nil
}

func mcpSnapshotFromObservation(
	documentExisted bool,
	selection aggregate.Selection,
	selected []mcpSelectedProjection,
	observation MCPProjectionObservation,
) (aggregate.Snapshot, error) {
	states := make([]aggregate.ProjectionState, 0, len(selected))
	for _, projection := range selected {
		canonical, present, err := observation.CanonicalEntry(projection.serverID)
		if err != nil {
			return aggregate.Snapshot{}, err
		}
		state, err := aggregate.NewProjectionState(
			projection.contract,
			observation.ParentPresent(),
			present,
			string(canonical),
		)
		if err != nil {
			return aggregate.Snapshot{}, err
		}
		states = append(states, state)
	}
	return aggregate.NewSnapshot(documentExisted, selection, states)
}

func mcpSelectedServerIDs(selected []mcpSelectedProjection) []string {
	result := make([]string, len(selected))
	for index, projection := range selected {
		result[index] = projection.serverID
	}
	return result
}

func mcpCodecFailure(
	err error,
	fallback aggregate.CodecFailureReason,
	contentPath aggregate.ContentPath,
) *aggregate.CodecFailure {
	reason := fallback
	if code, ok := MCPProjectionReasonCodeOf(err); ok {
		switch code {
		case MCPProjectionReasonConfigMalformed:
			reason = aggregate.CodecFailureDocumentMalformed
		case MCPProjectionReasonUnsupportedTransport:
			reason = aggregate.CodecFailureUnsupportedTransport
		case MCPProjectionReasonUnsupportedManagedField:
			reason = aggregate.CodecFailureUnsupportedManagedField
		case MCPProjectionReasonSecretLiteralForbidden:
			reason = aggregate.CodecFailureSecretLiteralForbidden
		case MCPProjectionReasonProjectionEquivalenceUndefined:
			reason = aggregate.CodecFailureEquivalenceUndefined
		case MCPProjectionReasonStaleAdapterContract:
			reason = aggregate.CodecFailureCanonicalInvalid
		}
	}
	failure, failureErr := aggregate.NewCodecFailure(reason, contentPath)
	if failureErr != nil {
		panic(failureErr)
	}
	return failure
}
