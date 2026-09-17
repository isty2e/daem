package hostroute

import (
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/desired/extension"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/target"
)

func TestPinTransitionExclusivityIncludesUnclaimedDesiredPins(t *testing.T) {
	before := "git:github.com/example/package@" + strings.Repeat("a", 40)
	after := "git:github.com/example/package@" + strings.Repeat("b", 40)
	old := pinRelationContract(t, "tools", before, target.ScopeGlobal)
	next := pinRelationContract(t, "tools", after, target.ScopeGlobal)
	claim := retainedCarrierFixtureFor(t, statusLockfileFromRecords(t, old)).claim
	pair, err := claim.PinTransitionTo(next)
	if err != nil {
		t.Fatal(err)
	}
	input := RelationInput{CurrentOwner: claim.Owner(), ManagedClaims: []durablecarrier.ManagedCarrierClaim{claim}}
	if !pinTransitionExclusive(input, []carrierRelationRecord{{contract: next}}, pair) {
		t.Fatal("sole admitted pin was not exclusive")
	}
	for _, test := range []struct {
		name, source string
		scope        target.Scope
		want         bool
	}{
		{"same_pin", before, target.ScopeGlobal, false},
		{"new_pin", after, target.ScopeGlobal, false},
		{"alias", strings.Replace(before, "git:github.com/", "git:https://github.com/", 1), target.ScopeGlobal, false},
		{"other_scope", before, target.ScopeProject, true},
		{"other_repository", strings.Replace(before, "example/package", "example/unrelated", 1), target.ScopeGlobal, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			other := pinRelationContract(t, "second", test.source, test.scope)
			records := []carrierRelationRecord{{contract: next}, {contract: other}}
			if got := pinTransitionExclusive(input, records, pair); got != test.want {
				t.Fatalf("desired-consumer exclusivity=%t, want %t", got, test.want)
			}
		})
	}
}

func pinRelationContract(t *testing.T, name, source string, scope target.Scope) lock.LockedSubjectContract {
	t.Helper()
	value := desiredtest.Extension(t, extension.Spec{Name: name, Carrier: extension.CarrierPiPackage, Target: target.TargetPi, Scope: scope, Source: desiredtest.ExtensionSource(t, extension.SourceKindHostSource, source)})
	file, _ := snapshottest.ExtensionCarrierFile(t, value)
	return file.Locked.Subjects()[0]
}
