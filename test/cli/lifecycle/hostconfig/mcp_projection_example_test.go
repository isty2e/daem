package cli_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

const (
	mcpPublicExampleServerID     = "context7"
	mcpPublicExampleSecretCanary = "literal-secret-canary"
)

type mcpPublicExampleCase struct {
	filename         string
	placementID      aggregate.MCPPlacementID
	wantDelegateRuns int
}

var mcpPublicExampleCases = []mcpPublicExampleCase{
	{filename: "antigravity-global-mcp-stdio.toml", placementID: aggregate.MCPPlacementAntigravityGlobal},
	{filename: "claude-global-mcp-stdio.toml", placementID: aggregate.MCPPlacementClaudeGlobal},
	{filename: "claude-project-mcp-stdio.toml", placementID: aggregate.MCPPlacementClaudeProject, wantDelegateRuns: 1},
	{filename: "codex-global-mcp-stdio.toml", placementID: aggregate.MCPPlacementCodexGlobal},
	{filename: "codex-project-mcp-stdio.toml", placementID: aggregate.MCPPlacementCodexProject},
	{filename: "opencode-global-mcp-stdio.toml", placementID: aggregate.MCPPlacementOpenCodeGlobal},
	{filename: "opencode-project-mcp-stdio.toml", placementID: aggregate.MCPPlacementOpenCodeProject},
}

func TestMCPPublicCLIExampleManifestsLockApplyAndReportStatus(t *testing.T) {
	assertMCPPublicExampleInventory(t, mcpPublicExampleCases)

	for _, test := range mcpPublicExampleCases {
		t.Run(test.filename, func(t *testing.T) {
			project := newMCPCLIProject(t)
			home := filepath.Join(project.root, "home")
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("CONTEXT7_API_TOKEN", mcpPublicExampleSecretCanary)

			operations, ok := mcpcodec.ImplementedMCPPlacementOperationsForID(test.placementID)
			if !ok {
				t.Fatalf("implemented MCP placement operations %q not found", test.placementID)
			}
			placement := operations.Placement()
			testkit.WriteFile(t, project.root, "daem.toml", string(readRepoFile(t, "examples", test.filename)))

			assertMCPPublicExampleDryRunLock(t, project, placement)
			runMCPLock(t, project)
			lockedProjection := assertMCPPublicExampleWrittenLock(t, project, placement)

			applyOutput := runMCPPublicExampleApply(t, project, placement, test.wantDelegateRuns)
			assertNoPublicMCPOutputLeaks(t, applyOutput)
			assertMCPPublicExampleHostProjection(t, project.root, home, operations, lockedProjection)

			statusOutput := runMCPPublicExampleStatus(t, project, placement)
			assertNoPublicMCPOutputLeaks(t, statusOutput)
		})
	}
}

func assertMCPPublicExampleDryRunLock(t *testing.T, project mcpCLIProject, placement aggregate.MCPPlacement) {
	t.Helper()
	exitCode, stdout, stderr := runMCPCLI(t, "lock", "--manifest", project.manifestPath, "--dry-run", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("lock dry-run exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	var payload clijson.Lock
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("decode lock dry-run JSON: %v\nstdout=%s", err, stdout)
	}
	if payload.EntryCounts.Subjects != 1 || len(payload.SubjectChanges) != 1 {
		t.Fatalf("lock dry-run counts subjects=%d changes=%#v, want one subject only", payload.EntryCounts.Subjects, payload.SubjectChanges)
	}
	change := payload.SubjectChanges[0]
	if change.Status != "added" || change.After == nil || change.After.Realization == nil {
		t.Fatalf("lock dry-run subject change = %#v, want one added managed aggregate realization", change)
	}
	realization := change.After.Realization
	assertMCPPublicExampleProjectionIdentity(
		t,
		placement,
		change.Subject.Kind,
		change.Subject.Namespace,
		change.Subject.Name,
		realization.Target,
		realization.Scope,
		realization.AggregateRoot,
		realization.ContentPath,
		realization.AdapterContractVersion,
	)
	assertNoPublicMCPOutputLeaks(t, stdout)
}

func assertMCPPublicExampleWrittenLock(
	t *testing.T,
	project mcpCLIProject,
	placement aggregate.MCPPlacement,
) aggregate.ManagedContribution {
	t.Helper()
	locked, err := lockfile.Load(project.lockfilePath)
	if err != nil {
		t.Fatalf("load written lockfile: %v", err)
	}
	if len(locked.Locked.Subjects()) != 1 {
		t.Fatalf("written lock subjects = %#v, want one", locked.Locked.Subjects())
	}
	assertMCPPublicExampleFileExcludesSecret(t, project.lockfilePath)
	contract := locked.Locked.Subjects()[0]
	contribution := testkit.LockedManagedAggregateContribution(t, contract)
	if strings.TrimSpace(contribution.CanonicalContribution()) == "" {
		t.Fatalf("written lock realization = %#v, want nonempty aggregate contribution", contribution)
	}
	subject := contract.SubjectID()
	assertMCPPublicExampleProjectionIdentity(
		t,
		placement,
		string(subject.Kind()),
		subject.Namespace(),
		subject.Key(),
		string(contribution.Target()),
		string(contribution.Scope()),
		contribution.AggregateRoot().String(),
		contribution.ContentPath(),
		string(contribution.CodecContractID()),
	)
	return contribution
}

func assertMCPPublicExampleProjectionIdentity(
	t *testing.T,
	placement aggregate.MCPPlacement,
	kind string,
	namespace string,
	name string,
	selectedTarget string,
	scope string,
	configPath string,
	contentPath string,
	adapterContract string,
) {
	t.Helper()
	wantContentPath, err := placement.ContentPath(mcpPublicExampleServerID)
	if err != nil {
		t.Fatalf("derive content path for %q: %v", placement.ID(), err)
	}
	id, err := entity.New(entity.KindMCPServer, mcpPublicExampleServerID)
	if err != nil {
		t.Fatalf("derive MCP entity: %v", err)
	}
	wantSubject, err := topologymcp.ProjectionSubject(placement.Target(), placement.Scope(), id.Name())
	if err != nil {
		t.Fatalf("derive projection subject for %q: %v", placement.ID(), err)
	}
	if kind != string(topology.SubjectProjection) ||
		namespace != wantSubject.Namespace() ||
		name != mcpPublicExampleServerID ||
		selectedTarget != string(placement.Target()) ||
		scope != string(placement.Scope()) ||
		configPath != placement.ConfigPath().String() ||
		contentPath != string(wantContentPath) ||
		adapterContract != string(placement.CodecContractID()) {
		t.Fatalf("projection identity kind=%q namespace=%q name=%q target=%q scope=%q config=%q content=%q adapter=%q, want placement %#v", kind, namespace, name, selectedTarget, scope, configPath, contentPath, adapterContract, placement)
	}
}

func runMCPPublicExampleApply(
	t *testing.T,
	project mcpCLIProject,
	placement aggregate.MCPPlacement,
	wantDelegateRuns int,
) string {
	t.Helper()
	delegateRuns := 0
	exitCode, stdout, stderr := runMCPCLIWithOptions(t, []string{
		"apply",
		"--manifest", project.manifestPath,
		"--target", string(placement.Target()),
		"--yes",
		"--json",
	}, clipkg.RunOptions{
		ApplyExecuteOptions: applyworkflow.ExecuteOptions{
			DelegateExecutor: delegate.NewExecutor(delegate.Options{
				Runner: func(_ context.Context, _ subprocess.CommandRequest) subprocess.CommandResult {
					delegateRuns++
					if wantDelegateRuns == 0 {
						t.Fatalf("placement %q unexpectedly invoked a delegate", placement.ID())
					}
					return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
				},
			}),
		},
	})
	if exitCode != 0 || stderr != "" {
		t.Fatalf("apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	if delegateRuns != wantDelegateRuns {
		t.Fatalf("delegate runs = %d, want %d for placement %q", delegateRuns, wantDelegateRuns, placement.ID())
	}
	payload := clijson.DecodeApplyResult(t, []byte(stdout))
	if payload.HasErrors {
		t.Fatalf("apply payload errors = %#v", payload.Errors)
	}
	return stdout
}

func assertMCPPublicExampleHostProjection(
	t *testing.T,
	projectRoot string,
	home string,
	operations mcpcodec.MCPPlacementOperations,
	locked aggregate.ManagedContribution,
) {
	t.Helper()
	placement := operations.Placement()
	hostConfigPath := mcpPublicExampleHostConfigPath(t, projectRoot, home, placement)
	content, err := os.ReadFile(hostConfigPath)
	if err != nil {
		t.Fatalf("read host config %q: %v", hostConfigPath, err)
	}
	if strings.Contains(string(content), mcpPublicExampleSecretCanary) {
		t.Fatalf("host config %q contains resolved secret canary", hostConfigPath)
	}
	assertMCPPublicExampleFileExcludesSecret(t, filepath.Join(projectRoot, ".daem", "state.json"))
	extracted, present, err := operations.ExtractCanonicalEntry(content, mcpPublicExampleServerID)
	if err != nil {
		t.Fatalf("extract %q from placement %q: %v", mcpPublicExampleServerID, placement.ID(), err)
	}
	if !present || len(extracted) == 0 {
		t.Fatalf("placement %q host config lacks a nonempty %q canonical entry", placement.ID(), mcpPublicExampleServerID)
	}
	comparison, err := operations.CompareCanonicalEntry(content, mcpPublicExampleServerID, []byte(locked.CanonicalContribution()))
	if err != nil {
		t.Fatalf("compare placement %q host config with lock: %v", placement.ID(), err)
	}
	if !comparison.Present || !comparison.Equivalent || comparison.ContentPath != locked.ContentPath() {
		t.Fatalf("placement %q host comparison = %#v, want present equivalent content path %q", placement.ID(), comparison, locked.ContentPath())
	}
}

func assertMCPPublicExampleFileExcludesSecret(t *testing.T, filename string) {
	t.Helper()
	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("read secret-sensitive file %q: %v", filename, err)
	}
	if strings.Contains(string(content), mcpPublicExampleSecretCanary) {
		t.Fatalf("secret-sensitive file %q contains resolved secret canary", filename)
	}
}

func mcpPublicExampleHostConfigPath(
	t *testing.T,
	projectRoot string,
	home string,
	placement aggregate.MCPPlacement,
) string {
	t.Helper()
	logical := placement.ConfigPath().String()
	switch placement.Scope() {
	case target.ScopeProject:
		if filepath.IsAbs(logical) || strings.HasPrefix(logical, "~") {
			t.Fatalf("project placement %q has non-project config path %q", placement.ID(), logical)
		}
		return mcpPublicExamplePathUnder(t, projectRoot, logical, placement)
	case target.ScopeGlobal:
		if !strings.HasPrefix(logical, "~/") {
			t.Fatalf("global placement %q has non-home config path %q", placement.ID(), logical)
		}
		return mcpPublicExamplePathUnder(t, home, strings.TrimPrefix(logical, "~/"), placement)
	default:
		t.Fatalf("placement %q has unsupported scope %q", placement.ID(), placement.Scope())
		return ""
	}
}

func mcpPublicExamplePathUnder(
	t *testing.T,
	root string,
	relative string,
	placement aggregate.MCPPlacement,
) string {
	t.Helper()
	destination := filepath.Clean(filepath.Join(root, filepath.FromSlash(relative)))
	contained, err := filepath.Rel(root, destination)
	if err != nil {
		t.Fatalf("resolve placement %q config path below %q: %v", placement.ID(), root, err)
	}
	if contained == "." || contained == ".." || strings.HasPrefix(contained, ".."+string(os.PathSeparator)) {
		t.Fatalf("placement %q config path %q escapes or aliases root %q", placement.ID(), relative, root)
	}
	return destination
}

func runMCPPublicExampleStatus(
	t *testing.T,
	project mcpCLIProject,
	placement aggregate.MCPPlacement,
) string {
	t.Helper()
	exitCode, stdout, stderr := runMCPCLI(
		t,
		"status",
		"--manifest", project.manifestPath,
		"--target", string(placement.Target()),
		"--check",
		"--json",
	)
	if exitCode != 0 || stderr != "" {
		t.Fatalf("status exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	payload := clijson.DecodePlan(t, []byte(stdout))
	assertMCPPublicExampleStatusPayload(t, payload, placement)
	return stdout
}

func assertMCPPublicExampleStatusPayload(
	t *testing.T,
	payload clijson.Plan,
	placement aggregate.MCPPlacement,
) {
	t.Helper()
	wantContentPath, err := placement.ContentPath(mcpPublicExampleServerID)
	if err != nil {
		t.Fatalf("derive status content path for %q: %v", placement.ID(), err)
	}
	id, err := entity.New(entity.KindMCPServer, mcpPublicExampleServerID)
	if err != nil {
		t.Fatalf("derive MCP entity: %v", err)
	}
	wantSubject, err := topologymcp.ProjectionSubject(placement.Target(), placement.Scope(), id.Name())
	if err != nil {
		t.Fatalf("derive status projection subject for %q: %v", placement.ID(), err)
	}
	matchingActions := 0
	for _, action := range payload.Actions {
		if action.Subject == nil || action.Projection == nil ||
			action.Subject.Kind != string(topology.SubjectProjection) ||
			action.Subject.Namespace != wantSubject.Namespace() ||
			action.Subject.Name != mcpPublicExampleServerID {
			continue
		}
		matchingActions++
		if action.Kind != "noop" || action.Reason != "already_current" ||
			action.Projection.Target != string(placement.Target()) ||
			action.Projection.Scope != string(placement.Scope()) ||
			action.Projection.ConfigPath != placement.ConfigPath().String() ||
			action.Projection.ContentPath != string(wantContentPath) {
			t.Fatalf("status action = %#v, want already-current placement %q", action, placement.ID())
		}
	}
	if matchingActions != 1 {
		t.Fatalf("status actions = %#v, want one action for placement %q", payload.Actions, placement.ID())
	}
	if len(payload.MCPStatuses) != 1 {
		t.Fatalf("MCP statuses = %#v, want one for placement %q", payload.MCPStatuses, placement.ID())
	}
	status := payload.MCPStatuses[0]
	assertMCPPublicExampleProjectionIdentity(
		t,
		placement,
		status.Subject.Kind,
		status.Subject.Namespace,
		status.Subject.Name,
		status.Target,
		status.Scope,
		status.ConfigPath,
		status.ContentPath,
		status.AdapterContractVersion,
	)
	projectionDimension := "project_projection"
	if placement.Scope() == target.ScopeGlobal {
		projectionDimension = "global_projection"
	}
	assertMCPJSONDimensionInGroup(t, payload, "projection", projectionDimension, "projected", "")
	assertMCPJSONDimensionInGroup(t, payload, "host", "same_scope_ownership", "managed", "")
}

func readRepoFile(t *testing.T, parts ...string) []byte {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(append([]string{testkit.RepositoryRoot(t)}, parts...)...))
	if err != nil {
		t.Fatalf("read repo file %v: %v", parts, err)
	}
	return content
}
