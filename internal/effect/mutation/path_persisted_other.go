//go:build !darwin

package mutation

func platformLegacyDirectoryEntryKey(pathSelection, string) string {
	return ""
}
