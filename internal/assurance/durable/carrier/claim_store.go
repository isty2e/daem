package carrier

import "github.com/isty2e/daem/internal/target"

// ClaimStore identifies custody of a completed carrier claim, not its local pending evidence.
type ClaimStore string

const (
	ClaimStoreProjectStatefile ClaimStore = "project_statefile"
	ClaimStoreGlobalRegistry   ClaimStore = "global_registry"
)

// ClaimStoreForScope selects logical custody without observing availability or granting authority.
func ClaimStoreForScope(scope target.Scope) (ClaimStore, bool) {
	switch scope {
	case target.ScopeProject:
		return ClaimStoreProjectStatefile, true
	case target.ScopeGlobal:
		return ClaimStoreGlobalRegistry, true
	default:
		return "", false
	}
}

// ClaimStore returns no store for the zero identity.
func (identity ManagedCarrierIdentity) ClaimStore() ClaimStore {
	store, _ := ClaimStoreForScope(identity.Scope())
	return store
}
