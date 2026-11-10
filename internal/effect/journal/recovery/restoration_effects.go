package recovery

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// RestorationEffects describes semantic writes, not physical publication,
// namespace authority or an admitted recovery phase.
type RestorationEffects struct {
	values      []output.Footprint
	existence   bool
	mode        bool
	initialized bool
}

func (effects RestorationEffects) Validate() error {
	if !effects.initialized {
		return fmt.Errorf("restoration effects are required")
	}
	return nil
}

func (effects RestorationEffects) Changed() bool {
	return len(effects.values) != 0 || effects.existence || effects.mode
}

func (effects RestorationEffects) ValueFootprints() []output.Footprint {
	return slices.Clone(effects.values)
}

func (effects RestorationEffects) ChangesExistence() bool { return effects.existence }
func (effects RestorationEffects) ChangesMode() bool      { return effects.mode }

// NewFileRestorationEffects compares bytes independently of regular-root mode.
func NewFileRestorationEffects(current aggregate.Document, mode *PermissionMode, candidate aggregate.Document, candidateMode *PermissionMode) (RestorationEffects, error) {
	if err := current.Validate(); err != nil {
		return RestorationEffects{}, err
	}
	if err := candidate.Validate(); err != nil {
		return RestorationEffects{}, err
	}
	before, err := NewPartialDocumentState(current.Exists(), mode, nil)
	if err != nil {
		return RestorationEffects{}, err
	}
	after, err := NewPartialDocumentState(candidate.Exists(), candidateMode, nil)
	if err != nil {
		return RestorationEffects{}, err
	}
	effects, err := before.RestorationEffects(after)
	if err != nil {
		return RestorationEffects{}, err
	}
	if !current.Equal(candidate) {
		effects.values = []output.Footprint{output.WholePathFootprint()}
	}
	return effects, nil
}

// NewWholePathRestorationEffects describes an admitted tree replacement or
// whole-path deletion. It does not approximate a selected document edit.
func NewWholePathRestorationEffects(existed, willExist bool) (RestorationEffects, error) {
	if !existed && !willExist {
		return RestorationEffects{}, fmt.Errorf("whole-path restoration has no value to publish or remove")
	}
	return RestorationEffects{
		values:    []output.Footprint{output.WholePathFootprint()},
		existence: existed != willExist, initialized: true,
	}, nil
}

func (state PartialDocumentState) RestorationEffects(other PartialDocumentState) (RestorationEffects, error) {
	values, err := state.ChangedValueFootprints(other)
	if err != nil {
		return RestorationEffects{}, err
	}
	return RestorationEffects{
		values: values, existence: state.exists != other.exists,
		mode: state.exists && other.exists && state.mode != other.mode, initialized: true,
	}, nil
}

// RestoreProjectionDocument uses the actual codec candidate, including retained
// foreign siblings, to derive selected-value and root effects.
func RestoreProjectionDocument(current aggregate.Document, mode *PermissionMode, baseline aggregate.Snapshot, candidateMode *PermissionMode, codecs aggregate.CodecCatalog) (aggregate.RenderedDocument, RestorationEffects, error) {
	selection, err := baseline.Selection()
	if err != nil {
		return aggregate.RenderedDocument{}, RestorationEffects{}, err
	}
	codec, present := codecs.Lookup(selection.CodecContractID())
	if !present {
		return aggregate.RenderedDocument{}, RestorationEffects{}, fmt.Errorf("restoration codec %q is not admitted", selection.CodecContractID())
	}
	observed, failure := codec.Read(current, selection)
	if failure != nil {
		return aggregate.RenderedDocument{}, RestorationEffects{}, failure
	}
	rendered, failure := codec.Restore(current, baseline)
	if failure != nil {
		return aggregate.RenderedDocument{}, RestorationEffects{}, failure
	}
	if !rendered.Document().Exists() {
		candidateMode = nil
	}
	before, err := NewPartialDocumentState(current.Exists(), mode, []aggregate.Snapshot{observed})
	if err != nil {
		return aggregate.RenderedDocument{}, RestorationEffects{}, err
	}
	after, err := NewPartialDocumentState(rendered.Document().Exists(), candidateMode, []aggregate.Snapshot{rendered.Expected()})
	if err != nil {
		return aggregate.RenderedDocument{}, RestorationEffects{}, err
	}
	effects, err := before.RestorationEffects(after)
	return rendered, effects, err
}
