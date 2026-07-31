//go:build darwin

package mutation

import "strings"

func platformLegacyDirectoryEntryKey(
	selection pathSelection,
	currentKey string,
) string {
	legacy := strings.ToLower(selectedAccessPath(selection))
	if legacy == currentKey {
		return ""
	}
	return legacy
}
