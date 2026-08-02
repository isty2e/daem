package aggregatecodec

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

type registryTestCodec struct {
	contractID aggregate.CodecContractID
}

func (codec registryTestCodec) ContractID() aggregate.CodecContractID {
	return codec.contractID
}

func (registryTestCodec) MaximumDocumentBytes() int64 { return 1024 }

func (registryTestCodec) ValidateContribution(aggregate.ManagedContribution) error {
	return nil
}

func (registryTestCodec) Read(
	aggregate.Document,
	aggregate.Selection,
) (aggregate.Snapshot, *aggregate.CodecFailure) {
	return aggregate.Snapshot{}, nil
}

func (registryTestCodec) Render(
	aggregate.Document,
	aggregate.Plan,
) (aggregate.RenderedDocument, *aggregate.CodecFailure) {
	return aggregate.RenderedDocument{}, nil
}

func (registryTestCodec) Restore(
	aggregate.Document,
	aggregate.Snapshot,
) (aggregate.RenderedDocument, *aggregate.CodecFailure) {
	return aggregate.RenderedDocument{}, nil
}

func TestAggregateCodecRegistryRejectsMissingMismatchedAndDuplicateRows(t *testing.T) {
	const contractID aggregate.CodecContractID = "test-codec-v1"

	tests := []struct {
		name string
		run  func() error
		want string
	}{
		{
			name: "missing",
			run: func() error {
				return validateCodecRegistration(
					make(map[aggregate.CodecContractID]string),
					"test",
					contractID,
					nil,
					false,
				)
			},
			want: "is missing",
		},
		{
			name: "mismatched",
			run: func() error {
				return validateCodecRegistration(
					make(map[aggregate.CodecContractID]string),
					"test",
					contractID,
					registryTestCodec{contractID: "other-codec-v1"},
					true,
				)
			},
			want: "reports contract",
		},
		{
			name: "duplicate",
			run: func() error {
				owners := map[aggregate.CodecContractID]string{contractID: "first"}
				return validateCodecRegistration(
					owners,
					"second",
					contractID,
					registryTestCodec{contractID: contractID},
					true,
				)
			},
			want: "owned by both",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.run()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validate codec registration error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestAggregateCodecRegistryCoversEveryStaticPlacement(t *testing.T) {
	catalog := Catalog()

	for _, placement := range aggregate.ImplementedHookPlacements() {
		codec, ok := catalog.Lookup(placement.CodecContractID())
		if !ok || codec.ContractID() != placement.CodecContractID() {
			t.Fatalf("Hook codec %q is not selected exactly", placement.CodecContractID())
		}
	}
	for _, placement := range aggregate.ImplementedMCPPlacements() {
		codec, ok := catalog.Lookup(placement.CodecContractID())
		if !ok || codec.ContractID() != placement.CodecContractID() {
			t.Fatalf("MCP codec %q is not selected exactly", placement.CodecContractID())
		}
	}
	if codec, ok := catalog.Lookup("unknown-codec-v1"); ok || codec != nil {
		t.Fatalf("unknown codec = %#v, %v; want nil, false", codec, ok)
	}
}
