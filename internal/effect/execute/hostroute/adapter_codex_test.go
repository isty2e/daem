package hostroute

import (
	"path/filepath"
	"slices"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
)

func TestBuildCommandBuildsCodexGlobalMarketplaceAttempt(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "project with spaces")
	command := mustBuildCodexCommand(t, codexHostRouteFixture{
		sourceRef:  "documents@openai-primary-runtime",
		subjectKey: "documents@openai-primary-runtime",
		scope:      target.ScopeGlobal,
		workDir:    workDir,
	})

	attempt := command.AttemptRequest()
	if attempt.Command != "codex" {
		t.Fatalf("command = %q, want codex", attempt.Command)
	}
	wantArgs := []string{
		"plugin",
		"add",
		"documents@openai-primary-runtime",
		"--json",
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

	attempt.Args[2] = "mutated"
	if command.AttemptRequest().Args[2] != "documents@openai-primary-runtime" {
		t.Fatal("AttemptRequest did not return a defensive args copy")
	}
}

type codexHostRouteFixture struct {
	sourceRef     string
	subjectKey    string
	scope         target.Scope
	workDir       string
	inventorySpec observerelation.InventorySpec
}

func mustBuildCodexCommand(t *testing.T, spec codexHostRouteFixture) Command {
	t.Helper()
	fixture := newCodexHostRouteFixture(t, spec)
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

func newCodexHostRouteFixture(t *testing.T, spec codexHostRouteFixture) builtFixture {
	t.Helper()
	record, subject := mustCodexPluginFixture(t, subjectSpec{
		sourceKind: desiredextension.SourceKindMarketplace,
		sourceRef:  spec.sourceRef,
		subjectKey: spec.subjectKey,
		scope:      spec.scope,
	})
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

func mustCodexPluginFixture(
	t *testing.T,
	spec subjectSpec,
) (lock.LockedSubjectContract, realization.DelegatedRelation) {
	t.Helper()
	declarationID := spec.declarationID
	if declarationID == "" {
		declarationID = "documents-managed"
	}
	return mustCarrierRecordAndRelation(
		t,
		desiredextension.CarrierCodexPlugin,
		target.TargetCodex,
		spec.scope,
		spec.sourceKind,
		spec.sourceRef,
		"codex.plugin-carrier",
		declarationID,
		spec.subjectKey,
	)
}
