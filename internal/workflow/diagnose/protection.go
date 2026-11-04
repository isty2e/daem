package diagnoseworkflow

import (
	"fmt"

	ownershipobserve "github.com/isty2e/daem/internal/assurance/observe/ownership"
	"github.com/isty2e/daem/internal/findings"
)

func permissionProtectionCheck(observation ownershipobserve.ProtectionObservation) findings.Check {
	const name = "permission_protection"
	if observation.KnownClear() {
		return findings.OKCheck(name, "no pending permission protection was observed for this owner")
	}
	if observation.State() == ownershipobserve.ProtectionUnsupported {
		return findings.UnsupportedCheck(name, "permission protection inventory cannot be qualified on this platform")
	}
	check := findings.ErrorCheck(name, "permission protection inventory could not be qualified")
	if observation.HasPending() {
		check.Detail = fmt.Sprintf("observed %d pending permission protection records for this owner", len(observation.Records()))
		check.NextStep = "run daem recover --dry-run with the same manifest selection"
	}
	return check
}
