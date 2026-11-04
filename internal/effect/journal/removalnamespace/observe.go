package removalnamespace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

func removalObservationCancellation(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return err
	case errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return nil
	}
}

// Observe compares fresh parent facts with the exact durable namespace
// relation. It performs no slot observation or mutation.
func Observe(
	ctx context.Context,
	root *rootedpath.CapturedRoot,
	destination rootedpath.Destination,
	namespace recovery.RemovalNamespaceAuthority,
	budget *physicalwork.Budget,
) (recovery.RemovalNamespaceObservation, error) {
	if err := ctx.Err(); err != nil {
		return recovery.RemovalNamespaceObservation{}, err
	}
	current, err := root.AuthorityBounded(budget)
	if cancellationErr := removalNamespaceCancellation(ctx, err); cancellationErr != nil {
		return recovery.RemovalNamespaceObservation{}, cancellationErr
	}
	if err != nil {
		if !isRootedRemovalAuthorityFailure(err) {
			return recovery.RemovalNamespaceObservation{}, err
		}
		status, detail := classifyRemovalRootObservationFailure(err)
		return newRemovalNamespaceObservation(status, detail)
	}
	if !current.Equal(destination.Root()) {
		return newRemovalNamespaceObservation(
			recovery.RemovalNamespaceChanged,
			"destination belongs to a different captured root",
		)
	}
	hostPath, err := destination.LexicalPath()
	if err != nil {
		return recovery.RemovalNamespaceObservation{}, err
	}
	switch namespace.Variant() {
	case recovery.RemovalNamespaceExistingParent:
		parent, parentPresent := namespace.ParentProvenance()
		if !parentPresent {
			return recovery.RemovalNamespaceObservation{}, fmt.Errorf("existing-parent removal namespace has no parent provenance")
		}
		parentPath := parent.PhysicalRoot()
		if filepath.Clean(filepath.Dir(filepath.Clean(hostPath))) != filepath.Clean(parentPath) {
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceChanged,
				"destination parent no longer matches captured existing parent",
			)
		}
		persisted, err := rootedpath.NewAuthorityProvenance(
			parent.PhysicalRoot(), parent.ObjectFingerprint(), parent.MountFingerprint(),
		)
		if err != nil {
			return recovery.RemovalNamespaceObservation{}, err
		}
		currentParent, err := observePersistedRemovalRoot(current, persisted, budget)
		if cancellationErr := removalNamespaceCancellation(ctx, err); cancellationErr != nil {
			return recovery.RemovalNamespaceObservation{}, cancellationErr
		}
		if err != nil {
			if !isRootedRemovalAuthorityFailure(err) {
				return recovery.RemovalNamespaceObservation{}, err
			}
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceChanged,
				"captured parent authority no longer matches; unlink and relocation cannot be distinguished",
			)
		}
		if err := persisted.Match(currentParent); err != nil {
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceChanged,
				"captured parent authority no longer matches; unlink and relocation cannot be distinguished",
			)
		}
		return newRemovalNamespaceObservation(recovery.RemovalNamespaceMatched, "")
	case recovery.RemovalNamespaceInitiallyAbsentParent:
		ancestor, present := namespace.RetainedAncestorProvenance()
		if !present {
			return recovery.RemovalNamespaceObservation{}, fmt.Errorf("initially-absent removal namespace has no retained ancestor provenance")
		}
		wantParent := filepath.Join(ancestor.PhysicalRoot(), filepath.FromSlash(namespace.MissingSuffix()))
		parent := filepath.Dir(filepath.Clean(hostPath))
		if filepath.Clean(wantParent) != parent {
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceChanged,
				"destination parent no longer matches captured missing suffix",
			)
		}
		persisted, err := rootedpath.NewAuthorityProvenance(
			ancestor.PhysicalRoot(), ancestor.ObjectFingerprint(), ancestor.MountFingerprint(),
		)
		if err != nil {
			return recovery.RemovalNamespaceObservation{}, err
		}
		currentAncestor, err := observePersistedRemovalRoot(current, persisted, budget)
		if cancellationErr := removalNamespaceCancellation(ctx, err); cancellationErr != nil {
			return recovery.RemovalNamespaceObservation{}, cancellationErr
		}
		if err != nil {
			if !isRootedRemovalAuthorityFailure(err) {
				return recovery.RemovalNamespaceObservation{}, err
			}
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceChanged,
				"retained ancestor authority no longer matches",
			)
		}
		if err := persisted.Match(currentAncestor); err != nil {
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceChanged,
				"retained ancestor authority no longer matches",
			)
		}
		if err := ctx.Err(); err != nil {
			return recovery.RemovalNamespaceObservation{}, err
		}
		if current.PhysicalRoot() == filepath.Clean(parent) {
			return newRemovalNamespaceObservation(recovery.RemovalNamespaceMatched, "")
		}
		parentRoot, err := rootedpath.CaptureCanonicalRootNoFollowBounded(
			parent,
			physicalwork.MaximumPhysicalPathDepth,
			budget,
		)
		if cancellationErr := removalNamespaceCancellation(ctx, err); cancellationErr != nil {
			return recovery.RemovalNamespaceObservation{}, cancellationErr
		}
		if errors.Is(err, fs.ErrNotExist) {
			return newRemovalNamespaceObservation(recovery.RemovalNamespaceMatched, "")
		}
		if err != nil {
			var failure *rootedpath.Failure
			if errors.As(err, &failure) && failure.Kind() == rootedpath.FailureRootReplaced {
				return newRemovalNamespaceObservation(
					recovery.RemovalNamespaceChanged,
					"initially absent parent is not a directory",
				)
			}
			if !errors.As(err, &failure) {
				return recovery.RemovalNamespaceObservation{}, err
			}
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceUnavailable,
				"initially absent parent authority could not be captured",
			)
		}
		defer parentRoot.Close()
		currentParent, err := parentRoot.AuthorityBounded(budget)
		if cancellationErr := removalNamespaceCancellation(ctx, err); cancellationErr != nil {
			return recovery.RemovalNamespaceObservation{}, cancellationErr
		}
		if err != nil {
			if !isRootedRemovalAuthorityFailure(err) {
				return recovery.RemovalNamespaceObservation{}, err
			}
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceUnavailable,
				"initially absent parent authority could not be read",
			)
		}
		if err := persisted.MatchDescendant(currentParent); err != nil {
			if cancellationErr := removalNamespaceCancellation(ctx, err); cancellationErr != nil {
				return recovery.RemovalNamespaceObservation{}, cancellationErr
			}
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceChanged,
				"initially absent parent authority no longer matches retained ancestor",
			)
		}
		if err := ctx.Err(); err != nil {
			return recovery.RemovalNamespaceObservation{}, err
		}
		return newRemovalNamespaceObservation(recovery.RemovalNamespaceMatched, "")
	default:
		return recovery.RemovalNamespaceObservation{}, fmt.Errorf("unsupported removal namespace variant %q", namespace.Variant())
	}
}

func isRootedRemovalAuthorityFailure(err error) bool {
	var failure *rootedpath.Failure
	return errors.As(err, &failure)
}

func observePersistedRemovalRoot(
	bound rootedpath.Authority,
	persisted rootedpath.AuthorityProvenance,
	budget *physicalwork.Budget,
) (rootedpath.Authority, error) {
	if filepath.Clean(bound.PhysicalRoot()) == filepath.Clean(persisted.PhysicalRoot()) {
		return bound, nil
	}
	root, err := rootedpath.CaptureCanonicalRootNoFollowBounded(
		persisted.PhysicalRoot(),
		physicalwork.MaximumPhysicalPathDepth,
		budget,
	)
	if err != nil {
		return rootedpath.Authority{}, err
	}
	defer root.Close()
	return root.AuthorityBounded(budget)
}

func classifyRemovalRootObservationFailure(
	err error,
) (recovery.RemovalNamespaceObservationStatus, string) {
	var failure *rootedpath.Failure
	if errors.As(err, &failure) &&
		(failure.Kind() == rootedpath.FailureRootReplaced ||
			failure.Kind() == rootedpath.FailureMountChanged) {
		return recovery.RemovalNamespaceChanged, "captured destination authority no longer matches"
	}
	return recovery.RemovalNamespaceUnavailable, "captured destination authority could not be observed"
}

func removalNamespaceCancellation(ctx context.Context, err error) error {
	if cancellationErr := removalObservationCancellation(err); cancellationErr != nil {
		return cancellationErr
	}
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func newRemovalNamespaceObservation(
	status recovery.RemovalNamespaceObservationStatus,
	detail string,
) (recovery.RemovalNamespaceObservation, error) {
	return recovery.NewRemovalNamespaceObservation(status, detail)
}
