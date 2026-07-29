package hostrelation_test

import (
	"strings"
	"testing"

	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/topology"
)

func TestRelationOrderConstraintPreservesRelativeOrderAndSeparatesItsFingerprint(t *testing.T) {
	alpha := mustOrderMember(t, "alpha", "@acme/alpha")
	beta := mustOrderMember(t, "beta", "@acme/beta")

	forward := mustOrderConstraint(t, []hostrelation.RelationOrderMember{alpha, beta})
	reverse := mustOrderConstraint(t, []hostrelation.RelationOrderMember{beta, alpha})

	if forward.Fingerprint() == "" || forward.Fingerprint() == reverse.Fingerprint() {
		t.Fatalf("order fingerprints = %q and %q", forward.Fingerprint(), reverse.Fingerprint())
	}
	if got := forward.Members(); got[0] != alpha || got[1] != beta {
		t.Fatalf("members = %#v, want authored order", got)
	}
}

func TestRelationOrderConstraintRejectsDuplicateSubjectOrHostLoadIdentity(t *testing.T) {
	alpha := mustOrderMember(t, "alpha", "@acme/alpha")
	sameSubject := mustOrderMember(t, "alpha", "@acme/other")
	sameLoadIdentity := mustOrderMember(t, "other", "@acme/alpha")

	tests := []struct {
		name    string
		members []hostrelation.RelationOrderMember
		want    string
	}{
		{name: "duplicate subject", members: []hostrelation.RelationOrderMember{alpha, sameSubject}, want: "subject"},
		{name: "duplicate load identity", members: []hostrelation.RelationOrderMember{alpha, sameLoadIdentity}, want: "host load identity"},
		{name: "empty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			classID := mustOrderClassID(t)
			_, err := hostrelation.NewRelationOrderConstraint(
				classID,
				"opencode-plugin-package-v1",
				hostrelation.ConfigOrderOnly,
				test.members,
			)
			if err == nil {
				t.Fatal("NewRelationOrderConstraint returned nil error")
			}
			if test.want != "" && !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %q, want %q", err, test.want)
			}
		})
	}
}

func TestRelationOrderConstraintDefensivelyCopiesMembers(t *testing.T) {
	input := []hostrelation.RelationOrderMember{
		mustOrderMember(t, "alpha", "@acme/alpha"),
		mustOrderMember(t, "beta", "@acme/beta"),
	}
	constraint := mustOrderConstraint(t, input)
	fingerprint := constraint.Fingerprint()

	input[0] = hostrelation.RelationOrderMember{}
	returned := constraint.Members()
	returned[0] = hostrelation.RelationOrderMember{}

	if constraint.Validate() != nil || constraint.Fingerprint() != fingerprint {
		t.Fatal("caller mutation changed the order constraint")
	}
}

func TestRelationOrderScalarsRejectInvalidValuesAndSubjectKinds(t *testing.T) {
	if _, err := hostrelation.NewOrderClassID(" "); err == nil {
		t.Fatal("NewOrderClassID accepted whitespace")
	}
	if _, err := hostrelation.NewPhysicalSequenceID("sequence\nid"); err == nil {
		t.Fatal("NewPhysicalSequenceID accepted a control character")
	}
	if _, err := hostrelation.NewHostLoadIdentity(""); err == nil {
		t.Fatal("NewHostLoadIdentity accepted empty identity")
	}
	if err := hostrelation.RuntimeMeaning("probable").Validate(); err == nil {
		t.Fatal("RuntimeMeaning.Validate accepted an open value")
	}

	resource, err := topology.NewSubjectID(topology.SubjectResource, "test", "resource")
	if err != nil {
		t.Fatal(err)
	}
	identity := mustHostLoadIdentity(t, "@acme/resource")
	if _, err := hostrelation.NewRelationOrderMember(resource, identity); err == nil {
		t.Fatal("NewRelationOrderMember accepted a non-relation subject")
	}
}

func mustOrderConstraint(
	t *testing.T,
	members []hostrelation.RelationOrderMember,
) hostrelation.RelationOrderConstraint {
	t.Helper()
	constraint, err := hostrelation.NewRelationOrderConstraint(
		mustOrderClassID(t),
		"opencode-plugin-package-v1",
		hostrelation.ConfigOrderOnly,
		members,
	)
	if err != nil {
		t.Fatalf("NewRelationOrderConstraint: %v", err)
	}
	return constraint
}

func mustOrderMember(t *testing.T, subjectKey string, loadIdentity string) hostrelation.RelationOrderMember {
	t.Helper()
	subject, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"opencode.plugin-carrier",
		subjectKey,
	)
	if err != nil {
		t.Fatalf("topology.NewSubjectID: %v", err)
	}
	member, err := hostrelation.NewRelationOrderMember(
		subject,
		mustHostLoadIdentity(t, loadIdentity),
	)
	if err != nil {
		t.Fatalf("NewRelationOrderMember: %v", err)
	}
	return member
}

func mustOrderClassID(t *testing.T) hostrelation.OrderClassID {
	t.Helper()
	id, err := hostrelation.NewOrderClassID("extension:opencode:project:plugins")
	if err != nil {
		t.Fatalf("NewOrderClassID: %v", err)
	}
	return id
}

func mustHostLoadIdentity(t *testing.T, value string) hostrelation.HostLoadIdentity {
	t.Helper()
	identity, err := hostrelation.NewHostLoadIdentity(value)
	if err != nil {
		t.Fatalf("NewHostLoadIdentity: %v", err)
	}
	return identity
}
