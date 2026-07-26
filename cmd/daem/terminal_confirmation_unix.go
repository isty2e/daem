//go:build darwin || linux

package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"

	"golang.org/x/sys/unix"
)

const confirmationPollIntervalMillis = 50

func readTerminalConfirmationLine(ctx context.Context, input io.Reader, maximumBytes int) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("confirmation context is required")
	}
	if maximumBytes <= 0 {
		return "", fmt.Errorf("confirmation response limit must be positive")
	}
	file, ok := input.(*os.File)
	if !ok {
		return "", fmt.Errorf("terminal confirmation input must be an OS file")
	}
	if file.Fd() > math.MaxInt32 {
		return "", fmt.Errorf("read confirmation: file descriptor is out of range")
	}

	answer := make([]byte, 0, 16)
	readBuffer := make([]byte, maximumBytes+1)
	descriptors := []unix.PollFd{{Fd: int32(file.Fd()), Events: unix.POLLIN}}
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		descriptors[0].Revents = 0
		ready, err := unix.Poll(descriptors, confirmationPollIntervalMillis)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return "", fmt.Errorf("poll confirmation input: %w", err)
		}
		if ready == 0 {
			continue
		}
		if descriptors[0].Revents&unix.POLLNVAL != 0 {
			return "", fmt.Errorf("poll confirmation input: invalid file descriptor")
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}

		count, readErr := file.Read(readBuffer)
		if count == 0 {
			if readErr != nil {
				return string(answer), readErr
			}
			return string(answer), io.EOF
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		for _, value := range readBuffer[:count] {
			if value == '\n' {
				return string(answer), nil
			}
			answer = append(answer, value)
			if len(answer) > maximumBytes {
				return "", fmt.Errorf("confirmation response exceeds %d bytes", maximumBytes)
			}
		}
		if readErr != nil {
			return string(answer), readErr
		}

		// A canonical terminal read becomes ready only after a line delimiter or
		// VEOF. VEOF is not returned as a byte, so a non-newline record is EOF.
		// Non-canonical terminals fail closed under the same rule.
		return string(answer), io.EOF
	}
}
