package mutation

import (
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func reservePlatformIdentityWork(path string, effect PathEffect, maximumDepth int, budget rootedpath.PhysicalTraversalBudget) error {
	depth := strings.Count(path, "/")
	if depth <= 0 || depth > maximumDepth {
		return fmt.Errorf("prepared path depth exceeds operation limits")
	}
	if effect == PathEffectDirectoryEntry {
		if err := budget.AdmitPathComponents(depth - 1); err != nil {
			return err
		}
	}
	if err := budget.AdmitPathComponents(depth); err != nil {
		return err
	}
	for parent := range depth {
		if err := budget.AdmitPathComponents(parent); err != nil {
			return err
		}
	}
	return nil
}
