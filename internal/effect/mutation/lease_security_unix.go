//go:build darwin || linux

package mutation

import (
	"fmt"
	"os"
	"syscall"
)

func validateLeaseEntryOwner(path string, info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("mutation lease path %q has unavailable owner identity", path)
	}
	if stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("mutation lease path %q is owned by uid %d, want invoking uid %d", path, stat.Uid, os.Geteuid())
	}
	return nil
}
