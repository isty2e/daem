package nativecodec

import (
	"context"
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
	syntax, err := nativeSyntax(contractID)
	if err != nil {
		return nil, err
	}
	return documentCodec{ctx, contractID, syntax, documentLimits, valueLimits}, nil
}

func (codec documentCodec) ContractID() aggregate.CodecContractID { return codec.contractID }
func (codec documentCodec) MaximumDocumentBytes() int64           { return codec.documentLimits.MaximumBytes }

func (codec documentCodec) RewriteLayout(input aggregate.Selection) (string, []output.KeyPath, error) {
	selection, failure := codec.selection(input)
	if failure != nil {
		return "", nil, failure
	}
	areas := make([]output.KeyPath, 0, len(selection.Contracts()))
	for _, contract := range selection.Contracts() {
		areas = append(areas, contract.Address().KeyPath())
	}
	return codec.syntax.String(), output.TopmostKeyPaths(areas), nil
}

func (codec documentCodec) selection(input aggregate.Selection) (aggregate.Selection, *aggregate.CodecFailure) {
	if err := codec.ctx.Err(); err != nil {
		return aggregate.Selection{}, nativeCodecFailure(err, aggregate.CodecFailureCanonicalInvalid, output.KeyPath{})
	}
	selection, err := aggregate.NewSelection(input.Contracts())
	if err != nil || selection.CodecContractID() != codec.contractID {
		return aggregate.Selection{}, nativeFailure(aggregate.CodecFailureCanonicalInvalid, output.KeyPath{})
	}
	for _, contract := range selection.Contracts() {
		if !validNativeContract(codec.contractID, contract) {
			return aggregate.Selection{}, nativeFailure(aggregate.CodecFailureCanonicalInvalid, contract.Address().KeyPath())
		}
	}
	return selection, nil
}

func (codec documentCodec) ValidateContributions(set aggregate.ContributionSet) error {
	return ValidateContributions(codec.ctx, codec.contractID, set, codec.valueLimits)
}

func (codec documentCodec) parse(document aggregate.Document) (configdocument.Document, *aggregate.CodecFailure) {
	if err := codec.ctx.Err(); err != nil {
		return configdocument.Document{}, nativeCodecFailure(err, aggregate.CodecFailureDocumentMalformed, output.KeyPath{})
	}
	if document.Validate() != nil {
		return configdocument.Document{}, nativeFailure(aggregate.CodecFailureDocumentMalformed, output.KeyPath{})
	}
	if !document.Exists() {
		parsed, err := configdocument.Absent(codec.syntax, codec.documentLimits)
		return parsed, nativeCodecFailure(err, aggregate.CodecFailureDocumentMalformed, output.KeyPath{})
	}
	if document.ContentSize() > codec.documentLimits.MaximumBytes {
		return configdocument.Document{}, nativeCodecFailure(configdocument.ErrLimitExceeded, aggregate.CodecFailureDocumentMalformed, output.KeyPath{})
	}
	parsed, err := configdocument.Parse(codec.ctx, document.Content(), codec.syntax, codec.documentLimits)
	return parsed, nativeCodecFailure(err, aggregate.CodecFailureDocumentMalformed, output.KeyPath{})
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
			return aggregate.Snapshot{}, nativeCodecFailure(err, aggregate.CodecFailureSelectedShapeUnsupported, key)
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
			return aggregate.Snapshot{}, nativeCodecFailure(err, aggregate.CodecFailureSelectedShapeUnsupported, key)
		}
		states = append(states, state)
	}
	if err := codec.ctx.Err(); err != nil {
		return aggregate.Snapshot{}, nativeCodecFailure(err, aggregate.CodecFailureCanonicalInvalid, output.KeyPath{})
	}
	snapshot, err := aggregate.NewSnapshot(document.Exists(), selection, states)
	if err != nil {
		return aggregate.Snapshot{}, nativeFailure(aggregate.CodecFailureSelectedShapeUnsupported, output.KeyPath{})
	}
	return snapshot, nil
}

func (codec documentCodec) ClassifyContributionOccupancy(state aggregate.ProjectionState, set aggregate.ContributionSet) (aggregate.ContributionOccupancySet, error) {
	if err := codec.ctx.Err(); err != nil {
		return aggregate.ContributionOccupancySet{}, nativeCodecFailure(err, aggregate.CodecFailureCanonicalInvalid, output.KeyPath{})
	}
	if state.Validate() != nil || !state.Contract().Equal(set.Contract()) {
		return aggregate.ContributionOccupancySet{}, nativeFailure(aggregate.CodecFailureCanonicalInvalid, state.Contract().Address().KeyPath())
	}
	if err := codec.ValidateContributions(set); err != nil {
		return aggregate.ContributionOccupancySet{}, err
	}
	occupancy := aggregate.ContributionAbsent
	if _, blocked := state.BlockedBy(); blocked {
		occupancy = aggregate.ContributionAmbiguous
	} else if state.Present() {
		if _, err := configdocument.DecodeValue(codec.ctx, codec.syntax, state.CanonicalProjection(), codec.valueLimits); err != nil {
			return aggregate.ContributionOccupancySet{}, nativeCodecFailure(err, aggregate.CodecFailureCanonicalInvalid, state.Contract().Address().KeyPath())
		}
		if state.CanonicalProjection() == set.Contributions()[0].Contribution().CanonicalContribution() {
			occupancy = aggregate.ContributionPresent
		}
	}
	if err := codec.ctx.Err(); err != nil {
		return aggregate.ContributionOccupancySet{}, nativeCodecFailure(err, aggregate.CodecFailureCanonicalInvalid, state.Contract().Address().KeyPath())
	}
	return aggregate.NewUniformContributionOccupancySet(set, occupancy)
}
