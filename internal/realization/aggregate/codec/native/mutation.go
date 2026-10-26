package nativecodec

import (
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func (codec documentCodec) Render(document aggregate.Document, input aggregate.Plan) (aggregate.RenderedDocument, *aggregate.CodecFailure) {
	if err := codec.ctx.Err(); err != nil {
		return aggregate.RenderedDocument{}, nativeCodecFailure(err, aggregate.CodecFailureCanonicalInvalid, output.KeyPath{})
	}
	plan, err := aggregate.NewPlan(input.Before(), input.Intents())
	if err != nil {
		return aggregate.RenderedDocument{}, nativeFailure(aggregate.CodecFailureCanonicalInvalid, output.KeyPath{})
	}
	selection, err := plan.Before().Selection()
	if err != nil {
		return aggregate.RenderedDocument{}, nativeFailure(aggregate.CodecFailureCanonicalInvalid, output.KeyPath{})
	}
	selection, failure := codec.selection(selection)
	if failure != nil {
		return aggregate.RenderedDocument{}, failure
	}
	parsed, failure := codec.parse(document)
	if failure != nil {
		return aggregate.RenderedDocument{}, failure
	}
	current, failure := codec.observe(parsed, selection)
	if failure != nil {
		return aggregate.RenderedDocument{}, failure
	}
	if !current.Equal(plan.Before()) {
		return aggregate.RenderedDocument{}, nativeFailure(aggregate.CodecFailureEquivalenceUndefined, output.KeyPath{})
	}

	var removals []selectedChange
	var desired []configdocument.Change
	for _, intent := range plan.Intents() {
		contract := intent.Before().Contract()
		key := contract.Address().KeyPath()
		if set, present := intent.Desired(); present {
			value, failure := contributionValue(codec.ctx, codec.contractID, codec.syntax, codec.valueLimits, set)
			if failure != nil {
				return aggregate.RenderedDocument{}, failure
			}
			change, err := configdocument.Set(key, value)
			if err != nil {
				return aggregate.RenderedDocument{}, nativeFailure(aggregate.CodecFailureCanonicalInvalid, key)
			}
			desired = append(desired, change)
			continue
		}
		value, present, err := parsed.Lookup(key)
		if err != nil {
			return aggregate.RenderedDocument{}, nativeCodecFailure(err, aggregate.CodecFailureSelectedShapeUnsupported, key)
		}
		if !present {
			continue
		}
		if plan.RetainsStructuralContainer(contract.Address()) && value.IsObject() {
			continue
		}
		change, err := configdocument.Remove(key)
		if err != nil {
			return aggregate.RenderedDocument{}, nativeFailure(aggregate.CodecFailureCanonicalInvalid, key)
		}
		removals = append(removals, selectedChange{key, change})
	}
	parsed, failure = codec.edit(parsed, topmostChanges(removals))
	if failure == nil {
		parsed, failure = codec.edit(parsed, desired)
	}
	if failure != nil {
		return aggregate.RenderedDocument{}, failure
	}
	expected, failure := codec.observe(parsed, selection)
	if failure != nil {
		return aggregate.RenderedDocument{}, failure
	}
	rendered, err := aggregate.NewRenderedDocument(nativeDocument(parsed), plan, expected)
	if err != nil {
		return aggregate.RenderedDocument{}, nativeFailure(aggregate.CodecFailurePreservationUndefined, output.KeyPath{})
	}
	return rendered, nil
}

func (codec documentCodec) Restore(document aggregate.Document, input aggregate.Snapshot) (aggregate.RenderedDocument, *aggregate.CodecFailure) {
	if err := codec.ctx.Err(); err != nil {
		return aggregate.RenderedDocument{}, nativeCodecFailure(err, aggregate.CodecFailureCanonicalInvalid, output.KeyPath{})
	}
	selection, err := input.Selection()
	if err != nil {
		return aggregate.RenderedDocument{}, nativeFailure(aggregate.CodecFailureCanonicalInvalid, output.KeyPath{})
	}
	selection, failure := codec.selection(selection)
	if failure != nil {
		return aggregate.RenderedDocument{}, failure
	}
	baseline, err := aggregate.NewSnapshot(input.DocumentExisted(), selection, input.States())
	if err != nil {
		return aggregate.RenderedDocument{}, nativeFailure(aggregate.CodecFailureCanonicalInvalid, output.KeyPath{})
	}
	parsed, failure := codec.parse(document)
	if failure != nil {
		return aggregate.RenderedDocument{}, failure
	}
	current, failure := codec.observe(parsed, selection)
	if failure != nil {
		return aggregate.RenderedDocument{}, failure
	}

	currentStates := current.States()
	var removals, replacements []selectedChange
	for index, state := range baseline.States() {
		key := state.Contract().Address().KeyPath()
		if state.Present() {
			value, err := configdocument.DecodeValue(codec.ctx, codec.syntax, state.CanonicalProjection(), codec.valueLimits)
			if err != nil {
				return aggregate.RenderedDocument{}, nativeCodecFailure(err, aggregate.CodecFailureCanonicalInvalid, key)
			}
			change, err := configdocument.Set(key, value)
			if err != nil {
				return aggregate.RenderedDocument{}, nativeFailure(aggregate.CodecFailureCanonicalInvalid, key)
			}
			replacements = append(replacements, selectedChange{key, change})
		} else if currentStates[index].Present() {
			change, err := configdocument.Remove(key)
			if err != nil {
				return aggregate.RenderedDocument{}, nativeFailure(aggregate.CodecFailureCanonicalInvalid, key)
			}
			removals = append(removals, selectedChange{key, change})
		}
	}
	parsed, failure = codec.edit(parsed, topmostChanges(removals))
	if failure == nil {
		parsed, failure = codec.edit(parsed, topmostChanges(replacements))
	}
	if failure != nil {
		return aggregate.RenderedDocument{}, failure
	}
	if !baseline.DocumentExisted() {
		root, _, err := parsed.Lookup(output.KeyPath{})
		if err != nil {
			return aggregate.RenderedDocument{}, nativeCodecFailure(err, aggregate.CodecFailurePreservationUndefined, output.KeyPath{})
		}
		if root.IsObject() && root.IsAtomic() && len(parsed.Comments()) == 0 {
			parsed, err = configdocument.Absent(codec.syntax, codec.documentLimits)
			if err != nil {
				return aggregate.RenderedDocument{}, nativeCodecFailure(err, aggregate.CodecFailurePreservationUndefined, output.KeyPath{})
			}
		}
	}
	expected, failure := codec.observe(parsed, selection)
	if failure != nil {
		return aggregate.RenderedDocument{}, failure
	}
	rendered, err := aggregate.NewRestoredDocumentWithExpected(nativeDocument(parsed), baseline, expected)
	if err != nil {
		return aggregate.RenderedDocument{}, nativeFailure(aggregate.CodecFailurePreservationUndefined, output.KeyPath{})
	}
	return rendered, nil
}

func (codec documentCodec) edit(document configdocument.Document, changes []configdocument.Change) (configdocument.Document, *aggregate.CodecFailure) {
	if err := codec.ctx.Err(); err != nil {
		return configdocument.Document{}, nativeCodecFailure(err, aggregate.CodecFailurePreservationUndefined, output.KeyPath{})
	}
	if len(changes) == 0 {
		return document, nil
	}
	edited, err := configdocument.Edit(codec.ctx, document, changes)
	return edited, nativeCodecFailure(err, aggregate.CodecFailurePreservationUndefined, output.KeyPath{})
}

func nativeDocument(document configdocument.Document) aggregate.Document {
	if !document.Exists() {
		return aggregate.AbsentDocument()
	}
	return aggregate.ExistingDocument(document.Bytes())
}
