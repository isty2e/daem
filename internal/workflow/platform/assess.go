// Package platform composes platform policy with current runtime evidence.
package platform

import (
	"context"
	"fmt"

	platformobserve "github.com/isty2e/daem/internal/assurance/observe/platform"
	"github.com/isty2e/daem/internal/platformsupport"
)

// RuntimeObserver obtains current runtime evidence without deciding admission.
type RuntimeObserver func(context.Context) (platformsupport.RuntimeObservation, error)

// Assess evaluates target policy before obtaining only the runtime evidence it requires.
func Assess(
	ctx context.Context,
	admission platformsupport.Admission,
	observer RuntimeObserver,
) (platformsupport.PlatformAssessment, error) {
	if ctx == nil {
		return platformsupport.PlatformAssessment{}, fmt.Errorf("platform assessment context is required")
	}
	if err := ctx.Err(); err != nil {
		return platformsupport.PlatformAssessment{}, err
	}
	if err := admission.RequireSupported(); err != nil {
		return platformsupport.AssessRuntime(admission, platformsupport.RuntimeObservation{}), nil
	}
	if _, required := admission.RuntimeRequirement(); !required {
		return platformsupport.AssessRuntime(admission, platformsupport.RuntimeObservation{}), nil
	}
	if observer == nil {
		observer = platformobserve.Current
	}
	observation, err := observer(ctx)
	if err != nil {
		return platformsupport.PlatformAssessment{}, err
	}
	return platformsupport.AssessRuntime(admission, observation), nil
}
