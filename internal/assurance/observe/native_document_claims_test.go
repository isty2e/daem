package observe

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	"github.com/isty2e/daem/test/outputtest"
)

func TestNativeDocumentClaimsPreserveFactsAndRefuseInvalidFragments(t *testing.T) {
	root := t.TempDir()
	exact := pathtest.Exact(filepath.Join(root, "settings.json"))
	owner, err := stateauthority.New(pathtest.Exact(filepath.Join(root, "state.json")), filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	alias, err := aggregate.NewDocumentAddress(target.TargetPi, target.ScopeGlobal, outputtest.Parse(t, "@pi-agent/settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := entity.New(entity.KindHostConfig, "settings")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := aggregate.HostConfigDocumentSubject(id, target.TargetPi, target.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	address, err := ownership.NewManagedAddress(exact, outputtest.KeyFootprint(t, "a"))
	if err != nil {
		t.Fatal(err)
	}
	claim, err := ownership.NewActiveClaim(address, owner)
	if err != nil {
		t.Fatal(err)
	}
	input := NativeDocumentClaimObservationInput{
		Owner: owner, Comparison: exact, Aliases: []aggregate.DocumentAddress{alias},
		BeforeSubjects: []topology.SubjectID{subject}, BeforeClaims: []ownership.Claim{claim}, CurrentClaims: []ownership.Claim{claim},
	}
	observation, err := NewNativeDocumentClaimObservation(input)
	if err != nil || observation.Validate() != nil {
		t.Fatal("valid qualified fragment was refused", err)
	}
	input.Aliases[0] = aggregate.DocumentAddress{}
	input.BeforeSubjects[0] = topology.SubjectID{}
	input.BeforeClaims[0] = ownership.Claim{}
	input.CurrentClaims[0] = ownership.Claim{}
	observation.Aliases()[0] = aggregate.DocumentAddress{}
	observation.BeforeSubjects()[0] = topology.SubjectID{}
	observation.BeforeClaims()[0] = ownership.Claim{}
	observation.CurrentClaims()[0] = ownership.Claim{}
	if observation.Aliases()[0] != alias || observation.BeforeSubjects()[0] != subject || !observation.BeforeClaims()[0].Equal(claim) || !observation.CurrentClaims()[0].Equal(claim) || !observation.ComparisonNamespace().Equal(exact) || observation.Owner() != owner {
		t.Fatal("constructor or returned slices exposed mutable ownership facts")
	}
	for _, fault := range []string{"zero owner", "zero comparison", "no alias", "duplicate alias", "invalid alias", "duplicate subject", "invalid subject", "absent current", "duplicate current", "invalid claim", "reserved before", "different namespace", "unrelated current"} {
		t.Run(fault, func(t *testing.T) {
			candidate := NativeDocumentClaimObservationInput{Owner: owner, Comparison: exact, Aliases: []aggregate.DocumentAddress{alias}, BeforeSubjects: []topology.SubjectID{subject}, BeforeClaims: []ownership.Claim{claim}, CurrentClaims: []ownership.Claim{claim}}
			switch fault {
			case "zero owner":
				candidate.Owner = stateauthority.Authority{}
			case "zero comparison":
				candidate.Comparison = pathauthority.Exact{}
			case "no alias":
				candidate.Aliases = nil
			case "duplicate alias":
				candidate.Aliases = append(candidate.Aliases, alias)
			case "invalid alias":
				candidate.Aliases[0] = aggregate.DocumentAddress{}
			case "duplicate subject":
				candidate.BeforeSubjects = append(candidate.BeforeSubjects, subject)
			case "invalid subject":
				candidate.BeforeSubjects[0] = topology.SubjectID{}
			case "absent current":
				candidate.CurrentClaims = nil
			case "duplicate current":
				candidate.CurrentClaims = append(candidate.CurrentClaims, claim)
			case "invalid claim":
				candidate.CurrentClaims[0] = ownership.Claim{}
			case "reserved before":
				reserved, err := ownership.NewReservedClaim(address, owner, "pending")
				if err != nil {
					t.Fatal(err)
				}
				candidate.BeforeClaims, candidate.CurrentClaims = []ownership.Claim{reserved}, []ownership.Claim{reserved}
			case "different namespace":
				candidate.Comparison = pathtest.Exact(filepath.Join(root, "other.json"))
			case "unrelated current":
				otherAddress, err := ownership.NewManagedAddress(pathtest.Exact(filepath.Join(root, "other.json")), address.Footprint())
				if err != nil {
					t.Fatal(err)
				}
				other, err := ownership.NewActiveClaim(otherAddress, owner)
				if err != nil {
					t.Fatal(err)
				}
				candidate.CurrentClaims = append(candidate.CurrentClaims, other)
			}
			if result, err := NewNativeDocumentClaimObservation(candidate); err == nil || result.Validate() == nil {
				t.Fatal("invalid claim fragment returned a usable observation")
			}
		})
	}
}
