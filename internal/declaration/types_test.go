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

func TestTargetsEqualMembershipIgnoresOrderAndKeepsDuplicates(t *testing.T) {
	if !(Targets{"claude-code", "codex"}).EqualMembership(Targets{"codex", "claude-code"}) {
		t.Fatal("permuted targets were not equal by membership")
	}
	if (Targets{"codex", "codex"}).EqualMembership(Targets{"codex"}) {
		t.Fatal("duplicate multiplicity was ignored")
	}
	if (Targets{"codex"}).EqualMembership(Targets{"claude-code"}) {
		t.Fatal("disjoint targets compared equal")
	}
}
