package document_test

import (
	"bytes"
	"errors"
	"testing"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeDocumentBoundariesRejectOversizedInputBeforeCopying(t *testing.T) {
	limits := configdocument.Limits{MaximumBytes: 4 << 20, MaximumDepth: 32}
	document := aggregate.ExistingDocument(bytes.Repeat([]byte(" "), int(2*limits.MaximumBytes)))
	codec, err := nativecodec.New(t.Context(), "native-json-v1", limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	root, err := output.Parse(".pi/custom.json")
	if err != nil {
		t.Fatal(err)
	}
	key, err := output.NewKeyPath("model")
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeProject, root, footprint, codec.ContractID())
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	participant, err := compose.NewParticipant(codec, selection, configdocument.JSON, []output.KeyPath{key})
	if err != nil {
		t.Fatal(err)
	}
	mode := recovery.NewPermissionMode(0o600)
	for _, test := range []struct {
		name    string
		read    func() error
		isLimit func(error) bool
	}{
		{"comparison", func() error {
			_, err := journaldocument.NewWholeComparison(t.Context(), journaldocument.ComparisonInput{
				Before: document, Expected: aggregate.AbsentDocument(), BeforeMode: mode, Limits: limits,
			})
			return err
		}, func(err error) bool { return err != nil }},
		{"native codec", func() error {
			_, failure := codec.Read(document, selection)
			if failure == nil {
				return nil
			}
			return failure
		}, func(err error) bool {
			var failure *aggregate.CodecFailure
			return errors.As(err, &failure) && failure.Reason() == aggregate.CodecFailureResourceLimitExceeded
		}},
		{"composition", func() error {
			_, err := compose.ReadBaselines(t.Context(), document, limits, []compose.Participant{participant})
			return err
		}, func(err error) bool { return errors.Is(err, configdocument.ErrLimitExceeded) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.read(); !test.isLimit(err) {
				t.Fatalf("oversized input did not retain its limit refusal: %v", err)
			}
			result := testing.Benchmark(func(b *testing.B) {
				for range b.N {
					if err := test.read(); err == nil {
						b.Fatal("oversized document was accepted")
					}
				}
			})
			allocated := result.AllocedBytesPerOp()
			t.Logf("oversized rejection: %d B/op", allocated)
			if result.N == 0 || allocated >= limits.MaximumBytes {
				t.Fatalf("oversized rejection allocated %d bytes per call before the selected byte limit", allocated)
			}
		})
	}
}
