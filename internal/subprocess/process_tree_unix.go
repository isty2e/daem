//go:build darwin || linux

package subprocess

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"

	"github.com/isty2e/daem/internal/subprocess/executioncontext"
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
		return fmt.Errorf("existing session or process-group policy conflicts with dedicated process-group ownership")
	}
	attributes.Setpgid = true
	command.SysProcAttr = attributes
	return nil
}

func terminateDedicatedProcessGroup(pid int, options processTerminationOptions, quiesce bool, leaderExited bool) (ProcessTermination, error) {
	if quiesce {
		occupancy, err := waitForDedicatedProcessGroup(pid, options.QuiescencePeriod)
		if err != nil {
			return ProcessTermination{}, err
		}
		switch occupancy {
		case executioncontext.GroupAbsent:
			return ProcessTermination{}, nil
		case executioncontext.GroupUnsignalable:
			return unsignalableProcessGroupTermination(pid, unix.EPERM)
		}
	}
	occupancy, err := executioncontext.ObserveGroup(pid)
	if err != nil {
		return ProcessTermination{}, err
	}
	switch occupancy {
	case executioncontext.GroupAbsent:
		return ProcessTermination{}, nil
	case executioncontext.GroupUnsignalable:
		return unsignalableProcessGroupTermination(pid, unix.EPERM)
	}

	termination := ProcessTermination{processesFound: true, residualMembers: leaderExited}
	termErr := signalDedicatedProcessGroup(pid, unix.SIGTERM)
	if errors.Is(termErr, unix.ESRCH) {
		return termination, nil
	}
	if errors.Is(termErr, unix.EPERM) {
		termination.unsignalableOccupancy = true
		return termination, unsignalableProcessGroupError(pid, termErr)
	}
	if termErr == nil {
		occupancy, waitErr := waitForDedicatedProcessGroup(pid, options.GracePeriod)
		if waitErr != nil {
			termErr = waitErr
		} else {
			switch occupancy {
			case executioncontext.GroupAbsent:
				return termination, nil
			case executioncontext.GroupUnsignalable:
				termination.unsignalableOccupancy = true
				return termination, unsignalableProcessGroupError(pid, unix.EPERM)
			}
		}
	}

	termination.escalated = true
	killErr := signalDedicatedProcessGroup(pid, unix.SIGKILL)
	if errors.Is(killErr, unix.ESRCH) {
		return termination, termErr
	}
	if errors.Is(killErr, unix.EPERM) {
		termination.unsignalableOccupancy = true
		return termination, errors.Join(termErr, unsignalableProcessGroupError(pid, killErr))
	}
	occupancy, waitErr := waitForDedicatedProcessGroup(pid, options.KillWait)
	if waitErr != nil {
		return termination, errors.Join(termErr, killErr, waitErr)
	}
	switch occupancy {
	case executioncontext.GroupAbsent:
		return termination, errors.Join(termErr, killErr)
	case executioncontext.GroupUnsignalable:
		termination.unsignalableOccupancy = true
		return termination, errors.Join(termErr, killErr, unsignalableProcessGroupError(pid, unix.EPERM))
	default:
		waitErr = fmt.Errorf("process group %d survived SIGKILL for %s", pid, options.KillWait)
		return termination, errors.Join(termErr, killErr, waitErr)
	}
}

func signalDedicatedProcessGroup(pid int, signal syscall.Signal) error {
	if pid <= 0 {
		return fmt.Errorf("invalid process group id %d", pid)
	}
	return unix.Kill(-pid, signal)
}

func waitForDedicatedProcessGroup(pid int, timeout time.Duration) (executioncontext.GroupOccupancy, error) {
	// Darwin can return EPERM for kill(-pgid, 0) while group members are
	// dying after SIGKILL. Keep polling until ESRCH or the bound; persistent
	// EPERM is unsignalable occupancy, not a leader-PID override.
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(processGroupPollInterval)
	defer ticker.Stop()

	for {
		occupancy, err := executioncontext.ObserveGroup(pid)
		if err != nil {
			return occupancy, err
		}
		if occupancy == executioncontext.GroupAbsent {
			return occupancy, nil
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			return executioncontext.ObserveGroup(pid)
		}
	}
}

func unsignalableProcessGroupTermination(pid int, err error) (ProcessTermination, error) {
	return ProcessTermination{unsignalableOccupancy: true}, unsignalableProcessGroupError(pid, err)
}

func unsignalableProcessGroupError(pid int, err error) error {
	if err == nil {
		err = unix.EPERM
	}
	return fmt.Errorf("%w: process group %d: %w", errProcessGroupUnsignalable, pid, err)
}
