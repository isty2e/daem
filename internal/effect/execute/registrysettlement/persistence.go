package registrysettlement

import (
	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/output/ownership"
)

// PersistenceSnapshot retains loaded facts, not freshness or execution authority.
type PersistenceSnapshot struct {
	state     durable.Snapshot
	scope     ownership.DocumentScope
	documents ownership.DocumentRegistryView
}

func NewPersistenceSnapshot(state durable.Snapshot, scope ownership.DocumentScope, registry ownership.Registry) (PersistenceSnapshot, error) {
	documents, err := registry.Documents(scope)
	if err != nil {
		return PersistenceSnapshot{}, err
	}
	return PersistenceSnapshot{state: state, scope: scope, documents: documents}, nil
}

func (snapshot PersistenceSnapshot) Matches(state durable.Snapshot, registry ownership.Registry) (bool, error) {
	if !snapshot.state.Equal(state) {
		return false, nil
	}
	return snapshot.MatchesRegistry(registry)
}

// MatchesRegistry permits actor-owned diagnostics independent of state changes.
func (snapshot PersistenceSnapshot) MatchesRegistry(registry ownership.Registry) (bool, error) {
	documents, err := registry.Documents(snapshot.scope)
	return err == nil && snapshot.documents.Equal(documents), err
}
