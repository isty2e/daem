//go:build !darwin

package mutation

import "github.com/isty2e/daem/internal/effect/mutation/rootedpath"

func reservePlatformIdentityWork(_ string, _ PathEffect, _ int, _ rootedpath.PhysicalTraversalBudget) error {
	return nil
}
