package hostroute

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
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
		source   string
		wantArgs []string
	}{
		{
			name:     "project",
			scope:    target.ScopeProject,
			source:   "github:acme/pi-tools",
			wantArgs: []string{"install", "github:acme/pi-tools", "-l"},
		},
		{
			name:     "global",
			scope:    target.ScopeGlobal,
			source:   "github:acme/pi-tools",
			wantArgs: []string{"install", "github:acme/pi-tools"},
		},
		{
			name:     "npm alias",
			scope:    target.ScopeGlobal,
			source:   "npm:pi-tools@npm:@acme/pi-tools@1.2.3",
			wantArgs: []string{"install", "npm:pi-tools@npm:@acme/pi-tools@1.2.3"},
		},
		{
			name:     "git plus transport",
			scope:    target.ScopeGlobal,
			source:   "git+https://github.com/acme/pi-tools.git#v1",
			wantArgs: []string{"install", "git+https://github.com/acme/pi-tools.git#v1"},
		},
		{
			name:     "single segment git repository",
			scope:    target.ScopeGlobal,
			source:   "git+https://git.example/repo.git#v1",
			wantArgs: []string{"install", "git+https://git.example/repo.git#v1"},
		},
		{
			name:     "encoded at sign in git path",
			scope:    target.ScopeGlobal,
			source:   "git+https://github.com/acme/tools%40scope.git",
			wantArgs: []string{"install", "git+https://github.com/acme/tools%40scope.git"},
		},
		{
			name:     "literal percent in git path",
			scope:    target.ScopeGlobal,
			source:   "git+https://example.com/acme/100%25-tool.git#v1",
			wantArgs: []string{"install", "git+https://example.com/acme/100%25-tool.git#v1"},
		},
		{
			name:     "plus-host scp",
			scope:    target.ScopeGlobal,
			source:   "git:git@short+host:acme/tools.git@v1",
			wantArgs: []string{"install", "git:git@short+host:acme/tools.git@v1"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workDir := filepath.Join(t.TempDir(), "project with spaces")
			command := mustBuildPiCommand(t, hostSourceRouteFixture{
				sourceRef:  test.source,
				subjectKey: test.source,
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

func TestBuildCommandRejectsUnauthorizableDurableHostSource(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
	}{
		{
			name:   "credential bearing",
			source: "git:user:actual-secret@github.com/acme/tool#https://example.test/ref",
		},
		{
			name:   "terminal credential bearing",
			source: "git:user:actual-secret@github.com",
		},
		{
			name:   "credential bearing with port",
			source: "user:actual-secret@short-host:443/acme/tool",
		},
		{
			name:   "credential bearing with IPv6 port",
			source: "user:actual-secret@[2001:db8::1]:443/acme/tool",
		},
		{
			name:   "credential bearing in URL fragment",
			source: "https://example.test/#git:user:actual-secret@github.com/acme/tool",
		},
		{
			name:   "unprefixed terminal credential bearing",
			source: "user:actual-secret@short-host",
		},
		{
			name:   "unprefixed credential bearing in URL fragment",
			source: "https://example.test/#user:actual-secret@short-host",
		},
		{
			name:   "wrapped credential bearing",
			source: "[git:user:actual-secret@short-host]",
		},
		{
			name:   "prefixed scp credential bearing",
			source: "git:user:actual-secret@short-host:repo/path",
		},
		{
			name:   "noncanonical host credential bearing",
			source: "user:actual-secret@short+host",
		},
		{
			name:   "punctuated wrapped credential bearing",
			source: "[git:user:actual-secret@short-host]:",
		},
		{
			name:   "uninspectable bracket authority",
			source: "git:user:actual-secret@[2001:db8::1",
		},
		{
			name:   "uninspectable host suffix",
			source: "user:actual-secret@short-host:not-a-port/repo",
		},
		{
			name:   "uninspectable",
			source: "github:acme/tool#release%252525252525value",
		},
		{
			name:   "persisted npm query",
			source: "npm:tool@https://example.com/archive.tgz?download=1",
		},
		{
			name:   "persisted fragment assignment",
			source: "github:acme/tool#download=1",
		},
		{
			name:   "encoded LF git path",
			source: "git+https://example.com/acme/tool%0Aforged.git#v1",
		},
		{
			name:   "encoded Bidi git path",
			source: "git+https://example.com/acme/tool%E2%80%AEforged.git#v1",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPiHostRouteFixture(t, hostSourceRouteFixture{
				sourceRef:  test.source,
				subjectKey: "legacy-source",
				scope:      target.ScopeGlobal,
				workDir:    t.TempDir(),
			})
			_, err := BuildCommand(BuildInput{
				Action:   fixture.action,
				Lockfile: fixture.lockfile,
				WorkDir:  fixture.workDir,
			})
			if err == nil || !strings.Contains(err.Error(), "inspectable") {
				t.Fatalf("BuildCommand error = %v, want source authorization rejection", err)
			}
		})
	}
}

func TestBuildPiCommandRejectsPathUnsafeGitHost(t *testing.T) {
	fixture := newPiHostRouteFixture(t, hostSourceRouteFixture{
		sourceRef:  "git+https://[2001:db8::1]/acme/tool.git#v1",
		subjectKey: "ipv6-source",
		scope:      target.ScopeGlobal,
		workDir:    t.TempDir(),
	})
	_, err := BuildCommand(BuildInput{
		Action:   fixture.action,
		Lockfile: fixture.lockfile,
		WorkDir:  fixture.workDir,
	})
	if err == nil || !strings.Contains(err.Error(), "path-safe") {
		t.Fatalf("BuildCommand error = %v, want Pi git host path-safety rejection", err)
	}
}

func TestDirectHostSourceArgAdaptersRejectCredentialBearingDurableSource(t *testing.T) {
	subject := mustHostRelationSubjectID(t, "test.plugin-carrier", "credential-gate")
	adapters := []struct {
		name    string
		scope   target.Scope
		adapter commandAdapter
	}{
		{name: "OpenCode install", scope: target.ScopeGlobal, adapter: openCodePluginCarrierCommandAdapter},
		{name: "OpenCode refresh", scope: target.ScopeGlobal, adapter: openCodePluginCarrierRefreshCommandAdapter},
		{name: "Pi install", scope: target.ScopeGlobal, adapter: piPackageCarrierCommandAdapter},
		{name: "Pi refresh", scope: target.ScopeGlobal, adapter: piPackageCarrierRefreshCommandAdapter},
		{name: "Pi remove", scope: target.ScopeGlobal, adapter: piPackageCarrierRemoveCommandAdapter},
		{name: "Antigravity install", scope: target.ScopeGlobal, adapter: antigravityCLIPluginCarrierCommandAdapter},
		{name: "Antigravity refresh", scope: target.ScopeGlobal, adapter: antigravityCLIPluginCarrierRefreshCommandAdapter},
	}
	for _, ref := range []string{
		"git:user:actual-secret@github.com",
		"npm:tool@token = actual-secret",
		"npm:alias@npm:tool@token = actual-secret",
	} {
		source, err := desiredextension.NewSourceRef(
			desiredextension.SourceKindHostSource,
			ref,
		)
		if err != nil {
			t.Fatalf("NewSourceRef durable host source %q: %v", ref, err)
		}
		for _, test := range adapters {
			t.Run(test.name+"/"+ref, func(t *testing.T) {
				_, err := test.adapter.build(commandAdapterInput{
					subject: subject,
					scope:   test.scope,
					source:  source,
					workDir: t.TempDir(),
				})
				if err == nil || !strings.Contains(err.Error(), "inspectable") {
					t.Fatalf("adapter error = %v, want source authorization rejection", err)
				}
			})
		}
	}
}

func TestBuildClaudeCommandRejectsUninspectableDurableMarketplaceSource(t *testing.T) {
	for _, ref := range []string{
		"plugin@market%zz",
		"plugin@https://user:actual-secret%40example.com",
	} {
		t.Run(ref, func(t *testing.T) {
			source, err := desiredextension.NewSourceRef(
				desiredextension.SourceKindMarketplace,
				ref,
			)
			if err != nil {
				t.Fatalf("NewSourceRef legacy marketplace source: %v", err)
			}
			subject, err := topology.NewSubjectID(
				topology.SubjectHostRelation,
				"claude-code.plugin-carrier",
				"legacy-source",
			)
			if err != nil {
				t.Fatal(err)
			}
			_, err = buildClaudePluginCarrierCommand(commandAdapterInput{
				subject: subject,
				scope:   target.ScopeGlobal,
				source:  source,
				workDir: t.TempDir(),
			})
			if err == nil {
				t.Fatal("buildClaudePluginCarrierCommand admitted an unauthorizable source")
			}
		})
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
