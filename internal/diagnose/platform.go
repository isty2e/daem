package diagnose

import (
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/findings"
	"github.com/isty2e/daem/internal/platformsupport"
)

// PlatformCheck projects product-platform admission into one doctor finding.
func PlatformCheck(admission platformsupport.Admission) findings.Check {
	if admission.IsAdmitted() {
		return findings.OKCheck(
			"platform",
			fmt.Sprintf("%s is an admitted product target (verification=%s)", admission.Target(), admission.Verification()),
		)
	}

	check := findings.ErrorCheck("platform", admission.RequireSupported().Error())
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
