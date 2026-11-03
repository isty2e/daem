package nativefixture

import (
	"testing"

	"github.com/isty2e/daem/test/testkit/clijson"
)

func RequireLifecycleClean(t *testing.T, output string) {
	t.Helper()
	status := clijson.DecodePlan(t, []byte(output))
	if status.HasErrors || status.CheckClean == nil || !*status.CheckClean {
		t.Fatal("persisted native lifecycle is not clean in real status", output)
	}
	for _, action := range status.Actions {
		if action.Kind != "noop" {
			t.Fatal("clean status still plans a state or host change", output)
		}
	}
}
