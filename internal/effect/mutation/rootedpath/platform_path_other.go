//go:build !windows

package rootedpath

func platformRootSelectorPath(value string) (path string, directoryOnly bool) {
	return value, false
}

func validatePlatformPhysicalRoot(string) error {
	return nil
}

func validatePlatformRelativeDestination(string) error {
	return nil
}

func validatePlatformDestinationPath(string) error {
	return nil
}

func validatePlatformComponent(string) error {
	return nil
}

func validatePlatformRelativeForRoot(*capturedRootPlatform, string) error {
	return nil
}
