// Package platform obtains runtime evidence required by platform admission.
package platform

import (
	"context"
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/platformsupport"
)

type commandResult struct {
	stdout          string
	stdoutTruncated bool
	timedOut        bool
	err             error
}

type commandRunner func(context.Context) commandResult

// Current observes the runtime evidence available on the running platform.
func Current(ctx context.Context) (platformsupport.RuntimeObservation, error) {
	if err := ctx.Err(); err != nil {
		return platformsupport.RuntimeObservation{}, err
	}
	run, available := currentCommandRunner()
	if !available {
		return platformsupport.RuntimeObservation{}, nil
	}
	return observeDarwinProductVersion(ctx, run)
}

func observeDarwinProductVersion(
	ctx context.Context,
	run commandRunner,
) (platformsupport.RuntimeObservation, error) {
	if err := ctx.Err(); err != nil {
		return platformsupport.RuntimeObservation{}, err
	}
	if run == nil {
		return observationFailure(platformsupport.RuntimeObservationCommandFailed)
	}

	result := run(ctx)
	if result.timedOut {
		return observationFailure(platformsupport.RuntimeObservationTimedOut)
	}
	if result.err == nil {
		if result.stdoutTruncated {
			return observationFailure(platformsupport.RuntimeObservationInvalidOutput)
		}
		value, err := canonicalProductVersionOutput(result.stdout)
		if err != nil {
			return observationFailure(platformsupport.RuntimeObservationInvalidOutput)
		}
		version, err := platformsupport.ParseMacOSProductVersion(value)
		if err != nil {
			return observationFailure(platformsupport.RuntimeObservationInvalidOutput)
		}
		return platformsupport.NewRuntimeObservation(version)
	}
	if err := ctx.Err(); err != nil {
		return platformsupport.RuntimeObservation{}, err
	}
	return observationFailure(platformsupport.RuntimeObservationCommandFailed)
}

func observationFailure(
	reason platformsupport.RuntimeObservationReason,
) (platformsupport.RuntimeObservation, error) {
	return platformsupport.NewRuntimeObservationFailure(reason)
}

func canonicalProductVersionOutput(output string) (string, error) {
	value := strings.TrimSuffix(output, "\n")
	if value == "" {
		return "", fmt.Errorf("macOS product-version output is empty")
	}
	if strings.ContainsAny(value, "\r\n") || strings.TrimSpace(value) != value {
		return "", fmt.Errorf("macOS product-version output is not one canonical line")
	}
	return value, nil
}
