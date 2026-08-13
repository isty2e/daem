package merge

import (
	"reflect"
	"testing"

	targetpkg "github.com/isty2e/daem/internal/target"
)

func TestMissingImportTargetsUseStableProductOrder(t *testing.T) {
	got := missingCanonicalImportTargets(
		[]targetpkg.Target{targetpkg.TargetCodex},
		[]targetpkg.Target{
			targetpkg.TargetPi,
			targetpkg.TargetCodex,
			targetpkg.TargetClaudeCode,
			targetpkg.TargetPi,
		},
	)
	want := []targetpkg.Target{targetpkg.TargetClaudeCode, targetpkg.TargetPi}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("missingCanonicalImportTargets = %#v, want %#v", got, want)
	}
}
