package reconcile

// RelationActionBasis identifies the canonical facts from which a relation action was derived.
type RelationActionBasis string

const (
	ActionBasisLockedRelation RelationActionBasis = "locked_relation"
)

// Basis returns the canonical fact family from which this action was derived.
func (action RelationAction) Basis() RelationActionBasis {
	return action.basis
}

// EvidenceSource returns the evidence class consumed by this action.
func (action RelationAction) EvidenceSource() string {
	return "passive_relation_inventory"
}

// ReplayBoundary states the strongest replay claim carried by this action.
func (action RelationAction) ReplayBoundary() string {
	return "locked_route_request_identity_only"
}
