package clipresent

import (
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/supply/artifact"
)

func TestExactIdentityChangeNamesIncludeEveryChangedComponent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		changes artifact.ExactIdentityChanges
		want    []string
	}{
		{"equal", artifact.ExactIdentityChanges{}, nil},
		{"source", artifact.ExactIdentityChanges{SourceID: true}, []string{"source_id"}},
		{"revision", artifact.ExactIdentityChanges{ResolvedRef: true}, []string{"resolved_ref"}},
		{"kind", artifact.ExactIdentityChanges{Kind: true}, []string{"kind"}},
		{"hash", artifact.ExactIdentityChanges{ContentHash: true}, []string{"content_hash"}},
		{"unclassified", artifact.ExactIdentityChanges{Unclassified: true}, []string{"exact_supply"}},
		{
			"known and unclassified",
			artifact.ExactIdentityChanges{ResolvedRef: true, Unclassified: true},
			[]string{"resolved_ref", "exact_supply"},
		},
		{
			"all in stable order",
			artifact.ExactIdentityChanges{SourceID: true, ResolvedRef: true, Kind: true, ContentHash: true, Unclassified: true},
			[]string{"source_id", "resolved_ref", "kind", "content_hash", "exact_supply"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := exactIdentityChangeNames(test.changes); !slices.Equal(got, test.want) {
				t.Fatalf("change names = %q, want %q", got, test.want)
			}
		})
	}
}

func TestExactSupplyChangeNamesSeparateFacetPresenceFromComponents(t *testing.T) {
	t.Parallel()

	hash := artifact.HashFileContent([]byte("same content"))
	file, err := artifact.NewExactIdentity("source", "ref", artifact.ArtifactKindFile, hash)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := artifact.NewExactIdentity("source", "ref", artifact.ArtifactKindDirectory, hash)
	if err != nil {
		t.Fatal(err)
	}
	present := optional[artifact.ExactIdentity]{value: file, present: true}
	absent := optional[artifact.ExactIdentity]{}

	tests := []struct {
		name   string
		before optional[artifact.ExactIdentity]
		after  optional[artifact.ExactIdentity]
		want   []string
	}{
		{"both absent", absent, absent, nil},
		{"facet added", absent, present, []string{"exact_supply"}},
		{"facet removed", present, absent, []string{"exact_supply"}},
		{"equal", present, present, nil},
		{"kind only", present, optional[artifact.ExactIdentity]{value: directory, present: true}, []string{"kind"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := exactSupplyChangeNames(test.before, test.after); !slices.Equal(got, test.want) {
				t.Fatalf("change names = %q, want %q", got, test.want)
			}
		})
	}
}

func TestChangedFacetNamesAreEmptyForEqualContracts(t *testing.T) {
	t.Parallel()

	contract := snapshottest.ExactSupply(t, snapshottest.ExactSupplyInput{
		Kind:         entity.KindSkill,
		Name:         "current",
		SourceID:     "local:skills/current?mode=vendor",
		ArtifactKind: artifact.ArtifactKindDirectory,
		ContentHash:  artifact.HashFileContent([]byte("current")),
	})
	if !contract.Equal(contract) {
		t.Fatal("fixture contract is not canonically equal to itself")
	}
	if names := changedFacetNames(contract, contract); len(names) != 0 {
		t.Fatalf("equal contract change reasons = %q, want none", names)
	}
}
