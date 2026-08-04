package aggregate

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/topology"
)

func TestContributionOccupancySetOwnsExactSubjectCoverage(t *testing.T) {
	alpha := mustSubjectContribution(
		t,
		"alpha",
		testSharedHookContributionInput(t, `{"command":"alpha"}`),
	)
	beta := mustSubjectContribution(
		t,
		"beta",
		testSharedHookContributionInput(t, `{"command":"beta"}`),
	)
	contributions, err := NewContributionSet([]SubjectContribution{beta, alpha})
	if err != nil {
		t.Fatal(err)
	}
	states := map[topology.SubjectID]ContributionOccupancyState{
		alpha.SubjectID(): ContributionPresent,
		beta.SubjectID():  ContributionAbsent,
	}
	occupancy, err := NewContributionOccupancySet(contributions, states)
	if err != nil {
		t.Fatalf("NewContributionOccupancySet returned error: %v", err)
	}
	states[alpha.SubjectID()] = ContributionAmbiguous
	if state, covered := occupancy.State(alpha.SubjectID()); !covered || state != ContributionPresent {
		t.Fatalf("copied alpha state = %q covered=%t, want present", state, covered)
	}

	delete(states, beta.SubjectID())
	if _, err := NewContributionOccupancySet(contributions, states); err == nil ||
		!strings.Contains(err.Error(), "state count") {
		t.Fatalf("incomplete occupancy error = %v", err)
	}
	states[beta.SubjectID()] = "future"
	if _, err := NewContributionOccupancySet(contributions, states); err == nil ||
		!strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unknown occupancy error = %v", err)
	}
}
