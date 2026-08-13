//go:build !darwin && !linux

package filesnapshot

import "os"

func regularFileObjectIdentity(os.FileInfo) (fileObjectIdentity, bool) {
	return fileObjectIdentity{}, false
}
