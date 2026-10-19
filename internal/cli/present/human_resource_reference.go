package clipresent

import (
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	lock "github.com/isty2e/daem/internal/realization/lock"
)

type humanResourceReference struct {
	resource string
	subject  string
}

func (reference humanResourceReference) fields(verbose bool) string {
	if reference.resource == "" {
		return "subject=" + Quote(reference.subject)
	}
	fields := "resource=" + Quote(reference.resource)
	if verbose {
		fields += " subject=" + Quote(reference.subject)
	}
	return fields
}

func carrierHumanReference(identity durablecarrier.ManagedCarrierIdentity) humanResourceReference {
	disclosure := carrierIdentityDisclosureFor(identity)
	projected := disclosure.relationSubject
	if projected == nil {
		return humanResourceReference{subject: identityDisclosureFor(identity.RelationSubject().String()).Value()}
	}
	reference := humanResourceReference{subject: lockSubjectString(jsonSubjectID{
		Kind: projected.Kind, Namespace: projected.Namespace, Name: projected.Name,
	})}
	if projected.NameRedacted {
		return reference
	}
	contract, err := lock.ReconstructDelegatedRelationCarrierContract(
		identity.Carrier().Key(), identity.RelationSubject(), identity.ExpectedRelation().SubjectKey(),
	)
	if err == nil {
		id := contract.EntityID()
		reference.resource = ResourceReference(id.Kind(), id.Name())
	}
	return reference
}

func lockHumanReference(
	entry lock.DeltaEntry,
	projection lockIdentityProjection,
	verbose bool,
) humanResourceReference {
	contract, side := lockDeltaContract(entry)
	subject := projection.subject(entry.Key, side, verbose)
	reference := humanResourceReference{subject: lockSubjectString(subject)}
	if !subject.NameRedacted {
		id := contract.EntityID()
		if id.Kind() != "" {
			reference.resource = ResourceReference(id.Kind(), id.Name())
		}
	}
	return reference
}

func lockDeltaContract(entry lock.DeltaEntry) (lock.LockedSubjectContract, lockIdentitySide) {
	if entry.Status == lock.DeltaStatusRemoved {
		return entry.Before, lockIdentityBefore
	}
	return entry.After, lockIdentityAfter
}
