package transaction

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/effect/fileset"
)

// TransactionInput is the declaration-owned manifest and lockfile projection
// of one generic recoverable file-set transaction.
type TransactionInput struct {
	ManifestTarget    declarationartifact.MutationTarget
	LockfileTarget    declarationartifact.MutationTarget
	StateDir          string
	ManifestBytes     []byte
	LockfileBytes     []byte
	SkipLockfileWrite bool
}

// CommitTransaction writes the manifest and lockfile under one recoverable
// file-set marker. The caller must hold their complete mutation lease set.
func CommitTransaction(ctx context.Context, input TransactionInput) error {
	if ctx == nil {
		return fmt.Errorf("authoring transaction context is required")
	}
	if err := input.ManifestTarget.Validate(ctx); err != nil {
		return fmt.Errorf("manifest target: %w", err)
	}
	if err := input.LockfileTarget.Validate(ctx); err != nil {
		return fmt.Errorf("lockfile target: %w", err)
	}
	manifest, err := fileset.NewFileWrite(input.ManifestTarget.Path(), input.ManifestBytes)
	if err != nil {
		return fmt.Errorf("manifest target: %w", err)
	}
	targets := []fileset.FileTarget{manifest}
	if !input.SkipLockfileWrite {
		lockfile, targetErr := fileset.NewFileWrite(input.LockfileTarget.Path(), input.LockfileBytes)
		if targetErr != nil {
			return fmt.Errorf("lockfile target: %w", targetErr)
		}
		targets = append(targets, lockfile)
	} else {
		lockfile, targetErr := fileset.NewFileRetain(input.LockfileTarget.Path())
		if targetErr != nil {
			return fmt.Errorf("retained lockfile target: %w", targetErr)
		}
		targets = append(targets, lockfile)
	}
	err = fileset.CommitFileSet(ctx, fileset.FileSetInput{StateDir: input.StateDir, Targets: targets})
	var writeFailure fileset.TargetWriteError
	if errors.As(err, &writeFailure) {
		return declarationartifact.ReplacementError{Path: writeFailure.Path, Err: err}
	}
	return err
}

// AuthorityPath returns the stable marker root that a writer must lease before recovery or commit.
func AuthorityPath(stateDir string) (string, error) {
	return fileset.FileSetAuthorityPath(stateDir)
}

// RecoverInterruptedTransaction recovers only exact current targets while their caller-owned leases are held.
func RecoverInterruptedTransaction(
	ctx context.Context,
	stateDir string,
	manifestPath string,
	lockfilePath string,
) error {
	paths, err := declarationartifact.MutationPaths(ctx, manifestPath, lockfilePath)
	if err != nil {
		return err
	}
	return fileset.RecoverFileSet(ctx, stateDir, paths)
}
