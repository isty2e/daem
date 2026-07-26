package declaration

import "testing"

func TestTargetsIntersectsUsesSetMembershipWithoutEmptyWildcard(t *testing.T) {
	if !(Targets{"codex", "codex"}).Intersects(Targets{"pi", "codex"}) {
		t.Fatal("shared target was not detected")
	}
	if Targets(nil).Intersects(Targets{"codex"}) || (Targets{"codex"}).Intersects(nil) {
		t.Fatal("empty targets behaved as a wildcard")
	}
	if (Targets{"codex"}).Intersects(Targets{"claude-code"}) {
		t.Fatal("disjoint targets intersected")
	}
}
