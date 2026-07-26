package authoring

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/desired"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

type selection struct {
	declaration *desiredextension.Extension
	identity    durablecarrier.ManagedCarrierIdentity
	hasIdentity bool
}

type ownedIdentity struct {
	identity durablecarrier.ManagedCarrierIdentity
}

func selectExtensionManagement(
	environment desired.Environment,
	state durable.Snapshot,
	registry durablecarrier.GlobalCarrierClaims,
	owner durablecarrier.StateAuthority,
	request UnmanageExtensionRequest,
) (selection, error) {
	declaration, hasDeclaration, err := selectDeclaration(environment.Extensions(), request)
	if err != nil {
		return selection{}, err
	}
	facts, err := ownedExtensionFacts(state, registry, owner, request.ID)
	if err != nil {
		return selection{}, err
	}

	if hasDeclaration {
		expected, err := managedIdentityFromDeclaration(declaration)
		if err != nil {
			return selection{}, err
		}
		identities := make([]durablecarrier.ManagedCarrierIdentity, 0, len(facts))
		for _, fact := range facts {
			if !fact.identity.ExactEqual(expected) {
				return selection{}, fmt.Errorf(
					"extension %q declaration conflicts with retained daem management identity",
					request.ID,
				)
			}
			identities = appendExactIdentity(identities, fact.identity)
		}
		selected := selection{declaration: &declaration}
		if len(identities) == 1 {
			selected.identity = identities[0]
			selected.hasIdentity = true
		}
		return selected, nil
	}

	identities := make([]durablecarrier.ManagedCarrierIdentity, 0, len(facts))
	for _, fact := range facts {
		if request.Target != "" && fact.identity.Target() != request.Target {
			continue
		}
		if request.Scope != "" && fact.identity.Scope() != request.Scope {
			continue
		}
		identities = appendExactIdentity(identities, fact.identity)
	}
	switch len(identities) {
	case 0:
		return selection{}, fmt.Errorf("%w: %q", ErrUnmanageExtensionNotFound, request.ID)
	case 1:
		return selection{identity: identities[0], hasIdentity: true}, nil
	default:
		return selection{}, fmt.Errorf(
			"%w: %q; narrow with --target/--scope",
			ErrUnmanageExtensionAmbiguous,
			request.ID,
		)
	}
}

func selectDeclaration(
	extensions []desiredextension.Extension,
	request UnmanageExtensionRequest,
) (desiredextension.Extension, bool, error) {
	sameID := make([]desiredextension.Extension, 0)
	matches := make([]desiredextension.Extension, 0)
	for _, extension := range extensions {
		if extension.ID().Name() != request.ID {
			continue
		}
		sameID = append(sameID, extension)
		if request.Target != "" && extension.Target() != request.Target {
			continue
		}
		if request.Scope != "" && extension.Scope() != request.Scope {
			continue
		}
		matches = append(matches, extension)
	}
	if len(matches) == 1 {
		return matches[0], true, nil
	}
	if len(matches) > 1 {
		return desiredextension.Extension{}, false, fmt.Errorf(
			"%w: declaration %q; narrow with --target/--scope",
			ErrUnmanageExtensionAmbiguous,
			request.ID,
		)
	}
	if len(sameID) != 0 {
		return desiredextension.Extension{}, false, fmt.Errorf(
			"%w: declaration %q does not match the supplied filters",
			ErrUnmanageExtensionNotFound,
			request.ID,
		)
	}
	return desiredextension.Extension{}, false, nil
}

func managedIdentityFromDeclaration(
	declaration desiredextension.Extension,
) (durablecarrier.ManagedCarrierIdentity, error) {
	subject, err := extensiontopology.Relation(declaration)
	if err != nil {
		return durablecarrier.ManagedCarrierIdentity{}, err
	}
	carrier, err := extensiontopology.NewCarrier(declaration.CarrierKey())
	if err != nil {
		return durablecarrier.ManagedCarrierIdentity{}, err
	}
	subjectKey, err := hostrelation.NewSubjectKey(declaration.Source().Ref())
	if err != nil {
		return durablecarrier.ManagedCarrierIdentity{}, err
	}
	relation, err := hostrelation.Derive(
		declaration.CarrierKey(),
		subject,
		subjectKey,
	)
	if err != nil {
		return durablecarrier.ManagedCarrierIdentity{}, err
	}
	return durablecarrier.NewManagedCarrierIdentity(carrier, subject, relation)
}

func ownedExtensionFacts(
	state durable.Snapshot,
	registry durablecarrier.GlobalCarrierClaims,
	owner durablecarrier.StateAuthority,
	id string,
) ([]ownedIdentity, error) {
	facts := make([]ownedIdentity, 0)
	add := func(candidateOwner durablecarrier.StateAuthority, identity durablecarrier.ManagedCarrierIdentity) error {
		if identity.RelationSubject().Key() != id {
			return nil
		}
		if candidateOwner.Equal(owner) && !candidateOwner.ExactEqual(owner) {
			return fmt.Errorf(
				"extension %q management owner provenance conflicts with selected manifest",
				id,
			)
		}
		if !candidateOwner.ExactEqual(owner) {
			return nil
		}
		facts = append(facts, ownedIdentity{identity: identity})
		return nil
	}
	for _, pending := range state.PendingCarrierInstalls() {
		if err := add(pending.Owner(), pending.Identity()); err != nil {
			return nil, err
		}
	}
	for _, pending := range state.PendingCarrierRemovals() {
		if err := add(pending.Owner(), pending.Identity()); err != nil {
			return nil, err
		}
	}
	for _, claim := range state.ManagedCarrierClaims() {
		if err := add(claim.Owner(), claim.Identity()); err != nil {
			return nil, err
		}
	}
	for _, claim := range registry.Claims() {
		if err := add(claim.Owner(), claim.Identity()); err != nil {
			return nil, err
		}
	}
	return facts, nil
}

func appendExactIdentity(
	identities []durablecarrier.ManagedCarrierIdentity,
	candidate durablecarrier.ManagedCarrierIdentity,
) []durablecarrier.ManagedCarrierIdentity {
	for _, existing := range identities {
		if existing.ExactEqual(candidate) {
			return identities
		}
	}
	return append(identities, candidate)
}

func selectedAxes(selected selection) (target.Target, target.Scope) {
	if selected.declaration != nil {
		return selected.declaration.Target(), selected.declaration.Scope()
	}
	return selected.identity.Target(), selected.identity.Scope()
}
