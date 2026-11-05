package preparation

import "slices"

func (effect ManagedPathEffect) Equal(other ManagedPathEffect) bool {
	a, b := effect.facts(), other.facts()
	if effect.Kind() != other.Kind() || a.subject != b.subject || !slices.Equal(a.consumerTargets, b.consumerTargets) ||
		a.scope != b.scope || a.destination != b.destination || a.desiredHash != b.desiredHash || a.liveHash != b.liveHash ||
		a.contentKind != b.contentKind || a.permissionPolicy != b.permissionPolicy ||
		a.desiredFileMode != b.desiredFileMode || a.liveFileMode != b.liveFileMode {
		return false
	}
	if a.previous == nil || b.previous == nil {
		return a.previous == nil && b.previous == nil
	}
	return a.previous.Equal(*b.previous)
}
