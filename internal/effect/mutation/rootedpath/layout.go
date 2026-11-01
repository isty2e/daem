package rootedpath

import (
	"fmt"
	"path/filepath"
)

type layoutAnchor struct {
	path   string
	object identityToken
	mount  identityToken
}

type layoutAlias struct {
	parent layoutAnchor
	name   string
	target string
}

// PathObservationBudget includes bounded alias reads in the caller allocation.
type PathObservationBudget interface {
	PhysicalTraversalBudget
	AdmitPhysicalWork(pathComponents int, entries int, bytes int64) error
}

// PathLayout retains read-only namespace facts, not a live mutation grant.
// Subsequent observations require the prepared parent/alias layout; the final
// regular file may be replaced without retaining its old inode.
type PathLayout struct {
	path     string
	anchor   layoutAnchor
	aliases  []layoutAlias
	referent bool
}

func CaptureDirectoryEntryLayoutBounded(path string, maximumPhysicalDepth int, budget PhysicalTraversalBudget) (PathLayout, error) {
	return capturePathLayoutPlatform(path, false, maximumPhysicalDepth, budget)
}

func CaptureReferentLayoutBounded(path string, maximumPhysicalDepth int, budget PhysicalTraversalBudget) (PathLayout, error) {
	return capturePathLayoutPlatform(path, true, maximumPhysicalDepth, budget)
}

// CanonicalPath is a prepared access spelling, not evidence of current authority.
func (layout PathLayout) CanonicalPath() string { return layout.path }

// ResolveBounded checks recorded aliases without following a newly introduced
// target, then observes the prepared canonical path without following aliases.
func (layout PathLayout) ResolveBounded(maximumPhysicalDepth int, budget PathObservationBudget) (string, []string, error) {
	if err := layout.validate(); err != nil {
		return "", nil, err
	}
	return resolvePathLayoutPlatform(layout, maximumPhysicalDepth, budget)
}

// CaptureDestinationBounded validates the prepared layout, then captures a
// fresh destination without resolving any newly introduced alias.
func (layout PathLayout) CaptureDestinationBounded(maximumPhysicalDepth int, budget PathObservationBudget) (*CapturedRoot, Destination, error) {
	if _, _, err := layout.ResolveBounded(maximumPhysicalDepth, budget); err != nil {
		return nil, Destination{}, err
	}
	traversal, err := newPhysicalTraversal(maximumPhysicalDepth, budget)
	if err != nil {
		return nil, Destination{}, err
	}
	traversal.rejectAliases = true
	return captureCanonicalDestinationBounded(layout.path, traversal)
}

// ReserveDestinationCapture projects validation plus fresh destination capture.
func (layout PathLayout) ReserveDestinationCapture(maximumPhysicalDepth int, budget PathObservationBudget) error {
	if err := layout.ReserveObservation(maximumPhysicalDepth, budget); err != nil {
		return err
	}
	return ChargeAbsolutePath(filepath.Dir(layout.path), maximumPhysicalDepth, budget)
}

// ReserveDestinationAccess bounds acquisition plus one rooted operation even
// when planned missing parents become directories before that acquisition.
func (layout PathLayout) ReserveDestinationAccess(maximumPhysicalDepth int, budget PathObservationBudget) error {
	if err := layout.validate(); err != nil {
		return err
	}
	for range 2 {
		if err := ChargeAbsolutePath(filepath.Dir(layout.path), maximumPhysicalDepth, budget); err != nil {
			return err
		}
	}
	return ChargeAbsolutePath(layout.path, maximumPhysicalDepth, budget)
}

// ReserveObservation projects the complete validation and no-follow resolution
// from immutable layout facts. It performs no filesystem I/O.
func (layout PathLayout) ReserveObservation(maximumPhysicalDepth int, budget PathObservationBudget) error {
	if err := layout.validate(); err != nil {
		return err
	}
	if err := ChargeAbsolutePath(layout.anchor.path, maximumPhysicalDepth, budget); err != nil {
		return err
	}
	for _, alias := range layout.aliases {
		if err := ChargeAbsolutePath(alias.parent.path, maximumPhysicalDepth, budget); err != nil {
			return err
		}
		if err := budget.AdmitPathComponents(1); err != nil {
			return err
		}
		if err := admitLayoutAliasRead(budget, alias); err != nil {
			return err
		}
	}
	path := layout.path
	if !layout.referent {
		path = filepath.Dir(path)
	}
	return ChargeAbsolutePath(path, maximumPhysicalDepth, budget)
}

func (layout PathLayout) validate() error {
	if layout.path == "" || layout.anchor.path == "" || layout.anchor.object == (identityToken{}) || layout.anchor.mount == (identityToken{}) {
		return fmt.Errorf("prepared path layout is unavailable")
	}
	return nil
}

func admitLayoutAliasRead(budget physicalWorkBudget, alias layoutAlias) error {
	return budget.AdmitPhysicalWork(0, 1, int64(len(alias.target))+1)
}
