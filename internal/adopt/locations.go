package adopt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

// DiscoveryLocations returns import-authorized host paths only.
func DiscoveryLocations(
	selectedTarget target.Target,
	resourceKind entity.Kind,
	scope target.Scope,
) []profile.DiscoveryLocation {
	return profile.Profile(selectedTarget).DiscoveryLocations(resourceKind, scope)
}

// RuntimeLocations returns classify-only host runtime paths.
func RuntimeLocations(
	selectedTarget target.Target,
	resourceKind entity.Kind,
	scope target.Scope,
) []profile.RuntimeLocation {
	return profile.Profile(selectedTarget).RuntimeLocations(resourceKind, scope)
}

func LocationPath(portablePath string) (string, error) {
	if strings.HasPrefix(portablePath, "~/") {
		homeDirectory, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return filepath.Join(homeDirectory, filepath.FromSlash(strings.TrimPrefix(portablePath, "~/"))), nil
	}
	if filepath.IsAbs(portablePath) {
		return filepath.Clean(portablePath), nil
	}
	return filepath.FromSlash(portablePath), nil
}

func PathExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func ResolveDestination(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("destination is required")
	}
	if strings.HasPrefix(value, "~/") {
		homeDirectory, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory for %q: %w", value, err)
		}
		return filepath.Join(homeDirectory, filepath.FromSlash(strings.TrimPrefix(value, "~/"))), nil
	}
	if filepath.IsAbs(value) {
		return "", fmt.Errorf("destination %q must be project-relative or home-relative", value)
	}
	cleaned, err := CleanProjectDestination(value)
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(".", filepath.FromSlash(cleaned))), nil
}

func CleanProjectDestination(value string) (string, error) {
	if strings.Contains(value, "\\") {
		return "", fmt.Errorf("destination %q must use slash-separated relative paths", value)
	}
	cleaned := filepath.ToSlash(filepath.Clean(value))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("destination %q must stay inside the project root", value)
	}
	return cleaned, nil
}
