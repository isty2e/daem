package subprocess

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

const (
	defaultQuiescencePeriod = 50 * time.Millisecond
	defaultGracePeriod      = 500 * time.Millisecond
	defaultKillWait         = 2 * time.Second
)

var (
	errProcessTreeUnsupported   = errors.New("process-group supervision is unsupported")
	errProcessGroupUnsignalable = errors.New("process group occupancy is unsignalable")
)

type processTerminationOptions struct {
	QuiescencePeriod time.Duration
	GracePeriod      time.Duration
	KillWait         time.Duration
}

// ProcessTermination describes one idempotent process-group termination attempt.
type ProcessTermination struct {
	processesFound        bool
	escalated             bool
	unsignalableOccupancy bool
}

// ProcessesFound reports whether the dedicated group still had signalable
// members when termination began.
func (termination ProcessTermination) ProcessesFound() bool {
	return termination.processesFound
}

// UnsignalableOccupancy reports that the dedicated pgid was occupied but not
// signalable. That is not proof of in-group descendants.
func (termination ProcessTermination) UnsignalableOccupancy() bool {
	return termination.unsignalableOccupancy
}

// ProcessGroup owns termination for one command's fresh dedicated process group.
type ProcessGroup struct {
	command *exec.Cmd
	options processTerminationOptions

	terminateOnce sync.Once
	terminateDone chan struct{}
	termination   ProcessTermination
	terminateErr  error
}

// BindProcessGroup configures command to create a fresh process group and replaces the
// CommandContext direct-child cancellation hook with dedicated-group cleanup.
// BindProcessGroup must be called before command.Start. Session-escaped
// descendants are outside this primitive.
func BindProcessGroup(command *exec.Cmd) (*ProcessGroup, error) {
	return bindProcessGroupWithOptions(command, processTerminationOptions{})
}

func bindProcessGroupWithOptions(command *exec.Cmd, options processTerminationOptions) (*ProcessGroup, error) {
	if command == nil {
		return nil, fmt.Errorf("bind process group: command is required")
	}
	if command.Process != nil {
		return nil, fmt.Errorf("bind process group: command already started")
	}
	if command.Cancel == nil {
		return nil, fmt.Errorf("bind process group: command must be created with exec.CommandContext")
	}
	if err := configureDedicatedProcessGroup(command); err != nil {
		return nil, fmt.Errorf("bind process group: %w", err)
	}
	group := &ProcessGroup{
		command:       command,
		options:       options.withDefaults(),
		terminateDone: make(chan struct{}),
	}
	command.Cancel = group.cancel
	return group, nil
}

// Terminate gracefully stops every member of the bound process group, then
// escalates and verifies disappearance within the configured bounds.
func (group *ProcessGroup) Terminate() (ProcessTermination, error) {
	return group.terminate(false)
}

// ReapAfterLeaderExit allows short-lived in-group descendants to finish
// naturally before terminating and reporting residual group members.
func (group *ProcessGroup) ReapAfterLeaderExit() (ProcessTermination, error) {
	return group.terminate(true)
}

func (group *ProcessGroup) terminate(quiesce bool) (ProcessTermination, error) {
	if group == nil || group.command == nil {
		return ProcessTermination{}, fmt.Errorf("terminate process group: group is required")
	}
	if group.command.Process == nil {
		return ProcessTermination{}, fmt.Errorf("terminate process group: command has not started")
	}

	group.terminateOnce.Do(func() {
		group.termination, group.terminateErr = terminateDedicatedProcessGroup(
			group.command.Process.Pid,
			group.options,
			quiesce,
		)
		close(group.terminateDone)
	})
	<-group.terminateDone
	return group.termination, group.terminateErr
}

func (group *ProcessGroup) cancel() error {
	termination, err := group.Terminate()
	if err != nil {
		return err
	}
	if !termination.ProcessesFound() {
		return os.ErrProcessDone
	}
	return nil
}

func (options processTerminationOptions) withDefaults() processTerminationOptions {
	if options.QuiescencePeriod <= 0 {
		options.QuiescencePeriod = defaultQuiescencePeriod
	}
	if options.GracePeriod <= 0 {
		options.GracePeriod = defaultGracePeriod
	}
	if options.KillWait <= 0 {
		options.KillWait = defaultKillWait
	}
	return options
}
