//go:build darwin || linux

package subprocess

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const processGroupPollInterval = 10 * time.Millisecond

func configureDedicatedProcessGroup(command *exec.Cmd) error {
	attributes := command.SysProcAttr
	if attributes == nil {
		attributes = &syscall.SysProcAttr{}
	} else {
		copy := *attributes
		attributes = &copy
	}
	if attributes.Setsid || attributes.Foreground || attributes.Pgid != 0 {
		return fmt.Errorf("existing session or process-group policy conflicts with dedicated process-tree ownership")
	}
	attributes.Setpgid = true
	command.SysProcAttr = attributes
	return nil
}

func terminateDedicatedProcessGroup(pid int, options processTerminationOptions, quiesce bool) (ProcessTermination, error) {
	if quiesce {
		gone, err := waitForDedicatedProcessGroup(pid, options.QuiescencePeriod)
		if err != nil {
			return ProcessTermination{}, err
		}
		if gone {
			return ProcessTermination{}, nil
		}
	}
	alive, err := dedicatedProcessGroupAlive(pid)
	if err != nil {
		return ProcessTermination{}, err
	}
	if !alive {
		return ProcessTermination{}, nil
	}

	termination := ProcessTermination{processesFound: true}
	termErr := signalDedicatedProcessGroup(pid, unix.SIGTERM)
	if errors.Is(termErr, unix.ESRCH) {
		return termination, nil
	}
	if termErr == nil {
		gone, waitErr := waitForDedicatedProcessGroup(pid, options.GracePeriod)
		if waitErr != nil {
			termErr = waitErr
		} else if gone {
			return termination, nil
		}
	}

	termination.escalated = true
	killErr := signalDedicatedProcessGroup(pid, unix.SIGKILL)
	if errors.Is(killErr, unix.ESRCH) {
		return termination, termErr
	}
	gone, waitErr := waitForDedicatedProcessGroup(pid, options.KillWait)
	if waitErr != nil {
		return termination, errors.Join(termErr, killErr, waitErr)
	}
	if !gone {
		waitErr = fmt.Errorf("process group %d survived SIGKILL for %s", pid, options.KillWait)
	}
	return termination, errors.Join(termErr, killErr, waitErr)
}

func signalDedicatedProcessGroup(pid int, signal syscall.Signal) error {
	if pid <= 0 {
		return fmt.Errorf("invalid process group id %d", pid)
	}
	return unix.Kill(-pid, signal)
}

func dedicatedProcessGroupAlive(pid int) (bool, error) {
	if pid <= 0 {
		return false, fmt.Errorf("invalid process group id %d", pid)
	}
	err := unix.Kill(-pid, 0)
	switch {
	case err == nil, errors.Is(err, unix.EPERM):
		return true, nil
	case errors.Is(err, unix.ESRCH):
		return false, nil
	default:
		return false, fmt.Errorf("probe process group %d: %w", pid, err)
	}
}

func waitForDedicatedProcessGroup(pid int, timeout time.Duration) (bool, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(processGroupPollInterval)
	defer ticker.Stop()

	for {
		alive, err := dedicatedProcessGroupAlive(pid)
		if err != nil || !alive {
			return !alive, err
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			alive, err := dedicatedProcessGroupAlive(pid)
			return !alive, err
		}
	}
}
