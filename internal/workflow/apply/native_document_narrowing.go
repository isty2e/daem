package apply

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	reconcileprojection "github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/supply/artifact"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

var ErrNativeDocumentBaseline = errors.New("verified native whole-document baseline is required")

type NativeDocumentNarrowingInput struct {
	Baseline        durable.ManagedPathState
	BaselineContent []byte
	Current         aggregate.Document
	Desired         []aggregate.ContributionSet
	CodecID         aggregate.CodecContractID
	DocumentLimits  configdocument.Limits
	ValueLimits     configdocument.Limits
}

// NativeDocumentNarrowing releases the old whole-file scope without making
// released values deletion intents. It grants no filesystem mutation authority.
type NativeDocumentNarrowing struct {
	plan             aggregate.Plan
	expected         aggregate.Document
	wholeFileChanged bool
	hasPlan          bool
	initialized      bool
}

func (narrowing NativeDocumentNarrowing) Validate() error {
	if !narrowing.initialized {
		return fmt.Errorf("native document narrowing is required")
	}
	return nil
}

// KeyPlan is absent for a release-only transition with no retained keys.
func (narrowing NativeDocumentNarrowing) KeyPlan() (aggregate.Plan, bool) {
	return narrowing.plan, narrowing.hasPlan
}

func (narrowing NativeDocumentNarrowing) ExpectedDocument() aggregate.Document {
	return narrowing.expected
}

// WholeFileChanged reports byte drift, not proof that a particular released key changed.
func (narrowing NativeDocumentNarrowing) WholeFileChanged() bool {
	return narrowing.wholeFileChanged
}

func BuildNativeDocumentNarrowing(ctx context.Context, input NativeDocumentNarrowingInput) (NativeDocumentNarrowing, error) {
	codec, err := nativecodec.New(ctx, input.CodecID, input.DocumentLimits, input.ValueLimits)
	if err != nil {
		return NativeDocumentNarrowing{}, err
	}
	owner, entityBacked := topologyprojection.EntityID(input.Baseline.Subject())
	if !entityBacked || owner.Kind() != entity.KindHostConfig ||
		input.Baseline.ContentKind() != realization.PathProjectionFile ||
		len(input.Baseline.ConsumerTargets()) != 1 || input.Baseline.FileMode()&0o111 != 0 {
		return NativeDocumentNarrowing{}, ErrNativeDocumentBaseline
	}
	if err := input.Baseline.ContentHash().Validate(); err != nil {
		return NativeDocumentNarrowing{}, fmt.Errorf("%w: %v", ErrNativeDocumentBaseline, err)
	}
	if int64(len(input.BaselineContent)) > input.DocumentLimits.MaximumBytes {
		return NativeDocumentNarrowing{}, configdocument.ErrLimitExceeded
	}
	baselineContent := append([]byte(nil), input.BaselineContent...)
	if artifact.HashFileContent(baselineContent) != input.Baseline.ContentHash() {
		return NativeDocumentNarrowing{}, ErrNativeDocumentBaseline
	}
	if !input.Current.Exists() {
		return NativeDocumentNarrowing{}, fmt.Errorf("narrowing requires an existing native document")
	}
	syntaxName, _ := aggregate.HostConfigSyntaxForCodec(input.CodecID)
	syntax, err := configdocument.ParseSyntax(syntaxName)
	if err != nil {
		return NativeDocumentNarrowing{}, err
	}
	currentContent := input.Current.Content()
	currentDocument, err := configdocument.Parse(ctx, currentContent, syntax, input.DocumentLimits)
	if err != nil {
		return NativeDocumentNarrowing{}, err
	}
	root, present, err := currentDocument.Lookup(output.KeyPath{})
	if err != nil || !present || !root.IsObject() {
		return NativeDocumentNarrowing{}, configdocument.ErrInvalidDocument
	}
	result := NativeDocumentNarrowing{
		expected: input.Current, initialized: true,
		wholeFileChanged: artifact.HashFileContent(currentContent) != input.Baseline.ContentHash(),
	}
	if len(input.Desired) == 0 {
		if _, err := configdocument.Parse(ctx, baselineContent, syntax, input.DocumentLimits); err != nil {
			return NativeDocumentNarrowing{}, err
		}
		return result, nil
	}

	contracts := make([]aggregate.ProjectionContract, len(input.Desired))
	for index, set := range input.Desired {
		if err := codec.ValidateContributions(set); err != nil {
			return NativeDocumentNarrowing{}, err
		}
		contract := set.Contract()
		document := contract.Address().Document()
		if document.AggregateRoot() != input.Baseline.Destination() || document.Scope() != input.Baseline.Scope() ||
			document.Target() != input.Baseline.ConsumerTargets()[0] {
			return NativeDocumentNarrowing{}, fmt.Errorf("narrowing changes the managed document placement")
		}
		for _, item := range set.Contributions() {
			id, _, native := topologyprojection.HostConfigKeyIdentity(item.SubjectID())
			if !native || id != owner {
				return NativeDocumentNarrowing{}, fmt.Errorf("narrowing changes the managed declaration")
			}
		}
		contracts[index] = contract
	}
	selection, err := aggregate.NewSelection(contracts)
	if err != nil {
		return NativeDocumentNarrowing{}, err
	}
	baseline, failure := codec.Read(aggregate.ExistingDocument(baselineContent), selection)
	if failure != nil {
		return NativeDocumentNarrowing{}, failure
	}
	current, failure := codec.Read(input.Current, selection)
	if failure != nil {
		return NativeDocumentNarrowing{}, failure
	}
	plan, err := reconcileprojection.BuildNativeNarrowingPlan(baseline, current, input.Desired)
	if err != nil {
		return NativeDocumentNarrowing{}, err
	}
	rendered, failure := codec.Render(input.Current, plan)
	if failure != nil {
		return NativeDocumentNarrowing{}, failure
	}
	result.plan, result.hasPlan, result.expected = plan, true, rendered.Document()
	return result, nil
}
