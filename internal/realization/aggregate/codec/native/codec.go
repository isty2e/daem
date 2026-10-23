package nativecodec

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

type documentCodec struct {
	ctx            context.Context
	contractID     aggregate.CodecContractID
	syntax         configdocument.Syntax
	documentLimits configdocument.Limits
	valueLimits    configdocument.Limits
}

// New binds a pure native codec to one operation's context and independent
// document/value limits. It does not admit host paths or mutation authority.
func New(ctx context.Context, contractID aggregate.CodecContractID, documentLimits, valueLimits configdocument.Limits) (aggregate.Codec, error) {
	if ctx == nil {
		return nil, fmt.Errorf("native codec context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := documentLimits.Validate(); err != nil {
		return nil, err
	}
	if err := valueLimits.Validate(); err != nil {
		return nil, err
	}
	name, ok := aggregate.HostConfigSyntaxForCodec(contractID)
	if !ok {
		return nil, fmt.Errorf("native codec contract is unsupported")
	}
	syntax, err := configdocument.ParseSyntax(name)
	if err != nil {
		return nil, err
	}
	return documentCodec{ctx, contractID, syntax, documentLimits, valueLimits}, nil
}

func (codec documentCodec) ContractID() aggregate.CodecContractID { return codec.contractID }
func (codec documentCodec) MaximumDocumentBytes() int64           { return codec.documentLimits.MaximumBytes }

func (codec documentCodec) validContract(contract aggregate.ProjectionContract) bool {
	if contract.Validate() != nil || contract.CodecContractID() != codec.contractID {
		return false
	}
	address := contract.Address()
	document := address.Document()
	expected, err := aggregate.NewHostConfigProjectionContract(document.Target(), document.Scope(), document.AggregateRoot(), address.Footprint(), codec.contractID)
	return err == nil && expected.Equal(contract)
}

func (codec documentCodec) selection(input aggregate.Selection) (aggregate.Selection, *aggregate.CodecFailure) {
	if err := codec.ctx.Err(); err != nil {
		return aggregate.Selection{}, codec.failure(err, aggregate.CodecFailureCanonicalInvalid, output.KeyPath{})
	}
	selection, err := aggregate.NewSelection(input.Contracts())
	if err != nil || selection.CodecContractID() != codec.contractID {
		return aggregate.Selection{}, nativeFailure(aggregate.CodecFailureCanonicalInvalid, output.KeyPath{})
	}
	for _, contract := range selection.Contracts() {
		if !codec.validContract(contract) {
			return aggregate.Selection{}, nativeFailure(aggregate.CodecFailureCanonicalInvalid, contract.Address().KeyPath())
		}
	}
	return selection, nil
}

func (codec documentCodec) desiredValue(set aggregate.ContributionSet) (configdocument.Value, *aggregate.CodecFailure) {
	key := set.Address().KeyPath()
	if err := codec.ctx.Err(); err != nil {
		return configdocument.Value{}, codec.failure(err, aggregate.CodecFailureCanonicalInvalid, key)
	}
	canonical, err := aggregate.NewContributionSet(set.Contributions())
	if err != nil || !codec.validContract(canonical.Contract()) {
		return configdocument.Value{}, nativeFailure(aggregate.CodecFailureCanonicalInvalid, key)
	}
	items := canonical.Contributions()
	if len(items) != 1 || aggregate.ValidateSubjectContract(items[0].SubjectID(), canonical.Contract()) != nil {
		return configdocument.Value{}, nativeFailure(aggregate.CodecFailureCanonicalInvalid, key)
	}
	value, err := configdocument.DecodeValue(codec.ctx, codec.syntax, items[0].Contribution().CanonicalContribution(), codec.valueLimits)
	if err != nil {
		return configdocument.Value{}, codec.failure(err, aggregate.CodecFailureCanonicalInvalid, key)
	}
	if !value.IsAtomic() {
		return configdocument.Value{}, nativeFailure(aggregate.CodecFailureCanonicalInvalid, key)
	}
	return value, nil
}

func (codec documentCodec) ValidateContributions(set aggregate.ContributionSet) error {
	_, failure := codec.desiredValue(set)
	if failure != nil {
		return failure
	}
	return nil
}

func (codec documentCodec) parse(document aggregate.Document) (configdocument.Document, *aggregate.CodecFailure) {
	if err := codec.ctx.Err(); err != nil {
		return configdocument.Document{}, codec.failure(err, aggregate.CodecFailureDocumentMalformed, output.KeyPath{})
	}
	if document.Validate() != nil {
		return configdocument.Document{}, nativeFailure(aggregate.CodecFailureDocumentMalformed, output.KeyPath{})
	}
	if !document.Exists() {
		parsed, err := configdocument.Absent(codec.syntax, codec.documentLimits)
		return parsed, codec.failure(err, aggregate.CodecFailureDocumentMalformed, output.KeyPath{})
	}
	parsed, err := configdocument.Parse(codec.ctx, document.Content(), codec.syntax, codec.documentLimits)
	return parsed, codec.failure(err, aggregate.CodecFailureDocumentMalformed, output.KeyPath{})
}

func (codec documentCodec) Read(document aggregate.Document, input aggregate.Selection) (aggregate.Snapshot, *aggregate.CodecFailure) {
	selection, failure := codec.selection(input)
	if failure != nil {
		return aggregate.Snapshot{}, failure
	}
	parsed, failure := codec.parse(document)
	if failure != nil {
		return aggregate.Snapshot{}, failure
	}
	return codec.observe(parsed, selection)
}

func (codec documentCodec) observe(document configdocument.Document, selection aggregate.Selection) (aggregate.Snapshot, *aggregate.CodecFailure) {
	contracts := selection.Contracts()
	states := make([]aggregate.ProjectionState, 0, len(contracts))
	for _, contract := range contracts {
		key := contract.Address().KeyPath()
		resolution, err := document.Resolve(key)
		if err != nil {
			return aggregate.Snapshot{}, codec.failure(err, aggregate.CodecFailureSelectedShapeUnsupported, key)
		}
		var state aggregate.ProjectionState
		if ancestor, blocked := resolution.BlockedBy(); blocked {
			state, err = aggregate.NewBlockedProjectionState(contract, ancestor)
		} else {
			value, present := resolution.Value()
			canonical := ""
			if present {
				canonical, err = value.CanonicalContext(codec.ctx, codec.valueLimits)
			}
			if err == nil {
				state, err = aggregate.NewProjectionState(contract, document.Exists(), present, canonical)
			}
		}
		if err != nil {
			return aggregate.Snapshot{}, codec.failure(err, aggregate.CodecFailureSelectedShapeUnsupported, key)
		}
		states = append(states, state)
	}
	if err := codec.ctx.Err(); err != nil {
		return aggregate.Snapshot{}, codec.failure(err, aggregate.CodecFailureCanonicalInvalid, output.KeyPath{})
	}
	snapshot, err := aggregate.NewSnapshot(document.Exists(), selection, states)
	if err != nil {
		return aggregate.Snapshot{}, nativeFailure(aggregate.CodecFailureSelectedShapeUnsupported, output.KeyPath{})
	}
	return snapshot, nil
}

func (codec documentCodec) ClassifyContributionOccupancy(state aggregate.ProjectionState, set aggregate.ContributionSet) (aggregate.ContributionOccupancySet, error) {
	if err := codec.ctx.Err(); err != nil {
		return aggregate.ContributionOccupancySet{}, codec.failure(err, aggregate.CodecFailureCanonicalInvalid, output.KeyPath{})
	}
	if state.Validate() != nil || !state.Contract().Equal(set.Contract()) {
		return aggregate.ContributionOccupancySet{}, nativeFailure(aggregate.CodecFailureCanonicalInvalid, state.Contract().Address().KeyPath())
	}
	if _, failure := codec.desiredValue(set); failure != nil {
		return aggregate.ContributionOccupancySet{}, failure
	}
	occupancy := aggregate.ContributionAbsent
	if _, blocked := state.BlockedBy(); blocked {
		occupancy = aggregate.ContributionAmbiguous
	} else if state.Present() {
		if _, err := configdocument.DecodeValue(codec.ctx, codec.syntax, state.CanonicalProjection(), codec.valueLimits); err != nil {
			return aggregate.ContributionOccupancySet{}, codec.failure(err, aggregate.CodecFailureCanonicalInvalid, state.Contract().Address().KeyPath())
		}
		if state.CanonicalProjection() == set.Contributions()[0].Contribution().CanonicalContribution() {
			occupancy = aggregate.ContributionPresent
		}
	}
	if err := codec.ctx.Err(); err != nil {
		return aggregate.ContributionOccupancySet{}, codec.failure(err, aggregate.CodecFailureCanonicalInvalid, state.Contract().Address().KeyPath())
	}
	return aggregate.NewUniformContributionOccupancySet(set, occupancy)
}

func (codec documentCodec) failure(err error, fallback aggregate.CodecFailureReason, key output.KeyPath) *aggregate.CodecFailure {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		fallback = aggregate.CodecFailureOperationCanceled
	case errors.Is(err, configdocument.ErrLimitExceeded):
		fallback = aggregate.CodecFailureResourceLimitExceeded
	}
	return nativeFailure(fallback, key)
}

func nativeFailure(reason aggregate.CodecFailureReason, key output.KeyPath) *aggregate.CodecFailure {
	failure, _ := aggregate.NewCodecFailure(reason, key)
	return failure
}
