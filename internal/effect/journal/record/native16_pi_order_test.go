package record

import (
	"bytes"
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func TestNative16PiOrderReplayKeepsHistoricalProjectionAndCommonInverse(t *testing.T) {
	for _, test := range []struct {
		scope       target.Scope
		destination string
	}{
		{target.ScopeProject, ".pi/settings.json"},
		{target.ScopeGlobal, "@pi-agent/settings.json"},
	} {
		t.Run(string(test.scope), func(t *testing.T) {
			input, observed := piEvidenceFixture(t, test.scope)
			evidence, err := persistedPiOrderEvidence(observed)
			if err != nil {
				t.Fatal(err)
			}
			order, err := evidence.canonical(t.Context(), input.Document)
			if err != nil {
				t.Fatal(err)
			}
			alias, err := PiOrderDocumentAlias(test.scope)
			if err != nil || alias.AggregateRoot().String() != test.destination {
				t.Fatal("native16 Pi alias changed", alias, err)
			}
			selected, err := order.SelectedSequenceEdit()
			projection := selected.Projection()
			if err != nil || projection.Destination() != alias.AggregateRoot() ||
				projection.Grammar() != configuration.JSON || projection.ArrayPath().String() != "/packages" ||
				string(order.Sequence().SequenceID()) != "pi:"+string(test.scope)+":settings.packages" ||
				evidence.Constraint.ClassID != "extension:pi:"+string(test.scope)+":packages" ||
				evidence.Constraint.MemberIdentityContract != "pi-package-load-identity-v1" {
				t.Fatal("native16 replay reinterpreted its historical Pi contract", err)
			}
			expected, exists := order.Candidate()
			if !exists {
				t.Fatal("order replay lost its existing settings document")
			}
			mode := recovery.PermissionMode(0o600)
			candidate, err := ReconstructNative16Selected(t.Context(), journaldocument.ComparisonInput{
				Before: input.Document, Expected: aggregate.ExistingDocument(expected),
				BeforeMode: &mode, ExpectedMode: &mode,
				Limits: configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32},
			}, nil, &order)
			if err != nil {
				t.Fatal(err)
			}
			current := aggregate.ExistingDocument(bytes.Replace(expected, []byte(`"theme":"day"`), []byte(`"theme":"night"`), 1))
			restored, err := candidate.Comparison().RestoreBefore(t.Context(), current, &mode)
			want := bytes.Replace(input.Document.Content(), []byte(`"theme":"day"`), []byte(`"theme":"night"`), 1)
			if err != nil || !bytes.Equal(restored.Document().Content(), want) {
				t.Fatal("native16 common inverse lost original tuple/foreign rows or an outside edit", err)
			}
			commented := aggregate.ExistingDocument(append([]byte("// not strict Pi JSON\n"), expected...))
			if match, err := candidate.Comparison().Match(t.Context(), commented, &mode); !errors.Is(err, configdocument.ErrInvalidDocument) || match.Recoverable() {
				t.Fatal("native16 comparison widened its historical strict-JSON grammar", err)
			}
			if restored, err := candidate.Comparison().RestoreBefore(t.Context(), commented, &mode); !errors.Is(err, configdocument.ErrInvalidDocument) || restored.Validate() == nil {
				t.Fatal("native16 restoration admitted a comment-bearing settings document", err)
			}
		})
	}
}

func TestNative16PiOrderCaptureRefusesAnUnrecordableProjectionContract(t *testing.T) {
	input, _ := piEvidenceFixture(t, target.ScopeProject)
	projection := input.Capability.NativeSequences()[0]
	alternate, err := profile.NewNativeSequenceProjection(projection.SequenceID(), projection.Destination(), configuration.JSONC, projection.ArrayPath())
	if err != nil {
		t.Fatal(err)
	}
	input.Capability, err = profile.NewExtensionOrderCapability(profile.ExtensionOrderCapabilityInput{
		Carrier: input.Capability.Carrier(), Scope: input.Scope, ClassID: input.Capability.ClassID(),
		MemberIdentityContract: input.Capability.MemberIdentityContract(), SequenceMembership: input.Capability.SequenceMembership(),
		NativeSequences: []profile.NativeSequenceProjection{alternate}, RuntimeMeaning: input.Capability.RuntimeMeaning(),
	})
	if err != nil {
		t.Fatal(err)
	}
	observation, err := pipackage.ReconstructOrder(input)
	if err != nil || !observation.Changed() {
		t.Fatal("fixture did not establish a different but valid selected contract", err)
	}
	if evidence, err := persistedPiOrderEvidence(observation); err == nil || evidence.SequenceID != "" {
		t.Fatal("native16 persisted an interpretation its wire grammar cannot retain", err)
	}
}
