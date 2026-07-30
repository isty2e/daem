package journal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/isty2e/daem/internal/effect/journal/retirement"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func EnsureNoActive(recoveryRoot string) error {
	operations, err := activeRecoveryOperations(recoveryRoot)
	if err != nil {
		return err
	}
	if len(operations) == 0 {
		return nil
	}
	if len(operations) > 1 {
		return fmt.Errorf("multiple interrupted apply operations found; run: daem recover --dry-run")
	}

	return fmt.Errorf("interrupted apply operation found; run: daem recover --dry-run")
}

func activeRecoveryOperations(recoveryRoot string) ([]string, error) {
	entries, err := os.ReadDir(recoveryRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("read recovery directory: %w", err)
	}

	operations := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if !isSafeRecoveryOperationID(entry.Name()) {
			return nil, fmt.Errorf("recovery operation id %q must be a safe path component", entry.Name())
		}
		operations = append(operations, entry.Name())
	}
	sort.Strings(operations)

	return operations, nil
}

func isSafeRecoveryOperationID(value string) bool {
	return retirement.ValidateOperationID(value) == nil
}

// RemoveJournal removes one recovery operation through retained root authority.
func RemoveJournal(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	capability rootedpath.CommitCapability,
) error {
	if filesystem == nil {
		return fmt.Errorf("recovery journal filesystem is required")
	}
	if capability == nil {
		return fmt.Errorf("recovery journal capability is required")
	}
	expected, err := filesystem.CaptureRootedEntryIdentity(ctx, capability)
	if errors.Is(err, os.ErrNotExist) {
		return capability.Close()
	}
	if err != nil {
		return errors.Join(err, capability.Close())
	}
	return filesystem.RemoveRootedEntry(ctx, capability, expected)
}
