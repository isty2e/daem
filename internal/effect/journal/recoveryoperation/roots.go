package recoveryoperation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

// Profile selects current roots independently of recorded selectors. Historical
// owned points additionally require raw persistence corroboration before capture.
// It contains no retained root or execution lease.
type Profile struct {
	StateCodec     durable.SnapshotCodec
	StatefileCodec statefile.PhysicalCodec
	Codecs         aggregate.CodecCatalog
	Limits         configdocument.Limits
	Manifest       string
	ManifestPath   string
	ResolveRoot    func(output.RootReference) (string, error)
	ResolvePoint   func(output.RootReference, configuration.PointAlternative) (string, error)
	Protection     journalnamespace.ReservationSelectors
	Budget         *physicalwork.Budget
	StateReader    durable.SnapshotReader
	RawRegistry    func(context.Context) (ownership.Registry, error)
}

type OwnedRoots struct {
	selection journalnamespace.Roots
	retained  []io.Closer
	closed    bool
}

var ErrOwnedSourceSelectionRequired = errors.New("owned source recovery requires phase-qualified historical endpoint selection")

func (profile Profile) OpenRoots(ctx context.Context, journal journalrecord.Native) (result *OwnedRoots, resultErr error) {
	return profile.openRoots(ctx, journal, nil, journalrecord.LoadedDocuments{}, ownership.Registry{}, nil)
}

func (profile Profile) openRoots(ctx context.Context, journal journalrecord.Native, demand *journalqualification.RecoveryRootDemand, records journalrecord.LoadedDocuments, registry ownership.Registry, resolver func(output.Destination) (string, error)) (result *OwnedRoots, resultErr error) {
	if ctx == nil {
		return nil, fmt.Errorf("native recovery context is required")
	}
	owned := &OwnedRoots{}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, owned.Close())
			result = nil
		}
	}()
	anchors, err := journal.RecordedRootAnchors()
	if err != nil {
		return nil, err
	}
	manifestReference, err := journal.ManifestRootReference()
	if err != nil {
		return nil, err
	}
	references := slices.Collect(maps.Keys(anchors))
	slices.SortFunc(references, output.RootReference.Compare)
	for _, reference := range references {
		if demand != nil {
			required, err := demand.RequiresRoot(journal, reference)
			if err != nil {
				return nil, err
			}
			if !required {
				continue
			}
		}
		var path string
		var expected pathauthority.Exact
		ownedPoint := configuration.NativeRootIsOwnedPoint(reference)
		if ownedPoint {
			if demand == nil {
				return nil, ErrOwnedSourceSelectionRequired
			}
			path, expected, err = selectOwnedSource(ctx, journal, records, registry, resolver, demand.CommittedAfter())
			if err != nil {
				return nil, err
			}
			path, err = mutation.CanonicalDirectoryEntryPathBounded(path, physicalwork.MaximumPhysicalPathDepth, profile.Budget)
			if err != nil {
				return nil, err
			}
		}
		alternative, point := journal.RecordedPointAlternative(reference)
		if point {
			if profile.ResolvePoint == nil {
				return nil, fmt.Errorf("point recovery requires an independently selected recorded alternative")
			}
			path, err = profile.ResolvePoint(reference, alternative)
			if err != nil {
				return nil, err
			}
		}
		if reference == manifestReference {
			path = profile.Manifest
		}
		if path == "" && !point && profile.ResolveRoot != nil {
			var err error
			path, err = profile.ResolveRoot(reference)
			if err != nil {
				return nil, err
			}
		}
		if path == "" || !filepath.IsAbs(path) {
			return nil, fmt.Errorf("native recovery requires an absolute independently selected root")
		}
		var selection journalnamespace.RootSelection
		var err error
		if ownedPoint {
			selection, err = journalnamespace.CaptureRecordedOwnedEndpointRoot(ctx, path, expected, anchors[reference], profile.Budget)
		} else if reference == manifestReference {
			var root *rootedpath.CapturedRoot
			root, err = rootedpath.CaptureCanonicalRootBounded(path, physicalwork.MaximumPhysicalPathDepth, profile.Budget)
			selection = journalnamespace.RootSelection{SelectedPath: path, Root: root}
		} else if point {
			selection, err = journalnamespace.CaptureRecordedEndpointRoot(ctx, path, alternative, anchors[reference], profile.Budget)
		} else {
			selection, err = journalnamespace.CaptureRecordedRoot(ctx, path, anchors[reference], profile.Budget)
		}
		if err != nil {
			return nil, err
		}
		owned.retained = append(owned.retained, selection)
		owned.selection, err = owned.selection.With(reference, selection)
		if err != nil {
			return nil, err
		}
	}
	return owned, ctx.Err()
}

func (owned *OwnedRoots) Selection() journalnamespace.Roots {
	if owned == nil || owned.closed {
		return journalnamespace.Roots{}
	}
	return owned.selection
}

func (owned *OwnedRoots) Close() error {
	if owned == nil || owned.closed {
		return nil
	}
	owned.closed = true
	var err error
	for _, root := range owned.retained {
		err = errors.Join(err, root.Close())
	}
	return err
}
