package paths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func selectUserStorage(selected Paths) (Paths, error) {
	configDir, err := defaultRootConfigDir()
	if err != nil {
		return Paths{}, err
	}
	userManifest := filepath.Join(configDir, manifestFileName)
	selectedUser, err := isUserManifestEntry(selected.ManifestPath, userManifest)
	if err != nil {
		return Paths{}, err
	}
	if !selectedUser {
		return selected, nil
	}

	user, err := defaultPaths()
	if err != nil {
		return Paths{}, err
	}
	user.ManifestPath = selected.ManifestPath
	user.ManifestRoot = selected.ManifestRoot
	user.LockfilePath = selected.LockfilePath
	user.ManifestOrigin = selected.ManifestOrigin
	return user, nil
}

func isUserManifestEntry(selected, user string) (bool, error) {
	if selected == user {
		return true, nil
	}
	if !strings.EqualFold(filepath.Base(selected), manifestFileName) {
		return false, nil
	}
	selectedParent, err := os.Stat(filepath.Dir(selected))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	userParent, err := os.Stat(filepath.Dir(user))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !os.SameFile(selectedParent, userParent) {
		return false, nil
	}
	if filepath.Base(selected) == manifestFileName {
		return true, nil
	}

	selectedEntry, err := os.Lstat(selected)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	userEntry, err := os.Lstat(user)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !os.SameFile(selectedEntry, userEntry) {
		return false, nil
	}
	// Case-folded spelling can name one entry, but two case-distinct hardlink
	// entries still select different workspaces on a case-sensitive filesystem.
	entries, err := os.ReadDir(filepath.Dir(user))
	if err != nil {
		return false, err
	}
	spellings := 0
	for _, entry := range entries {
		if strings.EqualFold(entry.Name(), manifestFileName) {
			spellings++
		}
	}
	return spellings == 1, nil
}

// LegacyUserState selects only the former manifest-local authority of the
// selected default user manifest. It is reserved for migration and recovery.
func (paths Paths) LegacyUserState() (Paths, error) {
	if paths.LegacyUserStateDir == "" {
		return Paths{}, fmt.Errorf("state migration requires the default user manifest")
	}
	legacy := paths
	legacy.StateDir = paths.LegacyUserStateDir
	legacy.StatefilePath = filepath.Join(legacy.StateDir, "state.json")
	legacy.RecoveryDir = filepath.Join(legacy.StateDir, "recovery")
	legacy.CacheDir = filepath.Join(legacy.StateDir, "cache")
	legacy.SourceCacheDir = filepath.Join(legacy.CacheDir, "sources")
	legacy.LegacyUserStateDir = ""
	return legacy, nil
}
