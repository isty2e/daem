package refine

import (
	"errors"
	"strings"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization/lock"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
)

func TestExtensionOrderConstraintsPreserveMembersAndSortClasses(t *testing.T) {
	values := []desiredextension.Extension{
		orderExtension(t, "pi-second", desiredextension.CarrierPiPackage, target.TargetPi, "pi-two"),
		orderExtension(t, "open-second", desiredextension.CarrierOpenCodePlugin, target.TargetOpenCode, "open-two"),
		orderExtension(t, "pi-first", desiredextension.CarrierPiPackage, target.TargetPi, "pi-one"),
		orderExtension(t, "open-first", desiredextension.CarrierOpenCodePlugin, target.TargetOpenCode, "open-one"),
	}

	constraints, err := ExtensionOrderConstraints(values, sourceRefOrderIdentity)
	if err != nil {
		t.Fatalf("ExtensionOrderConstraints returned error: %v", err)
	}
	if len(constraints) != 2 {
		t.Fatalf("constraints = %#v, want two", constraints)
	}
	if got := string(constraints[0].ClassID()); got != "extension:opencode:project:plugins" {
		t.Fatalf("constraint[0].ClassID = %q", got)
	}
	if got := string(constraints[1].ClassID()); got != "extension:pi:project:packages" {
		t.Fatalf("constraint[1].ClassID = %q", got)
	}
	assertOrderMemberNames(t, constraints[0], []string{"open-second", "open-first"})
	assertOrderMemberNames(t, constraints[1], []string{"pi-second", "pi-first"})
}

func TestExtensionOrderConstraintsOmitSingletonAndIgnoreUnadmittedCarrier(t *testing.T) {
	values := []desiredextension.Extension{
		orderExtension(t, "open-only", desiredextension.CarrierOpenCodePlugin, target.TargetOpenCode, "open-one"),
		orderExtension(t, "claude-one", desiredextension.CarrierClaudeCodePlugin, target.TargetClaudeCode, "one@market"),
		orderExtension(t, "claude-two", desiredextension.CarrierClaudeCodePlugin, target.TargetClaudeCode, "two@market"),
	}

	constraints, err := ExtensionOrderConstraints(values, sourceRefOrderIdentity)
	if err != nil {
		t.Fatalf("ExtensionOrderConstraints returned error: %v", err)
	}
	if len(constraints) != 0 {
		t.Fatalf("constraints = %#v, want none", constraints)
	}
	if _, err := ExtensionOrderConstraints(values[:1], nil); err != nil {
		t.Fatalf("singleton order class required resolver: %v", err)
	}
	if _, err := ExtensionOrderConstraints(values[1:], nil); err != nil {
		t.Fatalf("unadmitted extensions required resolver: %v", err)
	}
}

func TestExtensionOrderConstraintsRejectDuplicateHostIdentity(t *testing.T) {
	values := []desiredextension.Extension{
		orderExtension(t, "first", desiredextension.CarrierPiPackage, target.TargetPi, "same"),
		orderExtension(t, "second", desiredextension.CarrierPiPackage, target.TargetPi, "same"),
	}

	_, err := ExtensionOrderConstraints(values, sourceRefOrderIdentity)
	if err == nil || !strings.Contains(err.Error(), `host load identity "same" appears more than once`) {
		t.Fatalf("duplicate identity error = %v", err)
	}
}

func TestExtensionOrderConstraintsWrapIdentityResolverFailure(t *testing.T) {
	values := []desiredextension.Extension{
		orderExtension(t, "first", desiredextension.CarrierPiPackage, target.TargetPi, "one"),
		orderExtension(t, "second", desiredextension.CarrierPiPackage, target.TargetPi, "two"),
	}
	sentinel := errors.New("identity unavailable")

	_, err := ExtensionOrderConstraints(
		values,
		func(desiredextension.CarrierKey) (hostrelation.HostLoadIdentity, error) {
			return "", sentinel
		},
	)
	if !errors.Is(err, sentinel) || !strings.Contains(err.Error(), "extension[0]") {
		t.Fatalf("resolver error = %v", err)
	}
}

func TestValidateCurrentExtensionOrderAcceptsExactManifestOrder(t *testing.T) {
	extensions := []desiredextension.Extension{
		orderExtension(t, "first", desiredextension.CarrierPiPackage, target.TargetPi, "one"),
		orderExtension(t, "second", desiredextension.CarrierPiPackage, target.TargetPi, "two"),
	}
	file := extensionOrderFile(t, extensions)

	if err := ValidateCurrentExtensionOrder(extensions, file, sourceRefOrderIdentity); err != nil {
		t.Fatalf("ValidateCurrentExtensionOrder returned error: %v", err)
	}
}

func TestValidateCurrentExtensionOrderRejectsReorderedManifest(t *testing.T) {
	extensions := []desiredextension.Extension{
		orderExtension(t, "first", desiredextension.CarrierPiPackage, target.TargetPi, "one"),
		orderExtension(t, "second", desiredextension.CarrierPiPackage, target.TargetPi, "two"),
	}
	file := extensionOrderFile(t, extensions)
	reordered := []desiredextension.Extension{extensions[1], extensions[0]}

	err := ValidateCurrentExtensionOrder(reordered, file, sourceRefOrderIdentity)
	if !errors.Is(err, ErrStaleExtensionOrder) ||
		!strings.Contains(err.Error(), "run daem lock") {
		t.Fatalf("reordered manifest error = %v", err)
	}
}

func TestValidateCurrentExtensionOrderRejectsClassCountChange(t *testing.T) {
	extensions := []desiredextension.Extension{
		orderExtension(t, "first", desiredextension.CarrierPiPackage, target.TargetPi, "one"),
		orderExtension(t, "second", desiredextension.CarrierPiPackage, target.TargetPi, "two"),
	}
	file := extensionOrderFile(t, extensions)

	err := ValidateCurrentExtensionOrder(extensions[:1], file, sourceRefOrderIdentity)
	if !errors.Is(err, ErrStaleExtensionOrder) ||
		!strings.Contains(err.Error(), "manifest has 0 order classes but lockfile has 1") {
		t.Fatalf("class count error = %v", err)
	}
}

func extensionOrderFile(
	t *testing.T,
	extensions []desiredextension.Extension,
) lock.File {
	t.Helper()
	subjects, err := Extensions(extensions)
	if err != nil {
		t.Fatalf("Extensions returned error: %v", err)
	}
	constraints, err := ExtensionOrderConstraints(extensions, sourceRefOrderIdentity)
	if err != nil {
		t.Fatalf("ExtensionOrderConstraints returned error: %v", err)
	}
	section, err := lock.NewLockedSection(subjects, constraints)
	if err != nil {
		t.Fatalf("NewLockedSection returned error: %v", err)
	}
	return lock.File{Version: lock.CurrentVersion, Locked: section}
}

func orderExtension(
	t *testing.T,
	name string,
	carrier desiredextension.Carrier,
	selectedTarget target.Target,
	source string,
) desiredextension.Extension {
	t.Helper()
	sourceKind := desiredextension.SourceKindHostSource
	if carrier == desiredextension.CarrierClaudeCodePlugin {
		sourceKind = desiredextension.SourceKindMarketplace
	}
	return desiredtest.Extension(t, desiredextension.Spec{
		Name:    name,
		Carrier: carrier,
		Target:  selectedTarget,
		Scope:   target.ScopeProject,
		Source:  desiredtest.ExtensionSource(t, sourceKind, source),
	})
}

func sourceRefOrderIdentity(
	value desiredextension.CarrierKey,
) (hostrelation.HostLoadIdentity, error) {
	return hostrelation.NewHostLoadIdentity(value.Source().Ref())
}

func assertOrderMemberNames(
	t *testing.T,
	constraint hostrelation.RelationOrderConstraint,
	want []string,
) {
	t.Helper()
	members := constraint.Members()
	if len(members) != len(want) {
		t.Fatalf("members = %#v, want names %v", members, want)
	}
	for index, name := range want {
		if got := members[index].Subject().Key(); got != name {
			t.Fatalf("member[%d] = %q, want %q", index, got, name)
		}
	}
}
