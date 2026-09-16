package recoverygate

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
)

// RequireUserStateMigrated prevents a new storage selection from hiding legacy
// management or recovery. A relocation receipt is checked, never followed.
func RequireUserStateMigrated(ctx context.Context, paths daempaths.Paths) error {
	if err := requireBarrierContext(ctx); err != nil {
		return err
	}
	legacy, needed, err := legacyUserStateSelection(paths)
	if err != nil || !needed {
		return err
	}
	if err := Observe(ctx, legacy); err != nil {
		return LegacyUserStateRecoveryError{StateDir: legacy.StateDir, Err: err}
	}
	receipt, relocated, err := statefile.LoadRelocation(ctx, legacy.StatefilePath)
	if errors.Is(err, os.ErrNotExist) {
		claimed, claimErr := hasLegacyUserClaims(ctx, paths, legacy)
		if claimErr != nil || !claimed {
			return claimErr
		}
		err = nil
	}
	if err != nil {
		return fmt.Errorf("inspect legacy user state: %w", err)
	}
	if !relocated {
		return fmt.Errorf("user manifest has legacy state at %q; selected state is %q; preview daem migrate state --dry-run (or --recover --dry-run after an interrupted migration); legacy journals remain accessible with daem recover --legacy-user-state", legacy.StatefilePath, paths.StatefilePath)
	}
	old, err := selectedStateAuthority(legacy)
	if err != nil {
		return err
	}
	current, err := selectedStateAuthority(paths)
	if err != nil {
		return err
	}
	if !receipt.From().Equal(old) || !receipt.To().Equal(current) {
		return fmt.Errorf("legacy state relocation does not match the selected state authority; preserve both state roots and restore the original HOME/XDG selection")
	}
	if _, err := statefile.Load(ctx, paths.StatefilePath); err != nil {
		return fmt.Errorf("relocated user state is unavailable: %w", err)
	}
	return nil
}

// LegacyUserStateRecoveryError distinguishes apply-journal recovery from
// metadata recovery owned by the matching previous writer.
type LegacyUserStateRecoveryError struct {
	StateDir string
	Err      error
}

func (err LegacyUserStateRecoveryError) Error() string {
	if fileset.FileSetFenceKindOf(err.Err) != fileset.FileSetFenceClear {
		return fmt.Sprintf("legacy metadata at %q requires recovery using the matching previous daem version and original command before migration; preserve the evidence: %v", err.StateDir, err.Err)
	}
	return fmt.Sprintf("legacy user state at %q requires recovery with daem recover --legacy-user-state: %v", err.StateDir, err.Err)
}

func (err LegacyUserStateRecoveryError) Unwrap() error { return err.Err }

func hasLegacyUserClaims(ctx context.Context, paths, legacy daempaths.Paths) (bool, error) {
	key, err := mutation.CanonicalDirectoryEntryKey(legacy.StatefilePath)
	if err != nil {
		return false, err
	}
	outputs, err := ownershipstore.New(paths.OwnershipRegistryPath)
	if err != nil {
		return false, err
	}
	registry, err := outputs.Load(ctx)
	if err != nil {
		return false, err
	}
	for _, claim := range registry.Claims() {
		if claim.Owner().StatefileKey() == key {
			return true, nil
		}
	}

	carriers, err := carrierclaim.New(paths.CarrierClaimRegistryPath)
	if err != nil {
		return false, err
	}
	claims, err := carriers.Load(ctx)
	if err != nil {
		return false, err
	}
	for _, claim := range claims.Claims() {
		if claim.Owner().StatefileKey() == key {
			return true, nil
		}
	}
	return false, nil
}

func legacyUserStateSelection(paths daempaths.Paths) (daempaths.Paths, bool, error) {
	if paths.LegacyUserStateDir == "" {
		return daempaths.Paths{}, false, nil
	}
	legacy, err := paths.LegacyUserState()
	if err != nil {
		return daempaths.Paths{}, false, err
	}
	oldKey, err := mutation.CanonicalDirectoryEntryKey(legacy.StatefilePath)
	if err != nil {
		return daempaths.Paths{}, false, err
	}
	newKey, err := mutation.CanonicalDirectoryEntryKey(paths.StatefilePath)
	return legacy, oldKey != newKey, err
}

func selectedStateAuthority(paths daempaths.Paths) (stateauthority.Authority, error) {
	observed, err := mutation.ObservePersistedDirectoryEntryAuthority(paths.StatefilePath)
	if err != nil {
		return stateauthority.Authority{}, err
	}
	return stateauthority.New(observed.Exact(), paths.ManifestPath)
}
