package declarationartifact

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
)

// ReplacementError keeps a declaration's physical write target with its cause.
type ReplacementError struct {
	Path string
	Err  error
}

func (err ReplacementError) Error() string {
	return fmt.Sprintf("cannot replace declaration referent %q: %v", err.Path, err.Err)
}

func (err ReplacementError) Unwrap() error { return err.Err }

// MutationTarget binds a selected declaration address to its observed referent
// location. Logical workspace paths remain owned by the caller.
type MutationTarget struct {
	path           string
	throughSymlink bool
	revisions      mutation.RevisionSet
}

// CaptureMutationTarget observes an existing declaration or an ordinary absent
// entry. A dangling final link is not an absent entry.
func CaptureMutationTarget(ctx context.Context, selectedPath string) (MutationTarget, error) {
	requests, err := mutation.BoundedFileRevisionRequests(MaximumBytes, selectedPath)
	if err != nil {
		return MutationTarget{}, err
	}
	revisions, err := mutation.CaptureRevisionSet(ctx, requests...)
	if err != nil {
		return MutationTarget{}, err
	}
	path, throughSymlink, err := mutation.CanonicalReferentPath(selectedPath)
	if err != nil {
		return MutationTarget{}, err
	}
	target := MutationTarget{path: path, throughSymlink: throughSymlink, revisions: revisions}
	if err := target.Validate(ctx); err != nil {
		return MutationTarget{}, err
	}
	return target, nil
}

// Path returns the bound physical replacement entry, not a workspace root.
func (target MutationTarget) Path() string { return target.path }

// Validate refuses changed selected entries or referents before an effect.
func (target MutationTarget) Validate(ctx context.Context) error {
	if target.path == "" {
		return fmt.Errorf("declaration mutation target is uninitialized")
	}
	matches, err := target.revisions.MatchesCurrent(ctx)
	if err != nil {
		return err
	}
	if !matches {
		return mutation.StaleSnapshotError{}
	}
	return nil
}

// LinkedRecoveryError retains a recovery refusal observed through declaration
// links. Link traversal does not establish that a link change caused refusal.
type LinkedRecoveryError struct{ Err error }

func (err LinkedRecoveryError) Error() string { return err.Err.Error() }
func (err LinkedRecoveryError) Unwrap() error { return err.Err }

// RecoveryTargets retains physical paths and their observed link context.
type RecoveryTargets struct {
	paths          []string
	throughSymlink bool
}

func (targets RecoveryTargets) Paths() []string { return append([]string(nil), targets.paths...) }

func (targets RecoveryTargets) WrapError(err error) error {
	var authority fileset.RecoveryAuthorityError
	if targets.throughSymlink && errors.As(err, &authority) {
		return LinkedRecoveryError{Err: err}
	}
	return err
}

// CaptureRecoveryTargets resolves a declaration-only recovery allowlist. Callers
// must lease selected entry and referent domains before consuming these paths.
func CaptureRecoveryTargets(ctx context.Context, selectedPaths ...string) (RecoveryTargets, error) {
	targets := make([]MutationTarget, 0, len(selectedPaths))
	result := RecoveryTargets{paths: make([]string, 0, len(selectedPaths))}
	for _, selected := range selectedPaths {
		target, err := CaptureMutationTarget(ctx, selected)
		if err != nil {
			return RecoveryTargets{}, err
		}
		targets = append(targets, target)
		result.paths = append(result.paths, target.Path())
		result.throughSymlink = result.throughSymlink || target.throughSymlink
	}
	for _, target := range targets {
		if err := target.Validate(ctx); err != nil {
			return RecoveryTargets{}, err
		}
	}
	return result, nil
}
