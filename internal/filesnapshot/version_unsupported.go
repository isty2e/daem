//go:build !darwin && !linux

package filesnapshot

import "os"

func fileChangeVersion(os.FileInfo) (changeVersion, bool) {
	return changeVersion{}, false
}
