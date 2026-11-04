package aggregatecodec

import (
	"context"
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

var operationTestLimits = configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}

func TestOperationCatalogRunsNativeLifecycleWithoutChangingDefaultAdmission(t *testing.T) {
	fixedID := aggregate.ImplementedHookPlacements()[0].CodecContractID()
	for _, syntax := range []string{"json", "jsonc", "json-line-comments", "toml"} {
		t.Run(syntax, func(t *testing.T) {
			id, _ := aggregate.HostConfigCodecForSyntax(syntax)
			required := []aggregate.CodecContractID{id, fixedID, id}
			operations, err := ForOperation(t.Context(), required, operationTestLimits, operationTestLimits)
			if err != nil {
				t.Fatal(err)
			}
			values, err := ForContributions(t.Context(), required, operationTestLimits)
			if err != nil {
				t.Fatal(err)
			}
			set, selection, original := operationNativeFixture(t, id)
			required[0] = "unknown-after-construction"
			if err := operations.ValidateContributionSet(set); err != nil {
				t.Fatal(err)
			}
			if err := values.ValidateContributionSet(set); err != nil {
				t.Fatal(err)
			}
			if err := Catalog().ValidateContributionSet(set); err == nil {
				t.Fatal("operation construction enabled native values in the default catalog")
			}
			if err := Catalog().Contributions().ValidateContributionSet(set); err == nil {
				t.Fatal("value construction enabled contextless native family admission")
			}

			codec, _ := operations.Lookup(id)
			before, failure := codec.Read(original, selection)
			if failure != nil {
				t.Fatal(failure)
			}
			state, _ := before.State(set.Contract())
			intent, err := aggregate.NewProjectionIntent(state, &set)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := aggregate.NewPlan(before, []aggregate.ProjectionIntent{intent})
			if err != nil {
				t.Fatal(err)
			}
			candidate, failure := codec.Render(original, plan)
			if failure != nil {
				t.Fatal(failure)
			}
			actual, failure := codec.Read(candidate.Document(), selection)
			if failure != nil || !actual.Equal(candidate.Expected()) {
				t.Fatal("operation catalog changed the native render postcondition")
			}
			restored, failure := codec.Restore(candidate.Document(), before)
			if failure != nil {
				t.Fatal(failure)
			}
			actual, failure = codec.Read(restored.Document(), selection)
			if failure != nil || !actual.Equal(before) {
				t.Fatal("operation catalog changed the native recovery baseline")
			}

			fixed, _ := operations.Lookup(fixedID)
			originalFixed, _ := Catalog().Lookup(fixedID)
			if fixed.MaximumDocumentBytes() != originalFixed.MaximumDocumentBytes() {
				t.Fatal("native operation limits replaced the fixed codec's own policy")
			}
		})
	}
}

func TestOperationCatalogKeepsNativeContextsAndBudgetsIndependent(t *testing.T) {
	const id aggregate.CodecContractID = "native-json-v1"
	set, selection, original := operationNativeFixture(t, id)
	ctx, cancel := context.WithCancel(t.Context())
	first, err := ForOperation(ctx, []aggregate.CodecContractID{id}, operationTestLimits, operationTestLimits)
	if err != nil {
		t.Fatal(err)
	}
	firstValues, err := ForContributions(ctx, []aggregate.CodecContractID{id}, operationTestLimits)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ForOperation(t.Context(), []aggregate.CodecContractID{id}, operationTestLimits, operationTestLimits)
	if err != nil {
		t.Fatal(err)
	}
	secondValues, err := ForContributions(t.Context(), []aggregate.CodecContractID{id}, operationTestLimits)
	if err != nil {
		t.Fatal(err)
	}

	cancel()
	firstCodec, _ := first.Lookup(id)
	if snapshot, failure := firstCodec.Read(original, selection); failure == nil || failure.Reason() != aggregate.CodecFailureOperationCanceled || len(snapshot.States()) != 0 {
		t.Fatal("operation catalog discarded its native caller's cancelled context")
	}
	var cancellation *aggregate.CodecFailure
	if err := firstValues.ValidateContributionSet(set); !errors.As(err, &cancellation) || cancellation.Reason() != aggregate.CodecFailureOperationCanceled {
		t.Fatal("value catalog discarded its native caller's cancelled context")
	}
	secondCodec, _ := second.Lookup(id)
	if _, failure := secondCodec.Read(original, selection); failure != nil {
		t.Fatal("cancelling one operation poisoned another native operation")
	}
	if err := secondValues.ValidateContributionSet(set); err != nil {
		t.Fatal(err)
	}

	tiny := configdocument.Limits{MaximumBytes: 1, MaximumDepth: 64}
	smallDocument, err := ForOperation(t.Context(), []aggregate.CodecContractID{id}, tiny, operationTestLimits)
	if err != nil {
		t.Fatal(err)
	}
	if err := smallDocument.ValidateContributionSet(set); err != nil {
		t.Fatal("document byte limit leaked into native value-only validation")
	}
	smallCodec, _ := smallDocument.Lookup(id)
	if snapshot, failure := smallCodec.Read(original, selection); failure == nil || failure.Reason() != aggregate.CodecFailureResourceLimitExceeded || len(snapshot.States()) != 0 {
		t.Fatal("operation catalog ignored its explicit native document budget")
	}
	smallValues, err := ForContributions(t.Context(), []aggregate.CodecContractID{id}, tiny)
	if err != nil {
		t.Fatal(err)
	}
	var bounded *aggregate.CodecFailure
	if err := smallValues.ValidateContributionSet(set); !errors.As(err, &bounded) || bounded.Reason() != aggregate.CodecFailureResourceLimitExceeded {
		t.Fatal("value catalog ignored its independent native value budget")
	}
}

func TestOperationCatalogRefusesIncompleteConstruction(t *testing.T) {
	const id aggregate.CodecContractID = "native-json-v1"
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	for _, test := range []struct {
		name     string
		ctx      context.Context
		required []aggregate.CodecContractID
		limits   configdocument.Limits
	}{
		{"nil context", nil, []aggregate.CodecContractID{id}, operationTestLimits},
		{"cancelled context", cancelled, []aggregate.CodecContractID{id}, operationTestLimits},
		{"empty", t.Context(), nil, operationTestLimits},
		{"unknown after native", t.Context(), []aggregate.CodecContractID{id, "unknown"}, operationTestLimits},
		{"empty ID after native", t.Context(), []aggregate.CodecContractID{id, ""}, operationTestLimits},
		{"invalid native policy", t.Context(), []aggregate.CodecContractID{id}, configdocument.Limits{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			operations, err := ForOperation(test.ctx, test.required, test.limits, test.limits)
			if codec, present := operations.Lookup(id); err == nil || present || codec != nil {
				t.Fatal("failed operation construction returned partial native capability")
			}
			values, err := ForContributions(test.ctx, test.required, test.limits)
			if validator, present := values.Lookup(id); err == nil || present || validator != nil {
				t.Fatal("failed value construction returned partial native admission")
			}
		})
	}

	fixedID := aggregate.ImplementedHookPlacements()[0].CodecContractID()
	if _, err := ForOperation(t.Context(), []aggregate.CodecContractID{fixedID}, configdocument.Limits{}, configdocument.Limits{}); err != nil {
		t.Fatal("fixed-only operation required an irrelevant native policy")
	}
	if _, err := ForContributions(t.Context(), []aggregate.CodecContractID{fixedID}, configdocument.Limits{}); err != nil {
		t.Fatal("fixed-only values required an irrelevant native policy")
	}
}

func TestRequiredPortCollectionCancelsBetweenAndAfterResolvers(t *testing.T) {
	for _, cancelOn := range []aggregate.CodecContractID{"first", "last"} {
		ctx, cancel := context.WithCancel(t.Context())
		calls := 0
		ports, err := collectRequired(ctx, []aggregate.CodecContractID{"first", "first", "last"}, func(id aggregate.CodecContractID) (registryTestCodec, error) {
			calls++
			if id == cancelOn {
				cancel()
			}
			return registryTestCodec{contractID: id}, nil
		})
		if !errors.Is(err, context.Canceled) || ports != nil || calls != map[aggregate.CodecContractID]int{"first": 1, "last": 2}[cancelOn] {
			t.Fatal("cancellation returned partial ports or started an unadmitted resolver")
		}
	}
}

func operationNativeFixture(t *testing.T, id aggregate.CodecContractID) (aggregate.ContributionSet, aggregate.Selection, aggregate.Document) {
	t.Helper()
	key, _ := output.NewKeyPath("choice")
	footprint, _ := output.NewKeyFootprint(key)
	root, _ := output.Parse(".codex/extension.data")
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetCodex, target.ScopeProject, root, footprint, id)
	if err != nil {
		t.Fatal(err)
	}
	syntaxName, _ := aggregate.HostConfigSyntaxForCodec(id)
	syntax, err := configdocument.ParseSyntax(syntaxName)
	if err != nil {
		t.Fatal(err)
	}
	desired, original := `{"choice":true}`, `{"choice":false}`
	if syntax == configdocument.TOML {
		desired, original = "choice=true\n", "choice=false\n"
	}
	parsed, err := configdocument.Parse(t.Context(), []byte(desired), syntax, operationTestLimits)
	if err != nil {
		t.Fatal(err)
	}
	value, _, err := parsed.Lookup(key)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := value.CanonicalContext(t.Context(), operationTestLimits)
	if err != nil {
		t.Fatal(err)
	}
	body, err := aggregate.NewManagedContribution(contract, canonical)
	if err != nil {
		t.Fatal(err)
	}
	resource, _ := entity.New(entity.KindHostConfig, "native-operation")
	subject, err := topologyprojection.HostConfigKeySubject(resource, target.TargetCodex, target.ScopeProject, key)
	if err != nil {
		t.Fatal(err)
	}
	item, err := aggregate.NewSubjectContribution(subject, body)
	if err != nil {
		t.Fatal(err)
	}
	set, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{item})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	return set, selection, aggregate.ExistingDocument([]byte(original))
}
