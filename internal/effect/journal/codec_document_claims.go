package journal

import (
	"fmt"
	"slices"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	footprintcodec "github.com/isty2e/daem/internal/encoding/footprint"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

// Group claim DTOs preserve evidence, not group coverage or mutation authority.
// The native journal must bind their aliases and phases to its complete state.
type recoveryDocumentClaimSet struct {
	Kind     string                                `json:"kind"`
	Before   *[]recoveryClaimValue                 `json:"before,omitempty"`
	After    *[]recoveryClaimValue                 `json:"after,omitempty"`
	Acquires *[]recoveryDocumentProvisionalAcquire `json:"acquires,omitempty"`
}

type recoveryDocumentAliasRef struct {
	Target      string `json:"target"`
	Scope       string `json:"scope"`
	Destination string `json:"destination"`
}

type recoveryDocumentProvisionalAcquire struct {
	Alias              recoveryDocumentAliasRef `json:"alias"`
	Footprint          footprintcodec.Record    `json:"footprint"`
	CandidateAuthority pathAuthorityDTO         `json:"candidate_authority"`
	NamespaceAuthority pathAuthorityDTO         `json:"namespace_authority"`
	StatefileAuthority pathAuthorityDTO         `json:"statefile_authority"`
	ManifestPath       string                   `json:"manifest_path"`
	OperationID        string                   `json:"operation_id"`
}

func (record *recoveryDocumentClaimSet) UnmarshalJSON(content []byte) error {
	if record == nil {
		return fmt.Errorf("document claim set destination is nil")
	}
	if err := requireDocumentEvidenceFields(content, "kind"); err != nil {
		return err
	}
	type wire recoveryDocumentClaimSet
	var decoded wire
	if err := decodeRecoveryJSONStrict(content, &decoded); err != nil {
		return err
	}
	candidate := recoveryDocumentClaimSet(decoded)
	if err := candidate.validateShape(); err != nil {
		return err
	}
	*record = candidate
	return nil
}

func (record *recoveryDocumentProvisionalAcquire) UnmarshalJSON(content []byte) error {
	if record == nil {
		return fmt.Errorf("document provisional acquire destination is nil")
	}
	if err := requireDocumentEvidenceFields(content, "alias", "footprint", "candidate_authority", "namespace_authority", "statefile_authority", "manifest_path", "operation_id"); err != nil {
		return err
	}
	type wire recoveryDocumentProvisionalAcquire
	var decoded wire
	if err := decodeRecoveryJSONStrict(content, &decoded); err != nil {
		return err
	}
	*record = recoveryDocumentProvisionalAcquire(decoded)
	return nil
}

func (alias *recoveryDocumentAliasRef) UnmarshalJSON(content []byte) error {
	if alias == nil {
		return fmt.Errorf("document alias destination is nil")
	}
	if err := requireDocumentEvidenceFields(content, "target", "scope", "destination"); err != nil {
		return err
	}
	type wire recoveryDocumentAliasRef
	var decoded wire
	if err := decodeRecoveryJSONStrict(content, &decoded); err != nil {
		return err
	}
	*alias = recoveryDocumentAliasRef(decoded)
	return nil
}

func (record recoveryDocumentClaimSet) validateShape() error {
	switch record.Kind {
	case "absent":
		if record.Before != nil || record.After != nil || record.Acquires != nil {
			return fmt.Errorf("absent document claims retain phase evidence")
		}
	case "exact":
		if record.Before == nil || record.After == nil || record.Acquires != nil {
			return fmt.Errorf("exact document claims require only before and after arrays")
		}
		if len(*record.Before) == 0 && len(*record.After) == 0 {
			return fmt.Errorf("exact document claims require a nonempty phase")
		}
		if err := validateRecoveryOwnershipWorkBudget(len(*record.Before), len(*record.After), 0); err != nil {
			return err
		}
	case "provisional":
		if record.Before != nil || record.After != nil || record.Acquires == nil || len(*record.Acquires) == 0 {
			return fmt.Errorf("provisional document claims require only a nonempty acquire array")
		}
		if err := validateRecoveryOwnershipWorkBudget(0, 0, len(*record.Acquires)); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported document claim variant %q", record.Kind)
	}
	return nil
}

func persistedDocumentClaimTransition(transition ownershipmutation.DocumentClaimTransition, operationID string) (recoveryDocumentClaimSet, error) {
	if err := ownership.ValidateOperationID(operationID); err != nil {
		return recoveryDocumentClaimSet{}, err
	}
	if err := validateRecoveryOwnershipWorkBudget(len(transition.Before()), len(transition.After()), 0); err != nil {
		return recoveryDocumentClaimSet{}, err
	}
	canonical, err := ownershipmutation.NewDocumentClaimTransition(transition.Before(), transition.After(), operationID)
	if err != nil {
		return recoveryDocumentClaimSet{}, err
	}
	if !slices.EqualFunc(canonical.Prepared(), transition.Prepared(), ownership.Claim.Equal) {
		return recoveryDocumentClaimSet{}, fmt.Errorf("document claim preparation differs from the operation identity")
	}
	before, err := persistedDocumentClaims(canonical.Before())
	if err != nil {
		return recoveryDocumentClaimSet{}, err
	}
	after, err := persistedDocumentClaims(canonical.After())
	if err != nil {
		return recoveryDocumentClaimSet{}, err
	}
	record := recoveryDocumentClaimSet{Kind: "exact", Before: &before, After: &after}
	if err := record.validateShape(); err != nil {
		return recoveryDocumentClaimSet{}, err
	}
	return record, nil
}

func persistedDocumentClaims(claims []ownership.Claim) ([]recoveryClaimValue, error) {
	records := make([]recoveryClaimValue, 0, len(claims))
	for _, claim := range claims {
		value, err := ownership.PresentClaim(claim)
		if err != nil {
			return nil, err
		}
		record, err := recoveryClaimValueFrom(value)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func (record recoveryDocumentClaimSet) canonicalExact(owner stateauthority.Authority, operationID string, path pathauthority.Exact) (ownershipmutation.DocumentClaimTransition, error) {
	if err := record.validateShape(); err != nil {
		return ownershipmutation.DocumentClaimTransition{}, err
	}
	if record.Kind != "exact" {
		return ownershipmutation.DocumentClaimTransition{}, fmt.Errorf("exact document claims are required")
	}
	if err := owner.Validate(); err != nil {
		return ownershipmutation.DocumentClaimTransition{}, err
	}
	if err := ownership.ValidateOperationID(operationID); err != nil {
		return ownershipmutation.DocumentClaimTransition{}, err
	}
	if err := path.Validate(); err != nil {
		return ownershipmutation.DocumentClaimTransition{}, err
	}
	before, err := canonicalDocumentClaims(*record.Before, owner, path)
	if err != nil {
		return ownershipmutation.DocumentClaimTransition{}, err
	}
	after, err := canonicalDocumentClaims(*record.After, owner, path)
	if err != nil {
		return ownershipmutation.DocumentClaimTransition{}, err
	}
	return ownershipmutation.NewDocumentClaimTransition(before, after, operationID)
}

func (record recoveryDocumentClaimSet) canonicalObservedExact(owner stateauthority.Authority, operationID string, observed recoveryDocumentObservation) (ownershipmutation.DocumentClaimTransition, error) {
	if err := record.validateShape(); err != nil {
		return ownershipmutation.DocumentClaimTransition{}, err
	}
	if record.Kind != "exact" {
		return ownershipmutation.DocumentClaimTransition{}, fmt.Errorf("exact document claims are required")
	}
	if err := observed.validate(); err != nil {
		return ownershipmutation.DocumentClaimTransition{}, err
	}
	if observed.Kind == "exact" {
		exact, err := canonicalPathAuthority(*observed.Authority)
		if err != nil {
			return ownershipmutation.DocumentClaimTransition{}, err
		}
		return record.canonicalExact(owner, operationID, exact)
	}

	anchor := *record.Before
	historical := len(anchor) != 0
	if !historical {
		anchor = *record.After
	}
	value, err := canonicalClaimValue(anchor[0])
	if err != nil {
		return ownershipmutation.DocumentClaimTransition{}, err
	}
	claim, present := value.Get()
	if !present {
		return ownershipmutation.DocumentClaimTransition{}, fmt.Errorf("exact document claims require a present namespace anchor")
	}
	exact := claim.Address().PathAuthority()
	if historical {
		if !observed.matchesRetainedExact(exact) {
			return ownershipmutation.DocumentClaimTransition{}, fmt.Errorf("historical document claims differ from the missing namespace")
		}
	} else {
		provisional, err := observed.provisional()
		if err != nil || provisional.AdmitsExact(exact) != nil {
			return ownershipmutation.DocumentClaimTransition{}, fmt.Errorf("promoted document claims are outside the captured namespace")
		}
	}
	return record.canonicalExact(owner, operationID, exact)
}

func canonicalDocumentClaims(records []recoveryClaimValue, owner stateauthority.Authority, path pathauthority.Exact) ([]ownership.Claim, error) {
	claims := make([]ownership.Claim, 0, len(records))
	for _, record := range records {
		value, err := canonicalClaimValue(record)
		if err != nil {
			return nil, err
		}
		claim, present := value.Get()
		if !present || claim.State() != ownership.ClaimActive || !claim.Owner().Equal(owner) || !claim.Address().PathAuthority().Equal(path) {
			return nil, fmt.Errorf("document claim phase differs from its active owner or physical document")
		}
		claims = append(claims, claim)
	}
	return claims, nil
}

func persistedDocumentProvisionalClaims(acquires []journaldocument.ProvisionalAcquire) (recoveryDocumentClaimSet, error) {
	if err := validateRecoveryOwnershipWorkBudget(0, 0, len(acquires)); err != nil {
		return recoveryDocumentClaimSet{}, err
	}
	acquires = slices.Clone(acquires)
	slices.SortFunc(acquires, func(left, right journaldocument.ProvisionalAcquire) int {
		return output.CompareFootprints(left.Intent().Footprint(), right.Intent().Footprint())
	})
	records := make([]recoveryDocumentProvisionalAcquire, 0, len(acquires))
	aliases := make([]aggregate.DocumentAddress, 0)
	seenAliases := make(map[aggregate.DocumentAddress]bool)
	for _, acquire := range acquires {
		if err := acquire.Alias().Validate(); err != nil {
			return recoveryDocumentClaimSet{}, err
		}
		intent := acquire.Intent()
		if err := intent.Validate(); err != nil {
			return recoveryDocumentClaimSet{}, err
		}
		if acquire.Alias().Scope() != target.ScopeGlobal || acquire.Alias().AggregateRoot() != intent.Destination() {
			return recoveryDocumentClaimSet{}, fmt.Errorf("provisional document claim alias differs from its destination")
		}
		if !seenAliases[acquire.Alias()] {
			seenAliases[acquire.Alias()] = true
			aliases = append(aliases, acquire.Alias())
		}
		footprint, err := footprintcodec.Encode(intent.Footprint())
		if err != nil {
			return recoveryDocumentClaimSet{}, err
		}
		path := intent.Path()
		records = append(records, recoveryDocumentProvisionalAcquire{
			Alias: persistedDocumentAlias(acquire.Alias()), Footprint: footprint,
			CandidateAuthority: pathAuthorityDTO{Key: path.CandidateKey(), Witness: path.CandidateWitness()},
			NamespaceAuthority: *persistedPathAuthority(path.Namespace()),
			StatefileAuthority: *persistedPathAuthority(intent.Owner().StatefileAuthority()),
			ManifestPath:       intent.Owner().ManifestPath(), OperationID: intent.OperationID(),
		})
	}
	record := recoveryDocumentClaimSet{Kind: "provisional", Acquires: &records}
	if err := record.validateShape(); err != nil {
		return recoveryDocumentClaimSet{}, err
	}
	first := acquires[0].Intent()
	if _, err := record.canonicalProvisional(aliases, first.Owner(), first.OperationID(), first.Path()); err != nil {
		return recoveryDocumentClaimSet{}, err
	}
	return record, nil
}

func persistedDocumentAlias(alias aggregate.DocumentAddress) recoveryDocumentAliasRef {
	return recoveryDocumentAliasRef{Target: string(alias.Target()), Scope: string(alias.Scope()), Destination: alias.AggregateRoot().String()}
}

func (record recoveryDocumentClaimSet) canonicalProvisional(aliases []aggregate.DocumentAddress, owner stateauthority.Authority, operationID string, path pathauthority.Provisional) ([]journaldocument.ProvisionalAcquire, error) {
	if err := record.validateShape(); err != nil {
		return nil, err
	}
	if record.Kind != "provisional" {
		return nil, fmt.Errorf("provisional document claims are required")
	}
	if err := owner.Validate(); err != nil {
		return nil, err
	}
	if err := ownership.ValidateOperationID(operationID); err != nil {
		return nil, err
	}
	if err := path.Validate(); err != nil {
		return nil, err
	}
	if err := validateRecoveryOwnershipWorkBudget(len(aliases), 0, len(*record.Acquires)); err != nil {
		return nil, err
	}
	byAlias := make(map[recoveryDocumentAliasRef]aggregate.DocumentAddress, len(aliases))
	for _, alias := range aliases {
		if err := alias.Validate(); err != nil {
			return nil, err
		}
		key := persistedDocumentAlias(alias)
		if _, present := byAlias[key]; present {
			return nil, fmt.Errorf("document claim aliases are duplicated")
		}
		byAlias[key] = alias
	}
	acquires := make([]journaldocument.ProvisionalAcquire, 0, len(*record.Acquires))
	footprints := make([]output.Footprint, 0, len(*record.Acquires))
	for _, row := range *record.Acquires {
		alias, found := byAlias[row.Alias]
		if !found || alias.Scope() != target.ScopeGlobal {
			return nil, fmt.Errorf("provisional claim does not reference a declared global document alias")
		}
		footprint, err := footprintcodec.Decode(row.Footprint)
		if err != nil {
			return nil, err
		}
		candidate, err := pathauthority.NewProvisional(row.CandidateAuthority.Key, row.CandidateAuthority.Witness, row.NamespaceAuthority.Key, row.NamespaceAuthority.Witness)
		if err != nil || !candidate.Equal(path) {
			return nil, fmt.Errorf("provisional claim differs from the captured document namespace")
		}
		statefile, err := canonicalPathAuthority(row.StatefileAuthority)
		if err != nil {
			return nil, err
		}
		claimOwner, err := stateauthority.New(statefile, row.ManifestPath)
		if err != nil || !claimOwner.Equal(owner) || row.OperationID != operationID {
			return nil, fmt.Errorf("provisional claim differs from the operation owner or identity")
		}
		intent, err := ownership.NewProvisionalAcquireIntent(alias.AggregateRoot(), footprint, candidate, claimOwner, row.OperationID)
		if err != nil {
			return nil, err
		}
		acquires = append(acquires, journaldocument.NewProvisionalAcquire(alias, intent))
		footprints = append(footprints, footprint)
	}
	if err := validateNonOverlappingRecoveryFootprints(footprints); err != nil {
		return nil, err
	}
	return acquires, nil
}

func (record recoveryDocumentClaimSet) promoted(aliases []aggregate.DocumentAddress, owner stateauthority.Authority, operationID string, provisional pathauthority.Provisional, exact pathauthority.Exact) (recoveryDocumentClaimSet, error) {
	acquires, err := record.canonicalProvisional(aliases, owner, operationID, provisional)
	if err != nil {
		return recoveryDocumentClaimSet{}, err
	}
	after := make([]ownership.Claim, 0, len(acquires))
	for _, acquire := range acquires {
		address, err := ownership.NewManagedAddress(exact, acquire.Intent().Footprint())
		if err != nil {
			return recoveryDocumentClaimSet{}, err
		}
		transition, err := ownershipmutation.NewAcquireTransitionFromIntent(acquire.Intent(), address)
		if err != nil {
			return recoveryDocumentClaimSet{}, err
		}
		claim, _ := transition.After().Get()
		after = append(after, claim)
	}
	transition, err := ownershipmutation.NewDocumentClaimTransition(nil, after, operationID)
	if err != nil {
		return recoveryDocumentClaimSet{}, err
	}
	return persistedDocumentClaimTransition(transition, operationID)
}
