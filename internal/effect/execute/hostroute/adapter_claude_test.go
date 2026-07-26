package hostroute

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/subprocess"

	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestBuildCommandBuildsClaudeMarketplaceAttempts(t *testing.T) {
	tests := []struct {
		name          string
		scope         target.Scope
		marketplace   string
		wantHostScope string
	}{
		{
			name:          "project",
			scope:         target.ScopeProject,
			marketplace:   "context7@market;$(echo-nope)*",
			wantHostScope: "project",
		},
		{
			name:          "global",
			scope:         target.ScopeGlobal,
			marketplace:   "context7@market;$(echo-nope)*",
			wantHostScope: "user",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workDir := filepath.Join(t.TempDir(), "project with spaces")
			fixture := newHostRouteFixture(t, hostRouteFixture{
				sourceKind: desiredextension.SourceKindMarketplace,
				sourceRef:  test.marketplace,
				subjectKey: "context7@market",
				scope:      test.scope,
				workDir:    workDir,
			})
			command, err := BuildCommand(BuildInput{
				Action:   fixture.action,
				Lockfile: fixture.lockfile,
				WorkDir:  fixture.workDir,
			})
			if err != nil {
				t.Fatalf("BuildCommand returned error: %v", err)
			}

			attempt := command.AttemptRequest()
			if attempt.Command != "claude" {
				t.Fatalf("command = %q, want claude", attempt.Command)
			}
			wantArgs := []string{
				"plugin",
				"install",
				test.marketplace,
				"--scope",
				test.wantHostScope,
			}
			if !slices.Equal(attempt.Args, wantArgs) {
				t.Fatalf("args = %#v, want %#v", attempt.Args, wantArgs)
			}
			if attempt.WorkDir != workDir {
				t.Fatalf("workdir = %q, want %q", attempt.WorkDir, workDir)
			}
			if len(attempt.EnvRefs) != 0 {
				t.Fatalf("env refs = %#v, want none", attempt.EnvRefs)
			}
			if fixture.action.Scope() != test.scope {
				t.Fatalf("action scope = %q, want canonical daem scope %q", fixture.action.Scope(), test.scope)
			}
			if command.Subject() != fixture.record.SubjectID() || command.RouteRequest() != fixture.action.RouteRequest() {
				t.Fatalf("command metadata subject=%#v route=%#v, want locked daem route metadata", command.Subject(), command.RouteRequest())
			}

			attempt.Args[2] = "mutated"
			if command.AttemptRequest().Args[2] != test.marketplace {
				t.Fatal("AttemptRequest did not return a defensive args copy")
			}
		})
	}
}

func TestClaudePluginHostScopeArgRejectsUnsupportedHostLikeScopes(t *testing.T) {
	subject, err := topology.NewSubjectID(topology.SubjectHostRelation, "claude-code.plugin-carrier", "context7")
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	for _, scope := range []target.Scope{"user", "local", "managed", "workspace"} {
		t.Run(string(scope), func(t *testing.T) {
			_, err := claudePluginHostScopeArg(scope, subject)
			assertValidationCode(t, err, ReasonUnsupportedScope)
		})
	}
}

func TestBuildCommandOutputUsesGenericExecutorForMissingRunnerClassification(t *testing.T) {
	command := mustBuildCommand(t, hostRouteFixture{
		sourceKind: desiredextension.SourceKindMarketplace,
		sourceRef:  "context7@market",
		subjectKey: "context7@market",
		scope:      target.ScopeProject,
		workDir:    t.TempDir(),
	})

	request := command.AttemptRequest()
	root, err := rootedpath.CaptureRoot(request.WorkDir)
	if err != nil {
		t.Fatalf("CaptureRoot returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Errorf("close captured root: %v", err)
		}
	})
	result := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Clock: func() time.Time { return time.Unix(10, 0).UTC() },
		Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			if request.Command != "claude" {
				t.Fatalf("runner command = %q, want claude", request.Command)
			}
			return subprocess.CommandResult{MissingRunner: true}
		},
	}).ExecuteInWorkingDirectory(context.Background(), request, func() (subprocess.WorkingDirectoryBinding, error) {
		return root.AcquireWorkingDirectory()
	})
	if result.Reason() != subprocess.CommandReasonMissingRunner {
		t.Fatalf("reason = %q, want %q", result.Reason(), subprocess.CommandReasonMissingRunner)
	}
}

type hostRouteFixture struct {
	sourceKind    desiredextension.SourceKind
	sourceRef     string
	subjectKey    string
	scope         target.Scope
	workDir       string
	inventorySpec observeclaudeplugin.InventorySpec
}

type subjectSpec struct {
	sourceKind    desiredextension.SourceKind
	sourceRef     string
	subjectKey    string
	scope         target.Scope
	declarationID string
}

func mustBuildCommand(t *testing.T, spec hostRouteFixture) Command {
	t.Helper()
	fixture := newHostRouteFixture(t, spec)
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

func newHostRouteFixture(t *testing.T, spec hostRouteFixture) builtFixture {
	t.Helper()
	record, subject := mustClaudePluginFixture(t, subjectSpec{
		sourceKind: spec.sourceKind,
		sourceRef:  spec.sourceRef,
		subjectKey: spec.subjectKey,
		scope:      spec.scope,
	})
	inventorySpec := spec.inventorySpec
	if inventorySpec.Availability == "" {
		inventorySpec = observeclaudeplugin.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceFresh,
		}
	}
	return builtFixture{
		subject:  subject,
		record:   record,
		lockfile: testLockedFile(t, record),
		action:   relationActionFor(t, record, subject, inventorySpec, hostDelegatedAdmission(t)),
		workDir:  spec.workDir,
	}
}

func mustClaudePluginFixture(
	t *testing.T,
	spec subjectSpec,
) (lock.LockedSubjectContract, realization.DelegatedRelation) {
	t.Helper()
	declarationID := spec.declarationID
	if declarationID == "" {
		declarationID = "context7"
	}
	return mustCarrierRecordAndRelation(
		t,
		desiredextension.CarrierClaudeCodePlugin,
		target.TargetClaudeCode,
		spec.scope,
		spec.sourceKind,
		spec.sourceRef,
		"claude-code.plugin-carrier",
		declarationID,
		spec.subjectKey,
	)
}

func mustClaudeRow(
	t *testing.T,
	subjectKey string,
	managedKey string,
	hasManagedKey bool,
) observeclaudeplugin.Row {
	t.Helper()
	row, err := observeclaudeplugin.NewRow(observeclaudeplugin.RowSpec{
		SubjectKey:            subjectKey,
		HasManagedInstanceKey: hasManagedKey,
		ManagedInstanceKey:    managedKey,
		Scope:                 observeclaudeplugin.HostScopeProject,
	})
	if err != nil {
		t.Fatalf("NewRow returned error: %v", err)
	}
	return row
}
