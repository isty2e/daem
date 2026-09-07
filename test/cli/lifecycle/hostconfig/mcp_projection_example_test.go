package cli_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	mcptest "github.com/isty2e/daem/test/testkit/mcp"
)

const (
	mcpPublicExampleServerID     = "context7"
	mcpPublicExampleSecretCanary = "literal-secret-canary"
)

type mcpPublicExampleCase struct {
	filename         string
	placementID      aggregate.MCPPlacementID
	wantDelegateRuns int
	providerSource   string
	providerVersion  string
}

var mcpPublicExampleCases = []mcpPublicExampleCase{
	{filename: "antigravity-global-mcp-stdio.toml", placementID: aggregate.MCPPlacementAntigravityGlobal},
	{filename: "claude-global-mcp-stdio.toml", placementID: aggregate.MCPPlacementClaudeGlobal},
	{filename: "claude-project-mcp-stdio.toml", placementID: aggregate.MCPPlacementClaudeProject, wantDelegateRuns: 1},
	{filename: "codex-global-mcp-stdio.toml", placementID: aggregate.MCPPlacementCodexGlobal},
	{filename: "codex-project-mcp-stdio.toml", placementID: aggregate.MCPPlacementCodexProject},
	{filename: "opencode-global-mcp-stdio.toml", placementID: aggregate.MCPPlacementOpenCodeGlobal},
	{filename: "opencode-project-mcp-stdio.toml", placementID: aggregate.MCPPlacementOpenCodeProject},
	{
		filename:        "pi-global-mcp-stdio.toml",
		placementID:     aggregate.MCPPlacementPiGlobal,
		providerSource:  "npm:pi-mcp-adapter@^2.13.0",
		providerVersion: "2.15.0",
	},
	{
		filename:        "pi-project-mcp-stdio.toml",
		placementID:     aggregate.MCPPlacementPiProject,
		providerSource:  "npm:pi-mcp-adapter@^2.13.0",
		providerVersion: "2.15.0",
	},
}

func TestMCPPublicCLIExampleManifestsLockApplyAndReportStatus(t *testing.T) {
	for _, test := range mcpPublicExampleCases {
		t.Run(test.filename, func(t *testing.T) {
			project := newMCPCLIProject(t)
			home := filepath.Join(project.root, "home")
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("CONTEXT7_API_TOKEN", mcpPublicExampleSecretCanary)
			t.Setenv("CODEX_TOKEN", mcpPublicExampleSecretCanary)

			operations, ok := mcptest.OperationsForPlacementID(test.placementID)
			if !ok {
				t.Fatalf("implemented MCP placement operations %q not found", test.placementID)
			}
			placement := operations.Placement()
			testkit.WriteFile(t, project.root, "daem.toml", string(readRepoFile(t, "examples", test.filename)))

			assertMCPPublicExampleDryRunLock(t, project, placement, test)
			runMCPLock(t, project)
			lockedProjection := assertMCPPublicExampleWrittenLock(t, project, placement, test)

			applyOutput := runMCPPublicExampleApply(t, project, home, placement, test)
			assertNoPublicMCPOutputLeaks(t, applyOutput)
			assertMCPPublicExampleHostProjection(t, project.root, home, operations, lockedProjection)

			statusOutput := runMCPPublicExampleStatus(t, project, placement, test)
			assertNoPublicMCPOutputLeaks(t, statusOutput)
		})
	}
}

func assertMCPPublicExampleDryRunLock(
	t *testing.T,
	project mcpCLIProject,
	placement aggregate.MCPPlacement,
	test mcpPublicExampleCase,
) {
	t.Helper()
	exitCode, stdout, stderr := runMCPCLI(t, "lock", "--manifest", project.manifestPath, "--dry-run", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("lock dry-run exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	var payload clijson.Lock
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("decode lock dry-run JSON: %v\nstdout=%s", err, stdout)
	}
	wantSubjects := 1
	if test.providerSource != "" {
		wantSubjects = 2
	}
	if payload.EntryCounts.Subjects != wantSubjects || len(payload.SubjectChanges) != wantSubjects {
		t.Fatalf("lock dry-run counts subjects=%d changes=%#v, want %d subjects", payload.EntryCounts.Subjects, payload.SubjectChanges, wantSubjects)
	}
	change, found := mcpPublicExampleProjectionChange(payload.SubjectChanges)
	if !found {
		t.Fatalf("lock dry-run changes = %#v, want MCP projection subject", payload.SubjectChanges)
	}
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
	assertMCPPublicExampleDryRunProvider(t, payload.SubjectChanges, placement, test)
	assertNoPublicMCPOutputLeaks(t, stdout)
}

func assertMCPPublicExampleWrittenLock(
	t *testing.T,
	project mcpCLIProject,
	placement aggregate.MCPPlacement,
	test mcpPublicExampleCase,
) aggregate.ManagedContribution {
	t.Helper()
	locked, err := lockfile.Load(t.Context(), project.lockfilePath)
	if err != nil {
		t.Fatalf("load written lockfile: %v", err)
	}
	wantSubjects := 1
	if test.providerSource != "" {
		wantSubjects = 2
	}
	if len(locked.Locked.Subjects()) != wantSubjects {
		t.Fatalf("written lock subjects = %#v, want %d", locked.Locked.Subjects(), wantSubjects)
	}
	assertMCPPublicExampleFileExcludesSecret(t, project.lockfilePath)
	var contract lock.LockedSubjectContract
	found := false
	for _, candidate := range locked.Locked.Subjects() {
		if candidate.SubjectID().Kind() == topology.SubjectProjection &&
			candidate.EntityID().Name() == mcpPublicExampleServerID {
			contract = candidate
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("written lock subjects = %#v, want MCP projection subject", locked.Locked.Subjects())
	}
	if test.providerSource != "" &&
		!lockedEntitySubjectPresent(locked.Locked.Subjects(), mcpPublicExampleProviderID(placement), topology.SubjectHostRelation) {
		t.Fatalf("written lock subjects = %#v, want explicit Pi provider relation", locked.Locked.Subjects())
	}
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
	home string,
	placement aggregate.MCPPlacement,
	test mcpPublicExampleCase,
) string {
	t.Helper()
	delegateRuns := 0
	var hostRouteRequests []subprocess.CommandRequest
	hostRouteExecutor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			hostRouteRequests = append(hostRouteRequests, request)
			if test.providerSource == "" {
				t.Fatalf("placement %q unexpectedly invoked a host route", placement.ID())
			}
			assertMCPPublicExamplePiInstallRequest(t, request, project.root, placement, test.providerSource)
			writeMCPPublicExamplePiProviderState(
				t,
				project.root,
				home,
				placement,
				test.providerSource,
				test.providerVersion,
			)
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
		},
	})
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
					if test.wantDelegateRuns == 0 {
						t.Fatalf("placement %q unexpectedly invoked a delegate", placement.ID())
					}
					return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
				},
			}),
			HostRouteExecutor: hostRouteExecutor,
		},
	})
	if exitCode != 0 || stderr != "" {
		t.Fatalf("apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	if delegateRuns != test.wantDelegateRuns {
		t.Fatalf("delegate runs = %d, want %d for placement %q", delegateRuns, test.wantDelegateRuns, placement.ID())
	}
	wantHostRouteRuns := 0
	if test.providerSource != "" {
		wantHostRouteRuns = 1
	}
	if len(hostRouteRequests) != wantHostRouteRuns {
		t.Fatalf("host route requests = %#v, want %d for placement %q", hostRouteRequests, wantHostRouteRuns, placement.ID())
	}
	payload := clijson.DecodeApplyResult(t, []byte(stdout))
	if payload.HasErrors {
		t.Fatalf("apply payload errors = %#v", payload.Errors)
	}
	if len(payload.HostRouteAttempts) != wantHostRouteRuns {
		t.Fatalf("host route attempts = %#v, want %d for placement %q", payload.HostRouteAttempts, wantHostRouteRuns, placement.ID())
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
	extracted, present, err := mcptest.ExtractCanonicalEntry(operations, content, mcpPublicExampleServerID)
	if err != nil {
		t.Fatalf("extract %q from placement %q: %v", mcpPublicExampleServerID, placement.ID(), err)
	}
	if !present || len(extracted) == 0 {
		t.Fatalf("placement %q host config lacks a nonempty %q canonical entry", placement.ID(), mcpPublicExampleServerID)
	}
	comparison, err := mcptest.CompareCanonicalEntry(operations, content, mcpPublicExampleServerID, []byte(locked.CanonicalContribution()))
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
	test mcpPublicExampleCase,
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
	assertMCPPublicExampleStatusPayload(t, payload, placement, test)
	return stdout
}

func assertMCPPublicExampleStatusPayload(
	t *testing.T,
	payload clijson.Plan,
	placement aggregate.MCPPlacement,
	test mcpPublicExampleCase,
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
	if test.providerVersion != "" {
		if status.CurrentProviderVersion != test.providerVersion {
			t.Fatalf("provider version = %q, want %q", status.CurrentProviderVersion, test.providerVersion)
		}
		assertMCPJSONDimensionInGroup(t, payload, "host", "provider_prerequisite", "current", "")
	}
}

func mcpPublicExampleProjectionChange(
	changes []clijson.LockSubjectChange,
) (clijson.LockSubjectChange, bool) {
	for _, change := range changes {
		if change.Subject.Kind == string(topology.SubjectProjection) &&
			change.Subject.Name == mcpPublicExampleServerID {
			return change, true
		}
	}
	return clijson.LockSubjectChange{}, false
}

func assertMCPPublicExampleDryRunProvider(
	t *testing.T,
	changes []clijson.LockSubjectChange,
	placement aggregate.MCPPlacement,
	test mcpPublicExampleCase,
) {
	t.Helper()
	if test.providerSource == "" {
		return
	}
	providerID := mcpPublicExampleProviderID(placement)
	for _, change := range changes {
		if change.Subject.Kind == string(topology.SubjectHostRelation) &&
			change.Subject.Name == providerID &&
			change.Status == "added" {
			return
		}
	}
	t.Fatalf("lock dry-run changes = %#v, want explicit provider relation %q", changes, providerID)
}

func mcpPublicExampleProviderID(placement aggregate.MCPPlacement) string {
	return "pi-mcp-adapter-" + string(placement.Scope())
}

func assertMCPPublicExamplePiInstallRequest(
	t *testing.T,
	request subprocess.CommandRequest,
	projectRoot string,
	placement aggregate.MCPPlacement,
	source string,
) {
	t.Helper()
	wantArgs := []string{"install", source}
	if placement.Scope() == target.ScopeProject {
		wantArgs = append(wantArgs, "-l")
	}
	if request.Command != "pi" || !slices.Equal(request.Args, wantArgs) ||
		request.WorkDir != projectRoot {
		t.Fatalf("Pi provider request = %#v, want command=pi args=%v workdir=%q", request, wantArgs, projectRoot)
	}
}

func writeMCPPublicExamplePiProviderState(
	t *testing.T,
	projectRoot string,
	home string,
	placement aggregate.MCPPlacement,
	source string,
	version string,
) {
	t.Helper()
	base := filepath.Join(projectRoot, ".pi")
	if placement.Scope() == target.ScopeGlobal {
		base = filepath.Join(home, ".pi", "agent")
	}
	settings, err := json.Marshal(map[string][]string{"packages": {source}})
	if err != nil {
		t.Fatal(err)
	}
	testkit.WriteFile(t, base, "settings.json", string(settings)+"\n")
	testkit.WriteFile(
		t,
		base,
		filepath.Join("npm", "node_modules", "pi-mcp-adapter", "package.json"),
		`{"name":"pi-mcp-adapter","version":"`+version+`"}`+"\n",
	)
}

func readRepoFile(t *testing.T, parts ...string) []byte {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(append([]string{testkit.RepositoryRoot(t)}, parts...)...))
	if err != nil {
		t.Fatalf("read repo file %v: %v", parts, err)
	}
	return content
}
