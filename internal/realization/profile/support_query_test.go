package profile

import (
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
)

func TestTargetSupportsMatchesProfile(t *testing.T) {
	t.Parallel()

	for _, selectedTarget := range target.SupportedTargets() {
		for _, resourceKind := range resourceKinds {
			got := TargetSupports(selectedTarget, resourceKind)
			want := Profile(selectedTarget).Supports(resourceKind)
			if got != want {
				t.Fatalf(
					"TargetSupports(%q, %q) = %t, Profile().Supports = %t",
					selectedTarget,
					resourceKind,
					got,
					want,
				)
			}
		}
	}
	if TargetSupports(target.Target("unknown-target"), entity.KindSkill) {
		t.Fatal("unknown target must not support skills")
	}
}
