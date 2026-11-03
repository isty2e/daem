package transaction

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/effect/fileset"
)

type SourceTransactionInput struct {
	ManifestTarget declarationartifact.MutationTarget
	SourceTarget   declarationartifact.MutationTarget
	StateDir       string
	ManifestBytes  []byte
	SourceBytes    []byte
}

type SourceRetirementInput struct {
	ManifestTarget declarationartifact.MutationTarget
	StateDir       string
	ManifestBytes  []byte
}

// CommitSourceTransaction publishes a named local source before the declaration
// commit point. Eligibility, approval and complete leases belong to the caller.
func CommitSourceTransaction(ctx context.Context, input SourceTransactionInput) error {
	if ctx == nil {
		return fmt.Errorf("source transaction context is required")
	}
	if err := input.SourceTarget.Validate(ctx); err != nil {
		return fmt.Errorf("source target: %w", err)
	}
	source, err := fileset.NewFileWrite(input.SourceTarget.Path(), input.SourceBytes)
	if err != nil {
		return fmt.Errorf("source target: %w", err)
	}
	return commitSourceProjection(ctx, input.StateDir, input.ManifestTarget, input.ManifestBytes, []fileset.FileTarget{source})
}

// CommitSourceRetirement removes only the declaration; the old source is not a
// transaction target. The caller owns the same approval and lease obligations.
func CommitSourceRetirement(ctx context.Context, input SourceRetirementInput) error {
	return commitSourceProjection(ctx, input.StateDir, input.ManifestTarget, input.ManifestBytes, nil)
}

func commitSourceProjection(ctx context.Context, stateDir string, manifestTarget declarationartifact.MutationTarget, content []byte, targets []fileset.FileTarget) error {
	if ctx == nil {
		return fmt.Errorf("source transaction context is required")
	}
	if err := manifestTarget.Validate(ctx); err != nil {
		return fmt.Errorf("manifest target: %w", err)
	}
	manifest, err := fileset.NewFileCommitPointWrite(manifestTarget.Path(), content)
	if err != nil {
		return fmt.Errorf("manifest target: %w", err)
	}
	err = fileset.CommitFreshFileSet4WithReference(ctx, fileset.FileSetInput{StateDir: stateDir, Targets: append(targets, manifest)})
	var failure fileset.TargetWriteError
	if errors.As(err, &failure) && failure.Path == manifestTarget.Path() {
		return declarationartifact.ReplacementError{Path: failure.Path, Err: err}
	}
	return err
}
