//go:build !windows

package rootedpath

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
