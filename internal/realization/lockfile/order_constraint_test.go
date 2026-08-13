package lockfile

import (
	"bytes"
	"crypto/sha256"
	"strings"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/refine"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
)

func TestMarshalAndLoadOrderConstraintsPreserveAuthoredMemberOrder(t *testing.T) {
	extensions := []desiredextension.Extension{
		lockfileOrderExtension(t, "second", desiredextension.CarrierOpenCodePlugin, target.TargetOpenCode, "@acme/second"),
		lockfileOrderExtension(t, "first", desiredextension.CarrierOpenCodePlugin, target.TargetOpenCode, "@acme/first"),
	}
	file := orderLockfile(t, extensions)

	content, err := Marshal(file)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	rendered := string(content)
	assertInOrder(t, rendered, []string{
		currentLockfileVersionEnvelope(),
		"[[locked.subject]]",
		"[[locked.order_constraint]]",
		`class_id = "extension:opencode:project:plugins"`,
		`contract_version = "opencode-plugin-package-v1"`,
		`runtime_meaning = "config-order-only"`,
		"[[locked.order_constraint.member]]",
		`subject_id = "host_relation/opencode.plugin-carrier/second"`,
		`host_load_identity = "@acme/second"`,
		"[[locked.order_constraint.member]]",
		`subject_id = "host_relation/opencode.plugin-carrier/first"`,
		`host_load_identity = "@acme/first"`,
	})

	loaded, err := Load(t.Context(), writeLockfileText(t, rendered))
	if err != nil {
		t.Fatalf("Load returned error: %v\n%s", err, rendered)
	}
	constraints := loaded.Locked.OrderConstraints()
	if len(constraints) != 1 {
		t.Fatalf("constraints = %#v, want one", constraints)
	}
	members := constraints[0].Members()
	if members[0].Subject().Key() != "second" || members[1].Subject().Key() != "first" {
		t.Fatalf("member order changed: %#v", members)
	}
}

func TestOrderOnlyChangeChangesLockBytesWithoutChangingSubjectIdentity(t *testing.T) {
	first := lockfileOrderExtension(
		t,
		"first",
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		"@acme/first",
	)
	second := lockfileOrderExtension(
		t,
		"second",
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		"@acme/second",
	)
	forward := orderLockfile(t, []desiredextension.Extension{first, second})
	reverse := orderLockfile(t, []desiredextension.Extension{second, first})

	forwardContent, err := Marshal(forward)
	if err != nil {
		t.Fatalf("Marshal(forward) returned error: %v", err)
	}
	reverseContent, err := Marshal(reverse)
	if err != nil {
		t.Fatalf("Marshal(reverse) returned error: %v", err)
	}
	if bytes.Equal(forwardContent, reverseContent) {
		t.Fatal("order-only change did not change lockfile bytes")
	}
	if sha256.Sum256(forwardContent) == sha256.Sum256(reverseContent) {
		t.Fatal("order-only change did not change lockfile digest")
	}

	forwardSubjects := forward.Locked.Subjects()
	reverseSubjects := reverse.Locked.Subjects()
	if len(forwardSubjects) != len(reverseSubjects) {
		t.Fatalf("subject counts differ: %d != %d", len(forwardSubjects), len(reverseSubjects))
	}
	for index := range forwardSubjects {
		if !forwardSubjects[index].Equal(reverseSubjects[index]) {
			t.Fatalf("subject[%d] identity or route changed under reorder", index)
		}
	}
}

func TestLoadRejectsMalformedOrderConstraintShapesAndFacts(t *testing.T) {
	extensions := []desiredextension.Extension{
		lockfileOrderExtension(t, "first", desiredextension.CarrierPiPackage, target.TargetPi, "first"),
		lockfileOrderExtension(t, "second", desiredextension.CarrierPiPackage, target.TargetPi, "second"),
	}
	canonical := string(marshalLockfileForTest(t, orderLockfile(t, extensions)))

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "inline constraint",
			content: strings.Replace(
				canonical,
				"[[locked.order_constraint]]",
				"[locked.order_constraint]",
				1,
			),
			want: "locked.order_constraint must use [[locked.order_constraint]]",
		},
		{
			name: "inline member",
			content: strings.Replace(
				canonical,
				"[[locked.order_constraint.member]]",
				"[locked.order_constraint.member]",
				1,
			),
			want: "already created and cannot be used as an array",
		},
		{
			name: "wrong contract",
			content: strings.Replace(
				canonical,
				`contract_version = "pi-package-load-identity-v1"`,
				`contract_version = "future-v99"`,
				1,
			),
			want: "does not match profile",
		},
		{
			name: "wrong runtime",
			content: strings.Replace(
				canonical,
				`runtime_meaning = "runtime-precedence"`,
				`runtime_meaning = "config-order-only"`,
				1,
			),
			want: "does not match profile",
		},
		{
			name: "unknown member key",
			content: strings.Replace(
				canonical,
				`host_load_identity = "first"`,
				"host_load_identity = \"first\"\nfuture = true",
				1,
			),
			want: "unknown lockfile key",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.content == canonical {
				t.Fatal("tampered fixture matches canonical lockfile")
			}
			_, err := Load(t.Context(), writeLockfileText(t, test.content))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Load error = %v, want %q\n%s", err, test.want, test.content)
			}
		})
	}
}

func TestOrderConstraintsFromDTORejectsNonCanonicalClassOrder(t *testing.T) {
	member := lockedOrderMemberDTO{
		SubjectID:        "host_relation/example/first",
		HostLoadIdentity: "first",
	}
	row := func(classID string) lockedOrderConstraintDTO {
		return lockedOrderConstraintDTO{
			ClassID:         classID,
			ContractVersion: "example-v1",
			RuntimeMeaning:  string(hostrelation.RuntimeUnknown),
			Members:         []lockedOrderMemberDTO{member},
		}
	}

	tests := []struct {
		name string
		rows []lockedOrderConstraintDTO
	}{
		{name: "descending", rows: []lockedOrderConstraintDTO{row("z"), row("a")}},
		{name: "duplicate", rows: []lockedOrderConstraintDTO{row("a"), row("a")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := orderConstraintsFromDTO(test.rows)
			if err == nil || !strings.Contains(err.Error(), "not in canonical order") {
				t.Fatalf("error = %v, want canonical-order rejection", err)
			}
		})
	}
}

func orderLockfile(t *testing.T, extensions []desiredextension.Extension) lock.File {
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
	section, err := lock.NewLockedSection(subjects, constraints)
	if err != nil {
		t.Fatalf("NewLockedSection returned error: %v", err)
	}
	return lock.File{Version: lock.CurrentVersion, Locked: section}
}

func lockfileOrderExtension(
	t *testing.T,
	name string,
	carrier desiredextension.Carrier,
	selectedTarget target.Target,
	source string,
) desiredextension.Extension {
	t.Helper()
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
