package diagnose

import (
	"errors"
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/findings"
	"github.com/isty2e/daem/internal/platformsupport"
)

// PlatformCheck projects target and runtime admission into one doctor finding.
func PlatformCheck(assessment platformsupport.PlatformAssessment) findings.Check {
	admission := assessment.TargetAdmission()
	if assessment.IsAdmitted() {
		detail := fmt.Sprintf("%s is an admitted product target (verification=%s)", admission.Target(), admission.Verification())
		if minimum, required := assessment.RuntimeRequirement(); required {
			observed, _ := assessment.RuntimeObservation().Version()
			detail = fmt.Sprintf(
				"%s is an admitted product target (runtime=macOS %s; required>=%s; verification=%s)",
				admission.Target(),
				observed,
				minimum,
				admission.Verification(),
			)
		}
		return findings.OKCheck(
			"platform",
			detail,
		)
	}

	err := assessment.RequireSupported()
	check := findings.ErrorCheck("platform", err.Error())
	var runtimeError *platformsupport.UnsupportedRuntimeError
	if errors.As(err, &runtimeError) {
		if _, observed := runtimeError.ObservedVersion(); observed {
			check.NextStep = "upgrade macOS to " + runtimeError.MinimumVersion().String() + " or newer"
		} else {
			check.NextStep = "verify /usr/bin/sw_vers --productVersion, then rerun daem doctor"
		}
		return check
	}
	check.NextStep = "run daem on an admitted platform: " + strings.Join(admittedPlatformNames(), ", ")
	return check
}

func admittedPlatformNames() []string {
	targets := platformsupport.AdmittedTargets()
	names := make([]string, 0, len(targets))
	for _, target := range targets {
		names = append(names, target.String())
	}
	return names
}
