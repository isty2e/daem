package declarationartifact

import (
	"context"
	"fmt"

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
	path      string
	revisions mutation.RevisionSet
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
	path, err := mutation.CanonicalReferentPath(selectedPath)
	if err != nil {
		return MutationTarget{}, err
	}
	target := MutationTarget{path: path, revisions: revisions}
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

// MutationPaths resolves a declaration-only recovery allowlist. Callers must
// lease the selected entry and referent domains before consuming these paths.
func MutationPaths(ctx context.Context, selectedPaths ...string) ([]string, error) {
	targets := make([]MutationTarget, 0, len(selectedPaths))
	paths := make([]string, 0, len(selectedPaths))
	for _, selected := range selectedPaths {
		target, err := CaptureMutationTarget(ctx, selected)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
		paths = append(paths, target.Path())
	}
	for _, target := range targets {
		if err := target.Validate(ctx); err != nil {
			return nil, err
		}
	}
	return paths, nil
}
