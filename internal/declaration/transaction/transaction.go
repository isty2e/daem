package transaction

import (
	"context"
	"fmt"
)

// TransactionInput is the declaration-owned manifest and lockfile projection
// of one generic recoverable file-set transaction.
type TransactionInput struct {
	ManifestPath      string
	LockfilePath      string
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
	manifest, err := NewFileWrite(input.ManifestPath, input.ManifestBytes)
	if err != nil {
		return fmt.Errorf("manifest target: %w", err)
	}
	targets := []FileTarget{manifest}
	if !input.SkipLockfileWrite {
		lockfile, targetErr := NewFileWrite(input.LockfilePath, input.LockfileBytes)
		if targetErr != nil {
			return fmt.Errorf("lockfile target: %w", targetErr)
		}
		targets = append(targets, lockfile)
	} else {
		lockfile, targetErr := NewFileRetain(input.LockfilePath)
		if targetErr != nil {
			return fmt.Errorf("retained lockfile target: %w", targetErr)
		}
		targets = append(targets, lockfile)
	}
	return CommitFileSet(ctx, FileSetInput{StateDir: input.StateDir, Targets: targets})
}

// AuthorityPath returns the stable marker root that a writer must lease before recovery or commit.
func AuthorityPath(stateDir string) (string, error) {
	return FileSetAuthorityPath(stateDir)
}

// RecoverInterruptedTransaction recovers only exact current targets while their caller-owned leases are held.
func RecoverInterruptedTransaction(
	ctx context.Context,
	stateDir string,
	manifestPath string,
	lockfilePath string,
) error {
	return RecoverFileSet(ctx, stateDir, []string{manifestPath, lockfilePath})
}
