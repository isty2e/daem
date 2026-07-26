//go:build !darwin && !linux

package mutation

import "os"

func validateLeaseEntryOwner(string, os.FileInfo) error {
	return nil
}
