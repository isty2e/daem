package executioncontext

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/isty2e/daem/internal/executionidentity"
	"golang.org/x/sys/unix"
)

func readExecutionContext(ctx context.Context) (executionidentity.Context, error) {
	machine, err := readExecutionIdentityFile("/etc/machine-id", false)
	if err != nil {
		return executionidentity.Context{}, err
	}
	host, err := canonicalExecutionHostID(machine)
	if err != nil {
		return executionidentity.Context{}, err
	}
	epoch, err := readExecutionIdentityFile("/proc/sys/kernel/random/boot_id", true)
	if err != nil {
		return executionidentity.Context{}, err
	}
	pid, err := executionNamespace("pid", unix.CLONE_NEWPID)
	if err != nil {
		return executionidentity.Context{}, err
	}
	children, err := executionNamespace("pid_for_children", unix.CLONE_NEWPID)
	if err != nil || pid != children {
		return executionidentity.Context{}, fmt.Errorf("child PID namespace differs from the current syscall view")
	}
	user, err := executionNamespace("user", unix.CLONE_NEWUSER)
	if err != nil {
		return executionidentity.Context{}, err
	}
	if err := ctx.Err(); err != nil {
		return executionidentity.Context{}, err
	}
	return executionidentity.NewContextWithVisibility(executionidentity.Linux,
		executionContextDigest("host", "linux", host), strings.TrimSpace(epoch),
		executionContextDigest("visibility", "linux", pid, user))
}

func readExecutionIdentityFile(path string, proc bool) (string, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return "", err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return "", fmt.Errorf("execution identity source is not a regular file")
	}
	if proc {
		var filesystem unix.Statfs_t
		if err := unix.Fstatfs(fd, &filesystem); err != nil || filesystem.Type != unix.PROC_SUPER_MAGIC {
			return "", fmt.Errorf("execution epoch source is not procfs")
		}
	}
	content, err := io.ReadAll(io.LimitReader(file, 65))
	if err != nil || len(content) > 64 {
		return "", fmt.Errorf("execution identity source is unavailable or oversized")
	}
	return string(content), nil
}

func executionNamespace(name string, kind int) (string, error) {
	fd, err := unix.Open("/proc/thread-self/ns/"+name, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", err
	}
	defer unix.Close(fd)
	var filesystem unix.Statfs_t
	var stat unix.Stat_t
	if err := unix.Fstatfs(fd, &filesystem); err != nil || filesystem.Type != unix.NSFS_MAGIC {
		return "", fmt.Errorf("execution namespace source is not nsfs")
	}
	observed, err := unix.IoctlRetInt(fd, unix.NS_GET_NSTYPE)
	if err != nil || observed != kind {
		return "", fmt.Errorf("execution namespace kind is unavailable or inconsistent")
	}
	if err := unix.Fstat(fd, &stat); err != nil || stat.Ino == 0 {
		return "", fmt.Errorf("execution namespace identity unavailable")
	}
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), nil
}
