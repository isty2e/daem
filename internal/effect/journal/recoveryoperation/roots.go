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
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

// Profile selects recovery readers and permitted root paths independently of
// persisted selectors. It contains no retained root or execution lease.
type Profile struct {
	StateCodec   durable.SnapshotCodec
	Codecs       aggregate.CodecCatalog
	Limits       configdocument.Limits
	Manifest     string
	ManifestPath string
	ResolveRoot  func(output.RootReference) (string, error)
	ResolvePoint func(output.RootReference, configuration.PointAlternative) (string, error)
	Protection   journalnamespace.ReservationSelectors
	Budget       *physicalwork.Budget
	StateReader  durable.SnapshotReader
}

type OwnedRoots struct {
	selection journalnamespace.Roots
	retained  []io.Closer
	closed    bool
}

func (profile Profile) OpenRoots(ctx context.Context, journal journalrecord.Native) (result *OwnedRoots, resultErr error) {
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
		var path string
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
		if reference == manifestReference {
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
