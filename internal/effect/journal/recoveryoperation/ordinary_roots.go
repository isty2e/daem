package recoveryoperation

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/effect/journal/qualification"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

type ordinaryRootBinding struct {
	root        *rootedpath.CapturedRoot
	destination rootedpath.Destination
}

// OpenOrdinaryRoots independently selects destinations before reopening recorded
// roots. The caller owns retained captures, never a supplied resolver's roots.
func OpenOrdinaryRoots(ctx context.Context, entries []journalrecord.PathEntry, resolver func(output.Destination) (string, error), supplied journalnamespace.CapabilityResolver, budget *physicalwork.Budget) (selected journalnamespace.CapabilityResolver, retained []*rootedpath.CapturedRoot, resultErr error) {
	if supplied != nil {
		return supplied, nil, nil
	}
	defer func() {
		if resultErr != nil {
			for _, root := range retained {
				resultErr = errors.Join(resultErr, root.Close())
			}
			selected, retained = nil, nil
		}
	}()
	bindings := make(map[output.Destination]ordinaryRootBinding)
	currentRoots := make(map[string]rootedpath.Authority)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, retained, err
		}
		if err := entry.Validate(); err != nil {
			return nil, retained, err
		}
		if entry.Scope() != target.ScopeGlobal {
			continue
		}
		destination, err := output.Parse(entry.Path())
		if err != nil {
			return nil, retained, err
		}
		if _, present := bindings[destination]; present {
			continue
		}
		if resolver == nil || !entry.HasGlobalBinding() {
			return nil, retained, fmt.Errorf("ordinary global recovery requires independent selection and recorded root evidence")
		}
		observed, err := qualification.ObserveRecoveryGlobalBinding(destination, resolver, nil, budget)
		if err != nil {
			return nil, retained, err
		}
		if err := qualification.MatchRecordedGlobalBinding(entry, observed, currentRoots, budget); err != nil {
			return nil, retained, err
		}
		_, expected, err := entry.GlobalBinding()
		if err != nil {
			return nil, retained, err
		}
		root, err := rootedpath.CaptureCanonicalRootNoFollowBounded(expected.PhysicalRoot(), physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return nil, retained, err
		}
		retained = append(retained, root)
		authority, err := root.AuthorityBounded(budget)
		if err != nil {
			return nil, retained, err
		}
		if err := expected.Match(authority); err != nil {
			return nil, retained, err
		}
		path, err := filepath.Rel(expected.PhysicalRoot(), observed.ResolvedPath())
		if err != nil {
			return nil, retained, err
		}
		relative, err := rootedpath.NewRelativeDestination(filepath.ToSlash(path))
		if err != nil {
			return nil, retained, err
		}
		bound, err := authority.Bind(relative)
		if err != nil {
			return nil, retained, err
		}
		bindings[destination] = ordinaryRootBinding{root: root, destination: bound}
	}
	return func(destination output.Destination, allocation rootedpath.PhysicalTraversalBudget) (rootedpath.CommitCapability, bool, error) {
		binding, present := bindings[destination]
		if !present {
			return nil, false, nil
		}
		if allocation == nil {
			allocation = budget
		}
		capability, err := binding.root.AcquireBounded(binding.destination, physicalwork.MaximumPhysicalPathDepth, allocation)
		return capability, true, err
	}, retained, nil
}
