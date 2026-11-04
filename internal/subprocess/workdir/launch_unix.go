//go:build darwin || linux

package workdir

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	descriptorHelperEnv = "DAEM_INTERNAL_DESCRIPTOR_WORKDIR_V1"
	descriptorHelperArg = "--daem-internal-descriptor-workdir-v1"
	descriptorHelperFD  = 3
)

func init() {
	if os.Getenv(descriptorHelperEnv) == descriptorGateMode && len(os.Args) >= 2 && os.Args[1] == descriptorGateArg {
		if len(os.Args) >= 4 {
			runGatedDescriptorHelper()
		}
		os.Exit(126)
	}
	if os.Getenv(descriptorHelperEnv) != "1" || len(os.Args) < 3 || os.Args[1] != descriptorHelperArg {
		return
	}
	if err := unix.Fchdir(descriptorHelperFD); err != nil {
		writeDescriptorHelperError("working-directory setup", err)
		os.Exit(126)
	}
	execDescriptorTarget(os.Args[2], os.Args[3:])
}

func execDescriptorTarget(target string, args []string) {
	env := removeEnvName(os.Environ(), descriptorHelperEnv)
	argv := append([]string{target}, args...)
	if err := unix.Exec(target, argv, env); err != nil {
		writeDescriptorHelperError("target exec", err)
		os.Exit(126)
	}
}

func writeDescriptorHelperError(stage string, err error) {
	// The helper's fd 2 is the parent command runner's bounded capture pipe, not
	// a presentation-owned terminal handle.
	_, _ = unix.Write(2, fmt.Appendf(nil, "daem internal %s failed: %v\n", stage, err))
}

// ValidateCapability reports whether this platform can launch
// a child from descriptor-backed working-directory authority.
func ValidateCapability() error {
	return nil
}

// Validate verifies that directory is an open native directory
// witness suitable for descriptor-backed launch.
func Validate(directory *os.File) error {
	if directory == nil {
		return fmt.Errorf("working-directory descriptor is required")
	}
	info, err := directory.Stat()
	if err != nil {
		return fmt.Errorf("inspect working-directory descriptor: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("working-directory descriptor does not name a directory")
	}
	return nil
}

// PrepareCommand constructs a child command whose cwd is inherited from directory.
func PrepareCommand(
	ctx context.Context,
	target string,
	args []string,
	env []string,
	directory *os.File,
) (*exec.Cmd, error) {
	if err := Validate(directory); err != nil {
		return nil, err
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve descriptor helper executable: %w", err)
	}
	helperArgs := append([]string{descriptorHelperArg, target}, args...)
	cmd := exec.CommandContext(ctx, executable, helperArgs...)
	cmd.Env = appendEnvironmentValue(env, descriptorHelperEnv, "1")
	cmd.ExtraFiles = []*os.File{directory}
	return cmd, nil
}

func removeEnvName(env []string, name string) []string {
	prefix := name + "="
	result := make([]string, 0, len(env))
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			continue
		}
		result = append(result, item)
	}
	return result
}

func appendEnvironmentValue(env []string, name string, value string) []string {
	prefix := name + "="
	result := make([]string, 0, len(env)+1)
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			continue
		}
		result = append(result, item)
	}
	return append(result, prefix+value)
}
