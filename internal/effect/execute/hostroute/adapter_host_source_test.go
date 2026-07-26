package hostroute

import (
	"path/filepath"
	"slices"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
)

func TestBuildCommandBuildsOpenCodeHostSourceAttempts(t *testing.T) {
	tests := []struct {
		name     string
		scope    target.Scope
		wantArgs []string
	}{
		{
			name:     "project",
			scope:    target.ScopeProject,
			wantArgs: []string{"plugin", "@acme/opencode-formatter"},
		},
		{
			name:     "global",
			scope:    target.ScopeGlobal,
			wantArgs: []string{"plugin", "@acme/opencode-formatter", "--global"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workDir := filepath.Join(t.TempDir(), "project with spaces")
			command := mustBuildOpenCodeCommand(t, hostSourceRouteFixture{
				sourceRef:  "@acme/opencode-formatter",
				subjectKey: "@acme/opencode-formatter",
				scope:      test.scope,
				workDir:    workDir,
			})

			attempt := command.AttemptRequest()
			if attempt.Command != "opencode" {
				t.Fatalf("command = %q, want opencode", attempt.Command)
			}
			if !slices.Equal(attempt.Args, test.wantArgs) {
				t.Fatalf("args = %#v, want %#v", attempt.Args, test.wantArgs)
			}
			if attempt.WorkDir != workDir {
				t.Fatalf("workdir = %q, want %q", attempt.WorkDir, workDir)
			}
		})
	}
}

func TestBuildCommandBuildsPiHostSourceAttempts(t *testing.T) {
	tests := []struct {
		name     string
		scope    target.Scope
		wantArgs []string
	}{
		{
			name:     "project",
			scope:    target.ScopeProject,
			wantArgs: []string{"install", "github:acme/pi-tools", "-l"},
		},
		{
			name:     "global",
			scope:    target.ScopeGlobal,
			wantArgs: []string{"install", "github:acme/pi-tools"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workDir := filepath.Join(t.TempDir(), "project with spaces")
			command := mustBuildPiCommand(t, hostSourceRouteFixture{
				sourceRef:  "github:acme/pi-tools",
				subjectKey: "github:acme/pi-tools",
				scope:      test.scope,
				workDir:    workDir,
			})

			attempt := command.AttemptRequest()
			if attempt.Command != "pi" {
				t.Fatalf("command = %q, want pi", attempt.Command)
			}
			if !slices.Equal(attempt.Args, test.wantArgs) {
				t.Fatalf("args = %#v, want %#v", attempt.Args, test.wantArgs)
			}
			if attempt.WorkDir != workDir {
				t.Fatalf("workdir = %q, want %q", attempt.WorkDir, workDir)
			}
		})
	}
}

func TestBuildCommandBuildsAntigravityCLIHostSourceAttempts(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "project with spaces")
	command := mustBuildAntigravityCLICommand(t, hostSourceRouteFixture{
		sourceRef:  "modern-web-guidance@google",
		subjectKey: "modern-web-guidance@google",
		scope:      target.ScopeGlobal,
		workDir:    workDir,
	})

	attempt := command.AttemptRequest()
	if attempt.Command != "agy" {
		t.Fatalf("command = %q, want agy", attempt.Command)
	}
	wantArgs := []string{"plugin", "install", "modern-web-guidance@google"}
	if !slices.Equal(attempt.Args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", attempt.Args, wantArgs)
	}
	if attempt.WorkDir != workDir {
		t.Fatalf("workdir = %q, want %q", attempt.WorkDir, workDir)
	}
}

type hostSourceRouteFixture struct {
	sourceKind    desiredextension.SourceKind
	sourceRef     string
	subjectKey    string
	scope         target.Scope
	workDir       string
	inventorySpec observerelation.InventorySpec
}

func mustBuildOpenCodeCommand(t *testing.T, spec hostSourceRouteFixture) Command {
	t.Helper()
	fixture := newOpenCodeHostRouteFixture(t, spec)
	command, err := BuildCommand(BuildInput{
		Action:   fixture.action,
		Lockfile: fixture.lockfile,
		WorkDir:  fixture.workDir,
	})
	if err != nil {
		t.Fatalf("BuildCommand returned error: %v", err)
	}
	return command
}

func mustBuildPiCommand(t *testing.T, spec hostSourceRouteFixture) Command {
	t.Helper()
	fixture := newPiHostRouteFixture(t, spec)
	command, err := BuildCommand(BuildInput{
		Action:   fixture.action,
		Lockfile: fixture.lockfile,
		WorkDir:  fixture.workDir,
	})
	if err != nil {
		t.Fatalf("BuildCommand returned error: %v", err)
	}
	return command
}

func mustBuildAntigravityCLICommand(t *testing.T, spec hostSourceRouteFixture) Command {
	t.Helper()
	fixture := newAntigravityCLIHostRouteFixture(t, spec)
	command, err := BuildCommand(BuildInput{
		Action:   fixture.action,
		Lockfile: fixture.lockfile,
		WorkDir:  fixture.workDir,
	})
	if err != nil {
		t.Fatalf("BuildCommand returned error: %v", err)
	}
	return command
}

func newOpenCodeHostRouteFixture(t *testing.T, spec hostSourceRouteFixture) builtFixture {
	t.Helper()
	sourceKind := spec.sourceKind
	if sourceKind == "" {
		sourceKind = desiredextension.SourceKindHostSource
	}
	record, subject := mustCarrierRecordAndRelation(
		t,
		desiredextension.CarrierOpenCodePlugin,
		target.TargetOpenCode,
		spec.scope,
		sourceKind,
		spec.sourceRef,
		"opencode.plugin-carrier",
		"formatter-managed",
		spec.subjectKey,
	)
	return newGenericHostSourceFixture(t, spec, subject, record)
}

func newPiHostRouteFixture(t *testing.T, spec hostSourceRouteFixture) builtFixture {
	t.Helper()
	sourceKind := spec.sourceKind
	if sourceKind == "" {
		sourceKind = desiredextension.SourceKindHostSource
	}
	record, subject := mustCarrierRecordAndRelation(
		t,
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		spec.scope,
		sourceKind,
		spec.sourceRef,
		"pi.package-carrier",
		"tools-managed",
		spec.subjectKey,
	)
	return newGenericHostSourceFixture(t, spec, subject, record)
}

func newAntigravityCLIHostRouteFixture(t *testing.T, spec hostSourceRouteFixture) builtFixture {
	t.Helper()
	sourceKind := spec.sourceKind
	if sourceKind == "" {
		sourceKind = desiredextension.SourceKindHostSource
	}
	record, subject := mustCarrierRecordAndRelation(
		t,
		desiredextension.CarrierAntigravityCLIPlugin,
		target.TargetAntigravityCLI,
		spec.scope,
		sourceKind,
		spec.sourceRef,
		"antigravity-cli.plugin-carrier",
		"guidance-managed",
		spec.subjectKey,
	)
	return newGenericHostSourceFixture(t, spec, subject, record)
}

func newGenericHostSourceFixture(
	t *testing.T,
	spec hostSourceRouteFixture,
	subject realization.DelegatedRelation,
	record lock.LockedSubjectContract,
) builtFixture {
	t.Helper()
	inventorySpec := spec.inventorySpec
	if inventorySpec.Availability == "" {
		inventorySpec = observerelation.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceFresh,
		}
	}
	return builtFixture{
		subject:  subject,
		record:   record,
		lockfile: testLockedFile(t, record),
		action:   genericRelationActionFor(t, record, subject, inventorySpec, hostDelegatedAdmission(t)),
		workDir:  spec.workDir,
	}
}

func genericRelationActionFor(
	t *testing.T,
	record lock.LockedSubjectContract,
	subject realization.DelegatedRelation,
	inventorySpec observerelation.InventorySpec,
	admission reconciliation.RelationRouteAdmissionDecision,
) reconciliation.RelationAction {
	t.Helper()
	inventory, err := observerelation.NewInventory(inventorySpec)
	if err != nil {
		t.Fatalf("NewInventory returned error: %v", err)
	}
	relation := lockedDelegatedRelation(t, record)
	identity := managedCarrierIdentityForRecord(t, record)
	action, err := reconciliation.NewRelationAction(reconciliation.RelationActionInput{
		CarrierIdentity: identity,
		RouteRequest:    relation.RouteRequest(),
		Correlation:     observerelation.Correlate(subject.ExpectedRelation(), inventory),
		RouteAdmission:  admission,
	})
	if err != nil {
		t.Fatalf("relation.Plan returned error: %v", err)
	}
	return action
}
