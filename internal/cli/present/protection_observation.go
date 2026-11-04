package clipresent

import (
	"fmt"
	"io"

	ownershipobserve "github.com/isty2e/daem/internal/assurance/observe/ownership"
)

type permissionProtectionJSON struct {
	Observation ownershipobserve.ProtectionObservationState `json:"observation"`
	Records     []observedProtectionJSON                    `json:"observed_records,omitempty"`
	Reason      string                                      `json:"reason,omitempty"`
	NextStep    string                                      `json:"next_step,omitempty"`
}

type observedProtectionJSON struct {
	Invocation string                          `json:"invocation"`
	Phase      string                          `json:"phase"`
	Documents  []observedProtectedDocumentJSON `json:"documents"`
}

type observedProtectedDocumentJSON struct {
	Destinations []string `json:"destinations"`
}

func permissionProtectionJSONFor(observation ownershipobserve.ProtectionObservation) *permissionProtectionJSON {
	if observation.KnownClear() || observation.State() == ownershipobserve.ProtectionUnobserved {
		return nil
	}
	result := &permissionProtectionJSON{Observation: observation.State()}
	if observation.HasPending() {
		result.NextStep = "run daem recover --dry-run with the same manifest selection"
		for _, record := range observation.Records() {
			row := observedProtectionJSON{Invocation: record.Invocation().String(), Phase: string(record.Phase())}
			for _, document := range record.Documents() {
				var destinations []string
				for _, destination := range document.DestinationsForOwner(record.Owner().Key()) {
					destinations = append(destinations, destination.String())
				}
				row.Documents = append(row.Documents, observedProtectedDocumentJSON{Destinations: destinations})
			}
			result.Records = append(result.Records, row)
		}
	} else if observation.State() == ownershipobserve.ProtectionUnavailable {
		result.Reason = permissionReason(observation.Cause())
	}
	return result
}

func PrintPermissionProtection(output io.Writer, observation ownershipobserve.ProtectionObservation) {
	value := permissionProtectionJSONFor(observation)
	if value == nil {
		return
	}
	fmt.Fprintf(output, "permission protection: %s", value.Observation)
	if value.Reason != "" {
		fmt.Fprintf(output, " reason=%s", value.Reason)
	}
	fmt.Fprintln(output)
	for _, record := range value.Records {
		fmt.Fprintf(output, "  observed protection %s phase=%s\n", record.Invocation, record.Phase)
		for _, document := range record.Documents {
			for _, destination := range document.Destinations {
				fmt.Fprintf(output, "    %q\n", Escape(destination))
			}
		}
	}
	if value.NextStep != "" {
		fmt.Fprintln(output, value.NextStep)
	}
}
