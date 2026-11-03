package nativecodec_test

import (
	"context"
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
)

func TestNativeStaticAdmissionUsesTheOperationValueInterpreter(t *testing.T) {
	for _, syntax := range []configdocument.Syntax{configdocument.JSON, configdocument.JSONC, configdocument.JSONLineComments, configdocument.TOML} {
		codec := fixtureCodec(t, syntax)
		for _, text := range []string{"true", "{}", "[1,true]"} {
			set := fixtureSet(t, codec, key(t, "value"), fixtureValue(t, syntax, text))
			if err := nativecodec.ValidateContributions(t.Context(), codec.ContractID(), set, fixtureLimits); err != nil {
				t.Fatal(err)
			}
			if err := codec.ValidateContributions(set); err != nil {
				t.Fatal(err)
			}
		}
		object := `{"nested":true}`
		if syntax == configdocument.TOML {
			object = "{nested=true}"
		}
		set := fixtureSet(t, codec, key(t, "value"), fixtureValue(t, syntax, object))
		var direct, operation *aggregate.CodecFailure
		if err := nativecodec.ValidateContributions(t.Context(), codec.ContractID(), set, fixtureLimits); !errors.As(err, &direct) {
			t.Fatal("static admission accepted a nonempty desired object")
		}
		if err := codec.ValidateContributions(set); !errors.As(err, &operation) || direct.Reason() != operation.Reason() || direct.Location() != operation.Location() {
			t.Fatal("static and operation value admission disagree")
		}
	}
}

func TestNativeStaticAdmissionRequiresExplicitValuePolicyAndLiveContext(t *testing.T) {
	codec := fixtureCodec(t, configdocument.JSON)
	set := fixtureSet(t, codec, key(t, "value"), fixtureValue(t, configdocument.JSON, "true"))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var failure *aggregate.CodecFailure
	if err := nativecodec.ValidateContributions(ctx, codec.ContractID(), set, fixtureLimits); !errors.As(err, &failure) || failure.Reason() != aggregate.CodecFailureOperationCanceled {
		t.Fatal("static admission hid an immortal context")
	}
	if err := nativecodec.ValidateContributions(t.Context(), codec.ContractID(), set, configdocument.Limits{MaximumBytes: 1, MaximumDepth: 1}); !errors.As(err, &failure) || failure.Reason() != aggregate.CodecFailureResourceLimitExceeded {
		t.Fatal("static admission ignored its independent value budget")
	}
	if err := nativecodec.ValidateContributions(nil, codec.ContractID(), set, fixtureLimits); err == nil {
		t.Fatal("missing static validation context was inferred")
	}
	if err := nativecodec.ValidateContributions(t.Context(), codec.ContractID(), set, configdocument.Limits{}); err == nil {
		t.Fatal("missing static value policy was inferred")
	}
	if err := nativecodec.ValidateContributions(t.Context(), "unknown", set, fixtureLimits); err == nil {
		t.Fatal("unknown native value grammar was accepted")
	}
	if err := nativecodec.ValidateContributions(t.Context(), codec.ContractID(), aggregate.ContributionSet{}, fixtureLimits); err == nil {
		t.Fatal("empty contribution set acquired static admission")
	}
}
