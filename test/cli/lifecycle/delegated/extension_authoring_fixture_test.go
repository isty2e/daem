package cli_test

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func assertManagedClaudeCarrierRemoval(t *testing.T, actions []clijson.CarrierAbsenceAction) {
	t.Helper()
	if len(actions) != 1 {
		t.Fatalf("carrier absence actions = %#v, want one managed Claude removal", actions)
	}
	action := actions[0]
	if action.Subject == nil ||
		action.Subject.Kind != "host_relation" ||
		action.Subject.Namespace != "claude-code.plugin-carrier" ||
		action.Subject.Name != "context7-global" ||
		action.Kind != "carrier_absence" ||
		action.Target != "claude-code" ||
		action.Scope != "global" ||
		action.RequestedOutcome != "absent" ||
		action.SelectedAction != "remove" ||
		action.CorrelationState != "exact_correlation" ||
		action.EvidenceAvailability != "supported" ||
		action.EvidenceFreshness != "fresh" ||
		action.DaemKnownConsumerCount != 1 ||
		action.RemainingDaemKnownConsumers != 0 ||
		!action.InvokesHostRoute ||
		!action.RetiresClaim ||
		action.StateOnly ||
		action.BlocksOrdinaryApply ||
		action.RouteID != "claude-code.plugin-carrier.remove" ||
		action.RouteRequestHash == "" {
		t.Fatalf("managed Claude removal action = %#v", action)
	}
}

func assertRetainedClaudePluginFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		testkit.AssertFileContent(t, filepath.Join(root, path), content)
	}
}
