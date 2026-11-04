package observe

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestAggregateFailureObservationRetainsCompleteNativeDiagnostic(t *testing.T) {
	codec, admitted := aggregate.HostConfigCodecForSyntax("json")
	if !admitted {
		t.Fatal("native JSON contract was not admitted")
	}
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeGlobal, outputtest.Parse(t, "@pi-agent/settings.json"), outputtest.KeyFootprint(t, "selected"), codec)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	document := aggregate.ExistingDocument([]byte(`{"selected":"HOST_VALUE_CANARY"}`))
	for _, segments := range [][]string{nil, {""}, {"a/b"}, {"~1"}, {"."}, {"line\n\x00key"}} {
		location, err := output.NewKeyPath(segments...)
		if err != nil {
			t.Fatal(err)
		}
		failure, err := aggregate.NewCodecFailure(aggregate.CodecFailureDuplicateKey, location)
		if err != nil {
			t.Fatal(err)
		}
		observation, err := NewAggregateObservationFailure(document, selection, 0o600, failure)
		if err != nil {
			t.Fatal(err)
		}
		other, err := aggregate.NewCodecFailure(aggregate.CodecFailureCanonicalInvalid, output.KeyPath{})
		if err != nil {
			t.Fatal(err)
		}
		*failure = *other
		if observation.Location() != location || observation.Reason() != aggregate.CodecFailureDuplicateKey {
			t.Fatal("observed diagnostic reinterpreted native identity or aliased mutable input")
		}
		if !observation.Document().Equal(document) || observation.Selection().DocumentAddress() != selection.DocumentAddress() {
			t.Fatal("failed observation lost document/selection correlation")
		}
		if strings.Contains(observation.Error(), "HOST_VALUE_CANARY") || strings.ContainsAny(observation.Error(), "\n\x00") {
			t.Fatalf("failed observation disclosed raw values or controls: %q", observation.Error())
		}
	}
	if _, err := NewAggregateObservationFailure(document, selection, 0o600, &aggregate.CodecFailure{}); err == nil {
		t.Fatal("failed observation admitted an invalid diagnostic")
	}
}
