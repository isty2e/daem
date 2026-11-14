package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// A repeated signal remains an emergency escape hatch, but immediate os.Exit
// would bypass the bounded TERM-to-KILL cleanup started by root cancellation.
const forcedSignalExitDelay = 3 * time.Second

func exitWithSignalLifecycle(run func(context.Context) int) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	stopLoop := make(chan struct{})
	loopDone := make(chan os.Signal, 1)
	go func() {
		var first os.Signal
		var forceTimer *time.Timer
		var forceExit <-chan time.Time
		defer func() {
			if forceTimer != nil {
				forceTimer.Stop()
			}
		}()
		handle := func(received os.Signal) {
			if received == nil {
				return
			}
			if first == nil {
				first = received
				cancel()
				return
			}
			if forceTimer == nil {
				forceTimer = time.NewTimer(forcedSignalExitDelay)
				forceExit = forceTimer.C
			}
		}

		for {
			select {
			case received := <-signals:
				handle(received)
			case <-forceExit:
				os.Exit(signalExitCode(first))
			case <-stopLoop:
				for {
					select {
					case received := <-signals:
						handle(received)
					default:
						loopDone <- first
						return
					}
				}
			}
		}
	}()

	exitCode := run(ctx)
	// PTY teardown can deliver another HUP after command cleanup; interception
	// must remain active until the process exits.
	close(stopLoop)
	if first := <-loopDone; first != nil {
		exitCode = signalExitCode(first)
	}
	cancel()
	os.Exit(exitCode)
}

func signalExitCode(received os.Signal) int {
	switch received {
	case os.Interrupt:
		return 130
	case syscall.SIGTERM:
		return 143
	case syscall.SIGHUP:
		return 129
	default:
		return 1
	}
}
