package nativecodec_test

import (
	"context"
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
)

func TestNativeValueValidatorUsesTheSameAdmissionWithoutADocumentBudget(t *testing.T) {
	for _, syntax := range []configdocument.Syntax{configdocument.JSON, configdocument.JSONC, configdocument.JSONLineComments, configdocument.TOML} {
		codec := fixtureCodec(t, syntax)
		validator, err := nativecodec.NewContributionValidator(t.Context(), codec.ContractID(), fixtureLimits)
		if err != nil {
			t.Fatal(err)
		}
		catalog, err := aggregate.NewContributionCatalog([]aggregate.ContributionValidator{validator})
		if err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{"true", "{}", "[1,true]"} {
			set := fixtureSet(t, codec, key(t, "value"), fixtureValue(t, syntax, text))
			if err := catalog.ValidateContributionSet(set); err != nil {
				t.Fatal(err)
			}
			if err := codec.ValidateContributions(set); err != nil {
				t.Fatal(err)
			}
		}
		text := `{"nested":true}`
		if syntax == configdocument.TOML {
			text = "{nested=true}"
		}
		set := fixtureSet(t, codec, key(t, "value"), fixtureValue(t, syntax, text))
		var actual, expected *aggregate.CodecFailure
		if err := validator.ValidateContributions(set); !errors.As(err, &actual) {
			t.Fatal("static-only validator accepted a non-atomic desired object")
		}
		if err := codec.ValidateContributions(set); !errors.As(err, &expected) || actual.Reason() != expected.Reason() || actual.Location() != expected.Location() {
			t.Fatal("static-only validator changed native value refusal semantics")
		}
	}
}

func TestNativeValueValidatorRequiresLiveContextAndExplicitValuePolicy(t *testing.T) {
	codec := fixtureCodec(t, configdocument.JSON)
	set := fixtureSet(t, codec, key(t, "value"), fixtureValue(t, configdocument.JSON, "true"))
	ctx, cancel := context.WithCancel(t.Context())
	validator, err := nativecodec.NewContributionValidator(ctx, codec.ContractID(), fixtureLimits)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	var failure *aggregate.CodecFailure
	if err := validator.ValidateContributions(set); !errors.As(err, &failure) || failure.Reason() != aggregate.CodecFailureOperationCanceled {
		t.Fatal("static-only validator hid cancellation after construction")
	}
	if validator, err := nativecodec.NewContributionValidator(ctx, codec.ContractID(), fixtureLimits); !errors.Is(err, context.Canceled) || validator != nil {
		t.Fatal("canceled construction published a validator")
	}
	for _, input := range []struct {
		ctx    context.Context
		id     aggregate.CodecContractID
		limits configdocument.Limits
	}{
		{nil, codec.ContractID(), fixtureLimits},
		{t.Context(), "unknown", fixtureLimits},
		{t.Context(), codec.ContractID(), configdocument.Limits{}},
	} {
		if validator, err := nativecodec.NewContributionValidator(input.ctx, input.id, input.limits); err == nil || validator != nil {
			t.Fatal("invalid static capability was published")
		}
	}
	bounded, err := nativecodec.NewContributionValidator(t.Context(), codec.ContractID(), configdocument.Limits{MaximumBytes: 1, MaximumDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := bounded.ValidateContributions(set); !errors.As(err, &failure) || failure.Reason() != aggregate.CodecFailureResourceLimitExceeded {
		t.Fatal("static-only validator ignored the supplied value budget")
	}
}
