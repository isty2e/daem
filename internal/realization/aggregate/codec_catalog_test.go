package aggregate

import (
	"errors"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
	topologyhook "github.com/isty2e/daem/internal/topology/hook"
)

type codecCatalogTestCodec struct {
	contractID      CodecContractID
	maximumBytes    int64
	contributionErr error
}

func (codec *codecCatalogTestCodec) ContractID() CodecContractID {
	return codec.contractID
}

func (codec *codecCatalogTestCodec) MaximumDocumentBytes() int64 {
	if codec.maximumBytes == 0 {
		return 1024
	}
	return codec.maximumBytes
}

func (codec *codecCatalogTestCodec) ValidateContribution(ManagedContribution) error {
	return codec.contributionErr
}

func (*codecCatalogTestCodec) Read(Document, Selection) (Snapshot, *CodecFailure) {
	return Snapshot{}, nil
}

func (*codecCatalogTestCodec) ClassifyContributionOccupancy(
	ProjectionState,
	ContributionSet,
) (ContributionOccupancySet, error) {
	return ContributionOccupancySet{}, nil
}

func (*codecCatalogTestCodec) Render(Document, Plan) (RenderedDocument, *CodecFailure) {
	return RenderedDocument{}, nil
}

func (*codecCatalogTestCodec) Restore(Document, Snapshot) (RenderedDocument, *CodecFailure) {
	return RenderedDocument{}, nil
}

func TestNewCodecCatalogRejectsMissingNilAndDuplicateCodecs(t *testing.T) {
	const contractID CodecContractID = "test-codec-v1"
	var typedNil *codecCatalogTestCodec

	tests := []struct {
		name   string
		codecs []Codec
		want   string
	}{
		{name: "missing", want: "catalog is required"},
		{name: "nil interface", codecs: []Codec{nil}, want: "catalog[0] is nil"},
		{name: "typed nil", codecs: []Codec{typedNil}, want: "catalog[0] is nil"},
		{
			name: "duplicate contract",
			codecs: []Codec{
				&codecCatalogTestCodec{contractID: contractID},
				&codecCatalogTestCodec{contractID: contractID},
			},
			want: "registered more than once",
		},
		{
			name:   "invalid document byte limit",
			codecs: []Codec{&codecCatalogTestCodec{contractID: contractID, maximumBytes: -1}},
			want:   "non-positive document byte limit",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewCodecCatalog(test.codecs)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewCodecCatalog error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestCodecCatalogRejectsReportedIdentityDrift(t *testing.T) {
	codec := &codecCatalogTestCodec{contractID: "test-codec-v1"}
	catalog, err := NewCodecCatalog([]Codec{codec})
	if err != nil {
		t.Fatal(err)
	}

	codec.contractID = "other-codec-v1"
	if found, ok := catalog.Lookup("test-codec-v1"); ok || found != nil {
		t.Fatalf("Lookup accepted identity-drifting codec: %#v, %t", found, ok)
	}
	if found, ok := catalog.Lookup("other-codec-v1"); ok || found != nil {
		t.Fatalf("Lookup accepted unregistered reported identity: %#v, %t", found, ok)
	}
}

func TestCodecCatalogValidatesContributionThroughExactCodec(t *testing.T) {
	placement, ok := HookPlacementFor(target.TargetCodex, target.ScopeProject)
	if !ok {
		t.Fatal("Codex project Hook placement is missing")
	}
	contribution, err := placement.Contribution("opaque-but-structurally-valid")
	if err != nil {
		t.Fatal(err)
	}
	id, err := entity.New(entity.KindHook, "guard")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyhook.ProjectionSubjectID(id, target.TargetCodex, target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}

	wantErr := errors.New("codec rejected contribution")
	catalog, err := NewCodecCatalog([]Codec{&codecCatalogTestCodec{
		contractID:      placement.CodecContractID(),
		contributionErr: wantErr,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.ValidateSubjectContribution(subject, contribution); !errors.Is(err, wantErr) {
		t.Fatalf("ValidateSubjectContribution error = %v, want %v", err, wantErr)
	}

	unknown, err := NewCodecCatalog([]Codec{&codecCatalogTestCodec{contractID: "other-codec-v1"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := unknown.ValidateSubjectContribution(subject, contribution); err == nil ||
		!strings.Contains(err.Error(), "is not admitted") {
		t.Fatalf("unknown codec validation error = %v, want admission rejection", err)
	}
}
