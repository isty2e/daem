package lock_test

import (
	"strings"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestLockedSectionCanonicalizesOrderClassesWithoutSortingMembers(t *testing.T) {
	openCode := []desiredextension.Extension{
		lockedOrderExtension(t, "open-second", desiredextension.CarrierOpenCodePlugin, target.TargetOpenCode, "open-two"),
		lockedOrderExtension(t, "open-first", desiredextension.CarrierOpenCodePlugin, target.TargetOpenCode, "open-one"),
	}
	pi := []desiredextension.Extension{
		lockedOrderExtension(t, "pi-second", desiredextension.CarrierPiPackage, target.TargetPi, "pi-two"),
		lockedOrderExtension(t, "pi-first", desiredextension.CarrierPiPackage, target.TargetPi, "pi-one"),
	}
	extensions := append(append([]desiredextension.Extension(nil), pi...), openCode...)
	subjects, constraints := lockedOrderFixture(t, extensions)
	constraints[0], constraints[1] = constraints[1], constraints[0]

	section, err := lock.NewLockedSection(subjects, constraints)
	if err != nil {
		t.Fatalf("NewLockedSection returned error: %v", err)
	}
	got := section.OrderConstraints()
	if len(got) != 2 ||
		got[0].ClassID() >= got[1].ClassID() {
		t.Fatalf("order constraints are not class-sorted: %#v", got)
	}
	if got[0].Members()[0].Subject().Key() != "open-second" ||
		got[0].Members()[1].Subject().Key() != "open-first" {
		t.Fatalf("OpenCode member order changed: %#v", got[0].Members())
	}

	got[0] = hostrelation.RelationOrderConstraint{}
	if section.OrderConstraints()[0].ClassID() == "" {
		t.Fatal("OrderConstraints returned storage alias")
	}
}

func TestLockedSectionRequiresConstraintForMultiMemberOrderClass(t *testing.T) {
	extensions := []desiredextension.Extension{
		lockedOrderExtension(t, "first", desiredextension.CarrierPiPackage, target.TargetPi, "one"),
		lockedOrderExtension(t, "second", desiredextension.CarrierPiPackage, target.TargetPi, "two"),
	}
	subjects, _ := lockedOrderFixture(t, extensions)

	_, err := lock.NewLockedSection(subjects, nil)
	if err == nil || !strings.Contains(err.Error(), "requires an order constraint") {
		t.Fatalf("missing constraint error = %v", err)
	}
}

func TestLockedSectionRejectsOrderConstraintProfileDrift(t *testing.T) {
	extensions := []desiredextension.Extension{
		lockedOrderExtension(t, "first", desiredextension.CarrierPiPackage, target.TargetPi, "one"),
		lockedOrderExtension(t, "second", desiredextension.CarrierPiPackage, target.TargetPi, "two"),
	}
	subjects, constraints := lockedOrderFixture(t, extensions)
	canonical := constraints[0]

	tests := []struct {
		name       string
		contract   string
		runtime    hostrelation.RuntimeMeaning
		wantPhrase string
	}{
		{
			name:       "identity contract",
			contract:   "future-contract-v99",
			runtime:    canonical.RuntimeMeaning(),
			wantPhrase: "does not match profile",
		},
		{
			name:       "runtime meaning",
			contract:   canonical.MemberIdentityContract(),
			runtime:    hostrelation.ConfigOrderOnly,
			wantPhrase: "does not match profile",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			drifted, err := hostrelation.NewRelationOrderConstraint(
				canonical.ClassID(),
				test.contract,
				test.runtime,
				canonical.Members(),
			)
			if err != nil {
				t.Fatalf("NewRelationOrderConstraint returned error: %v", err)
			}
			_, err = lock.NewLockedSection(subjects, []hostrelation.RelationOrderConstraint{drifted})
			if err == nil || !strings.Contains(err.Error(), test.wantPhrase) {
				t.Fatalf("profile drift error = %v", err)
			}
		})
	}
}

func TestLockedSectionRejectsIncompleteAndDuplicateOrderClasses(t *testing.T) {
	extensions := []desiredextension.Extension{
		lockedOrderExtension(t, "first", desiredextension.CarrierOpenCodePlugin, target.TargetOpenCode, "one"),
		lockedOrderExtension(t, "second", desiredextension.CarrierOpenCodePlugin, target.TargetOpenCode, "two"),
	}
	subjects, constraints := lockedOrderFixture(t, extensions)
	canonical := constraints[0]
	incomplete, err := hostrelation.NewRelationOrderConstraint(
		canonical.ClassID(),
		canonical.MemberIdentityContract(),
		canonical.RuntimeMeaning(),
		canonical.Members()[:1],
	)
	if err != nil {
		t.Fatalf("NewRelationOrderConstraint returned error: %v", err)
	}

	if _, err := lock.NewLockedSection(subjects, []hostrelation.RelationOrderConstraint{incomplete}); err == nil ||
		!strings.Contains(err.Error(), "want exactly 2") {
		t.Fatalf("incomplete class error = %v", err)
	}
	if _, err := lock.NewLockedSection(
		subjects,
		[]hostrelation.RelationOrderConstraint{canonical, canonical},
	); err == nil || !strings.Contains(err.Error(), "appears more than once") {
		t.Fatalf("duplicate class error = %v", err)
	}
}

func TestLockedSectionRejectsDanglingCrossClassAndSingletonOrderMembers(t *testing.T) {
	piExtensions := []desiredextension.Extension{
		lockedOrderExtension(t, "pi-first", desiredextension.CarrierPiPackage, target.TargetPi, "pi-first"),
		lockedOrderExtension(t, "pi-second", desiredextension.CarrierPiPackage, target.TargetPi, "pi-second"),
	}
	openCodeExtension := lockedOrderExtension(
		t,
		"open-only",
		desiredextension.CarrierOpenCodePlugin,
		target.TargetOpenCode,
		"open-only",
	)

	t.Run("dangling member", func(t *testing.T) {
		subjects, constraints := lockedOrderFixture(t, piExtensions)
		ghostSubjects, _ := lockedOrderFixture(t, []desiredextension.Extension{
			lockedOrderExtension(t, "ghost", desiredextension.CarrierPiPackage, target.TargetPi, "ghost"),
		})
		ghost := lockedOrderMember(t, ghostSubjects[0].SubjectID(), "ghost")
		members := constraints[0].Members()
		members[1] = ghost
		malformed := lockedOrderConstraint(t, constraints[0], members)

		_, err := lock.NewLockedSection(
			subjects,
			[]hostrelation.RelationOrderConstraint{malformed},
		)
		if err == nil || !strings.Contains(err.Error(), "references missing subject") {
			t.Fatalf("dangling member error = %v", err)
		}
	})

	t.Run("cross-class member", func(t *testing.T) {
		extensions := append(append(
			[]desiredextension.Extension(nil),
			piExtensions...,
		), openCodeExtension)
		subjects, constraints := lockedOrderFixture(t, extensions)
		var openCodeSubject topology.SubjectID
		for _, subject := range subjects {
			if subject.EntityID().Name() == "open-only" {
				openCodeSubject = subject.SubjectID()
				break
			}
		}
		if openCodeSubject == (topology.SubjectID{}) {
			t.Fatal("OpenCode fixture subject not found")
		}
		members := constraints[0].Members()
		members[1] = lockedOrderMember(t, openCodeSubject, "open-only")
		malformed := lockedOrderConstraint(t, constraints[0], members)

		_, err := lock.NewLockedSection(
			subjects,
			[]hostrelation.RelationOrderConstraint{malformed},
		)
		if err == nil || !strings.Contains(err.Error(), "belongs to another order class") {
			t.Fatalf("cross-class member error = %v", err)
		}
	})

	t.Run("singleton class", func(t *testing.T) {
		subjects, _ := lockedOrderFixture(t, piExtensions[:1])
		capability, admitted := profile.Profile(target.TargetPi).ExtensionOrder(
			desiredextension.CarrierPiPackage,
			target.ScopeProject,
		)
		if !admitted {
			t.Fatal("Pi project order capability not admitted")
		}
		constraint, err := hostrelation.NewRelationOrderConstraint(
			capability.ClassID(),
			capability.MemberIdentityContract(),
			capability.RuntimeMeaning(),
			[]hostrelation.RelationOrderMember{
				lockedOrderMember(t, subjects[0].SubjectID(), "pi-first"),
			},
		)
		if err != nil {
			t.Fatalf("NewRelationOrderConstraint returned error: %v", err)
		}

		_, err = lock.NewLockedSection(
			subjects,
			[]hostrelation.RelationOrderConstraint{constraint},
		)
		if err == nil || !strings.Contains(err.Error(), "requires at least two locked members") {
			t.Fatalf("singleton class error = %v", err)
		}
	})
}

func TestValidateExtensionOrderIdentitiesRejectsContextualMismatch(t *testing.T) {
	extensions := []desiredextension.Extension{
		lockedOrderExtension(t, "first", desiredextension.CarrierOpenCodePlugin, target.TargetOpenCode, "one"),
		lockedOrderExtension(t, "second", desiredextension.CarrierOpenCodePlugin, target.TargetOpenCode, "two"),
	}
	subjects, constraints := lockedOrderFixture(t, extensions)
	section, err := lock.NewLockedSection(subjects, constraints)
	if err != nil {
		t.Fatalf("NewLockedSection returned error: %v", err)
	}
	file := lock.File{Version: lock.CurrentVersion, Locked: section}

	if err := lock.ValidateExtensionOrderIdentities(
		file,
		func(value desiredextension.CarrierKey) (hostrelation.HostLoadIdentity, error) {
			return hostrelation.NewHostLoadIdentity(value.Source().Ref())
		},
	); err != nil {
		t.Fatalf("canonical identity validation failed: %v", err)
	}
	err = lock.ValidateExtensionOrderIdentities(
		file,
		func(desiredextension.CarrierKey) (hostrelation.HostLoadIdentity, error) {
			return hostrelation.NewHostLoadIdentity("different")
		},
	)
	if err == nil || !strings.Contains(err.Error(), "does not match derived identity") {
		t.Fatalf("identity mismatch error = %v", err)
	}
}

func lockedOrderConstraint(
	t *testing.T,
	canonical hostrelation.RelationOrderConstraint,
	members []hostrelation.RelationOrderMember,
) hostrelation.RelationOrderConstraint {
	t.Helper()
	constraint, err := hostrelation.NewRelationOrderConstraint(
		canonical.ClassID(),
		canonical.MemberIdentityContract(),
		canonical.RuntimeMeaning(),
		members,
	)
	if err != nil {
		t.Fatalf("NewRelationOrderConstraint returned error: %v", err)
	}
	return constraint
}

func lockedOrderMember(
	t *testing.T,
	subject topology.SubjectID,
	identity string,
) hostrelation.RelationOrderMember {
	t.Helper()
	hostIdentity, err := hostrelation.NewHostLoadIdentity(identity)
	if err != nil {
		t.Fatalf("NewHostLoadIdentity returned error: %v", err)
	}
	member, err := hostrelation.NewRelationOrderMember(subject, hostIdentity)
	if err != nil {
		t.Fatalf("NewRelationOrderMember returned error: %v", err)
	}
	return member
}

func lockedOrderFixture(
	t *testing.T,
	extensions []desiredextension.Extension,
) ([]lock.LockedSubjectContract, []hostrelation.RelationOrderConstraint) {
	t.Helper()
	subjects, err := refine.Extensions(extensions)
	if err != nil {
		t.Fatalf("refine.Extensions returned error: %v", err)
	}
	constraints, err := refine.ExtensionOrderConstraints(
		extensions,
		func(value desiredextension.CarrierKey) (hostrelation.HostLoadIdentity, error) {
			return hostrelation.NewHostLoadIdentity(value.Source().Ref())
		},
	)
	if err != nil {
		t.Fatalf("ExtensionOrderConstraints returned error: %v", err)
	}
	return subjects, constraints
}

func lockedOrderExtension(
	t *testing.T,
	name string,
	carrier desiredextension.Carrier,
	selectedTarget target.Target,
	source string,
) desiredextension.Extension {
	t.Helper()
	if _, admitted := profile.Profile(selectedTarget).ExtensionOrder(carrier, target.ScopeProject); !admitted {
		t.Fatalf("test carrier %q is not order-admitted", carrier)
	}
	return desiredtest.Extension(t, desiredextension.Spec{
		Name:    name,
		Carrier: carrier,
		Target:  selectedTarget,
		Scope:   target.ScopeProject,
		Source: desiredtest.ExtensionSource(
			t,
			desiredextension.SourceKindHostSource,
			source,
		),
	})
}
