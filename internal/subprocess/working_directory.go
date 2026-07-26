package subprocess

import (
	"os"
	"strings"
)

const (
	workingDirectorySetupFailurePrefix = "daem internal working-directory setup failed:"
	targetExecFailurePrefix            = "daem internal target exec failed:"
)

// WorkingDirectoryBinding is one short-lived native directory authority for a process launch.
type WorkingDirectoryBinding interface {
	Validate() error
	OpenDirectory() (*os.File, error)
	Close() error
}

// WorkingDirectoryBinder acquires one fresh binding for one process attempt. The caller owns
// and closes every non-nil binding returned successfully.
type WorkingDirectoryBinder func() (WorkingDirectoryBinding, error)

// reportsWorkingDirectorySetupFailure identifies a descriptor-helper cwd failure.
func reportsWorkingDirectorySetupFailure(stderr string) bool {
	return strings.Contains(stderr, workingDirectorySetupFailurePrefix)
}

// reportsTargetExecFailure identifies a descriptor-helper target exec failure.
func reportsTargetExecFailure(stderr string) bool {
	return strings.Contains(stderr, targetExecFailurePrefix)
}
