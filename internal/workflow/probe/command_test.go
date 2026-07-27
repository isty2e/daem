package probe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/runtimeprobe"
	runtimeprobemcp "github.com/isty2e/daem/internal/assurance/runtimeprobe/mcp"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/subprocess"
	lockworkflow "github.com/isty2e/daem/internal/workflow/lock"
)

func stringSlicesEqual(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func runProbeCommand(
	ctx context.Context,
	input CommandInput,
	executor RuntimeProbeExecutor,
) (CommandResult, error) {
	prepared, err := Prepare(ctx, input)
	if err != nil {
		return CommandResult{}, err
	}
	defer prepared.Close()
	if input.Mode == ModeDryRun {
		return prepared.Disclosure(), nil
	}
	return prepared.Execute(ctx, executor)
}

func TestRunDryRunDisclosesWithoutExecuting(t *testing.T) {
	project := newProbeWorkflowProject(t)
	writeProbeWorkflowManifest(t, project.root, "node", []string{"server.js"}, map[string]string{"API_TOKEN": "HOST_TOKEN"})
	writeProbeWorkflowLock(t, project)
	executor := &fakeRuntimeProbeExecutor{
		facts: observedOKFacts(),
	}

	result, err := runProbeCommand(context.Background(), CommandInput{
		ServerName:   "context7",
		ManifestPath: project.manifestPath,
		LockfilePath: project.lockfilePath,
		TargetValue:  "claude-code",
		ScopeValue:   "project",
		Mode:         ModeDryRun,
		Timeout:      5 * time.Second,
	}, executor)
	if err != nil {
		t.Fatalf("runProbeCommand returned error: %v", err)
	}
	if executor.called {
		t.Fatal("dry-run called probe executor")
	}
	if result.Runtime.Launcher().State() != runtimeprobe.NotProbed ||
		result.Runtime.ProtocolInitialize().State() != runtimeprobe.NotProbed {
		t.Fatalf("runtime = %#v, want not_probed dimensions", result.Runtime)
	}
	if result.ProbeRequest.Command != "node" ||
		!stringSlicesEqual(result.ProbeRequest.Args, []string{"server.js"}) ||
		result.ProbeRequest.Env["API_TOKEN"] != "HOST_TOKEN" ||
		result.WorkingDirectory != project.root {
		t.Fatalf("result = %#v, want locked command, args, env refs, and selected workdir", result)
	}
}

func TestRunExecuteFoldsProbeFacts(t *testing.T) {
	project := newProbeWorkflowProject(t)
	writeProbeWorkflowManifest(t, project.root, "node", []string{"server.js"}, nil)
	writeProbeWorkflowLock(t, project)
	executor := &fakeRuntimeProbeExecutor{
		facts: observedOKFacts(),
	}

	result, err := runProbeCommand(context.Background(), CommandInput{
		ServerName:   "context7",
		ManifestPath: project.manifestPath,
		LockfilePath: project.lockfilePath,
		TargetValue:  "claude-code",
		ScopeValue:   "project",
		Mode:         ModeExecute,
		Timeout:      5 * time.Second,
	}, executor)
	if err != nil {
		t.Fatalf("runProbeCommand returned error: %v", err)
	}
	if !executor.called {
		t.Fatal("execute did not call probe executor")
	}
	if executor.request.Command != "node" ||
		!stringSlicesEqual(executor.request.Args, []string{"server.js"}) ||
		result.WorkingDirectory != project.root {
		t.Fatalf("probe executor request = %#v, want exact locked command vector", executor.request)
	}
	physicalRoot, err := filepath.EvalSymlinks(project.root)
	if err != nil {
		t.Fatalf("resolve selected-root directory: %v", err)
	}
	if executor.workDir != physicalRoot {
		t.Fatalf("authority directory = %q, want physical root %q", executor.workDir, physicalRoot)
	}
	if result.HasRuntimeErrors() {
		t.Fatalf("HasRuntimeErrors = true for observed_ok runtime: %#v", result.Runtime)
	}
	if result.Runtime.Launcher().State() != runtimeprobe.ObservedOK ||
		result.Runtime.ProtocolInitialize().State() != runtimeprobe.ObservedOK {
		t.Fatalf("runtime = %#v, want observed_ok launch and initialize", result.Runtime)
	}
}

func TestPreparedProbeExecutesImmutableDisclosureExactlyOnce(t *testing.T) {
	project := newProbeWorkflowProject(t)
	writeProbeWorkflowManifest(t, project.root, "old-server", []string{"old.js"}, map[string]string{"API_TOKEN": "OLD_TOKEN"})
	writeProbeWorkflowLock(t, project)
	prepared, err := Prepare(context.Background(), CommandInput{
		ServerName:   "context7",
		ManifestPath: project.manifestPath,
		LockfilePath: project.lockfilePath,
		TargetValue:  "claude-code",
		ScopeValue:   "project",
		Mode:         ModeDryRun,
		Timeout:      5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}
	defer prepared.Close()

	disclosure := prepared.Disclosure()
	disclosure.ProbeRequest.Command = "forged"
	disclosure.ProbeRequest.Args[0] = "forged.js"
	disclosure.ProbeRequest.Env["API_TOKEN"] = "FORGED_TOKEN"
	disclosure.SideEffects[0] = "forged"
	writeProbeWorkflowManifest(t, project.root, "new-server", []string{"new.js"}, map[string]string{"API_TOKEN": "NEW_TOKEN"})
	writeProbeWorkflowLock(t, project)

	executor := &fakeRuntimeProbeExecutor{facts: observedOKFacts()}
	result, err := prepared.Execute(context.Background(), executor)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if executor.request.Command != "old-server" ||
		!stringSlicesEqual(executor.request.Args, []string{"old.js"}) ||
		executor.request.Env["API_TOKEN"] != "OLD_TOKEN" {
		t.Fatalf("executed request = %#v, want original disclosed request", executor.request)
	}
	if result.Mode != ModeExecute || result.ProbeRequest.Command != "old-server" {
		t.Fatalf("result = %#v, want executed original disclosure", result)
	}

	copy := *prepared
	second := &fakeRuntimeProbeExecutor{facts: observedOKFacts()}
	if _, err := copy.Execute(context.Background(), second); !errors.Is(err, errPreparedProbeConsumed) {
		t.Fatalf("second Execute error = %v, want consumed authority", err)
	}
	if second.called {
		t.Fatal("second Execute called probe executor")
	}
}

func TestPreparedProbeCopiesCannotExecuteConcurrently(t *testing.T) {
	project := newProbeWorkflowProject(t)
	writeProbeWorkflowManifest(t, project.root, "node", []string{"server.js"}, nil)
	writeProbeWorkflowLock(t, project)
	prepared, err := Prepare(context.Background(), CommandInput{
		ServerName: "context7", ManifestPath: project.manifestPath, LockfilePath: project.lockfilePath,
		TargetValue: "claude-code", ScopeValue: "project", Mode: ModeDryRun,
	})
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}
	defer prepared.Close()
	copy := *prepared
	values := []*PreparedCommand{prepared, &copy}
	executors := []*fakeRuntimeProbeExecutor{
		{facts: observedOKFacts()},
		{facts: observedOKFacts()},
	}
	errorsByIndex := make([]error, len(values))
	start := make(chan struct{})
	var workers sync.WaitGroup
	for index := range values {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			_, errorsByIndex[index] = values[index].Execute(
				context.Background(),
				executors[index],
			)
		}(index)
	}
	close(start)
	workers.Wait()

	executions := 0
	consumed := 0
	for index, executeErr := range errorsByIndex {
		if executeErr == nil && executors[index].called {
			executions++
			continue
		}
		if errors.Is(executeErr, errPreparedProbeConsumed) && !executors[index].called {
			consumed++
			continue
		}
		t.Fatalf("execution[%d] = called %t error %v", index, executors[index].called, executeErr)
	}
	if executions != 1 || consumed != 1 {
		t.Fatalf("executions = %d consumed = %d, want 1/1", executions, consumed)
	}
}

func TestRunOpenCodeDryRunUsesExactProjectionWithoutDelegatePlan(t *testing.T) {
	project := newProbeWorkflowProject(t)
	writeProbeWorkflowManifestForTarget(t, project.root, "opencode", "node", []string{"server.js"}, nil)
	writeProbeWorkflowLock(t, project)
	executor := &fakeRuntimeProbeExecutor{
		facts: observedOKFacts(),
	}

	result, err := runProbeCommand(context.Background(), CommandInput{
		ServerName:   "context7",
		ManifestPath: project.manifestPath,
		LockfilePath: project.lockfilePath,
		TargetValue:  "opencode",
		ScopeValue:   "project",
		Mode:         ModeDryRun,
		Timeout:      5 * time.Second,
	}, executor)
	if err != nil {
		t.Fatalf("runProbeCommand returned error: %v", err)
	}
	if executor.called {
		t.Fatal("OpenCode dry-run called probe executor")
	}
	if result.ProbeRequest.Command != "node" ||
		!stringSlicesEqual(result.ProbeRequest.Args, []string{"server.js"}) ||
		len(result.ProbeRequest.Env) != 0 ||
		result.WorkingDirectory != project.root {
		t.Fatalf("result = %#v, want OpenCode exact projection command argv without env refs in project workdir", result)
	}
	sideEffects := strings.Join(result.SideEffects, "\n")
	if strings.Contains(sideEffects, "environment variables") {
		t.Fatalf("side effects = %q, must not disclose OpenCode env lookup", sideEffects)
	}
	if !strings.Contains(sideEffects, "process environment") {
		t.Fatalf("side effects = %q, want inherited ambient environment disclosure", sideEffects)
	}
	if !strings.Contains(sideEffects, "workdir") {
		t.Fatalf("side effects = %q, want selected project root workdir disclosure", sideEffects)
	}
}

func TestRunOpenCodeExecuteUsesLockedProjectionCommandVector(t *testing.T) {
	project := newProbeWorkflowProject(t)
	writeProbeWorkflowManifestForTarget(t, project.root, "opencode", "node", []string{"server.js", "--stdio"}, nil)
	writeProbeWorkflowLock(t, project)
	executor := &fakeRuntimeProbeExecutor{
		facts: observedOKFacts(),
	}

	result, err := runProbeCommand(context.Background(), CommandInput{
		ServerName:   "context7",
		ManifestPath: project.manifestPath,
		LockfilePath: project.lockfilePath,
		TargetValue:  "opencode",
		ScopeValue:   "project",
		Mode:         ModeExecute,
		Timeout:      5 * time.Second,
	}, executor)
	if err != nil {
		t.Fatalf("runProbeCommand returned error: %v", err)
	}
	if !executor.called {
		t.Fatal("OpenCode execute did not call probe executor")
	}
	if executor.request.Command != "node" ||
		!stringSlicesEqual(executor.request.Args, []string{"server.js", "--stdio"}) ||
		len(executor.request.Env) != 0 ||
		result.WorkingDirectory != project.root {
		t.Fatalf("probe executor request = %#v, want OpenCode locked exact projection command vector", executor.request)
	}
}

func TestRunRejectsStaleLockBeforeExecuting(t *testing.T) {
	project := newProbeWorkflowProject(t)
	writeProbeWorkflowManifest(t, project.root, "node", []string{"server.js"}, nil)
	writeProbeWorkflowLock(t, project)
	writeProbeWorkflowManifest(t, project.root, "node", []string{"server.js", "--changed"}, nil)
	executor := &fakeRuntimeProbeExecutor{
		facts: observedOKFacts(),
	}

	_, err := runProbeCommand(context.Background(), CommandInput{
		ServerName:   "context7",
		ManifestPath: project.manifestPath,
		LockfilePath: project.lockfilePath,
		TargetValue:  "claude-code",
		ScopeValue:   "project",
		Mode:         ModeExecute,
		Timeout:      5 * time.Second,
	}, executor)
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("runProbeCommand error = %v, want stale lock", err)
	}
	if executor.called {
		t.Fatal("stale lock called probe executor")
	}
}

func TestProbeRequestRejectsClaudeProjectionThatDisagreesWithLockedLaunchIdentity(t *testing.T) {
	project := newProbeWorkflowProject(t)
	writeProbeWorkflowManifest(t, project.root, "node", []string{"server.js"}, nil)
	writeProbeWorkflowLock(t, project)
	record := tamperedClaudeLockedProjection(t, project, func(canonical string) string {
		return strings.Replace(canonical, "server.js", "evil.js", 1)
	})

	_, err := probeRequestFromLockedLaunchIdentity(record)
	if err == nil || !strings.Contains(err.Error(), "locked launch identity") {
		t.Fatalf("probeRequestFromLockedLaunchIdentity error = %v, want locked launch identity mismatch", err)
	}
}

func TestProbeRequestRejectsClaudeProjectionWithoutLockedLaunchIdentity(t *testing.T) {
	project := newProbeWorkflowProject(t)
	writeProbeWorkflowManifest(t, project.root, "node", []string{"server.js"}, nil)
	writeProbeWorkflowLock(t, project)
	locked, err := lockfile.Load(project.lockfilePath)
	if err != nil {
		t.Fatalf("Load lockfile: %v", err)
	}
	record := locked.Locked.Subjects()[0]
	spec, ok := record.Realization()
	if !ok {
		t.Fatal("locked subject is missing realization")
	}
	withoutDelegate, err := lock.NewLockedSubjectContract(lock.LockedSubjectContractInput{
		EntityID:           record.EntityID(),
		SubjectID:          record.SubjectID(),
		Realization:        &spec,
		Ownership:          record.Ownership(),
		OnAbsent:           record.OnAbsent(),
		Replay:             record.ReplayCoverage(),
		OperationContracts: probeOperationContractsFromRecord(record),
	})
	if err != nil {
		t.Fatalf("NewLockedSubjectContract: %v", err)
	}

	_, err = probeRequestFromLockedLaunchIdentity(withoutDelegate)
	if err == nil || !strings.Contains(err.Error(), "missing locked launch identity") {
		t.Fatalf("probeRequestFromLockedLaunchIdentity error = %v, want missing launch identity", err)
	}
}

func TestRuntimeProbeAdmissionIsDerivedFromProfileCapabilities(t *testing.T) {
	if got := runtimeProbeTargetValues(); !stringSlicesEqual(got, []string{"claude-code", "opencode"}) {
		t.Fatalf("runtime-probe targets = %#v", got)
	}
	if got := runtimeProbeScopeValues(); !stringSlicesEqual(got, []string{"project"}) {
		t.Fatalf("runtime-probe scopes = %#v", got)
	}
}

func TestRunRejectsMissingLockBeforeExecuting(t *testing.T) {
	project := newProbeWorkflowProject(t)
	writeProbeWorkflowManifest(t, project.root, "node", []string{"server.js"}, nil)
	executor := &fakeRuntimeProbeExecutor{
		facts: observedOKFacts(),
	}

	_, err := runProbeCommand(context.Background(), CommandInput{
		ServerName:   "context7",
		ManifestPath: project.manifestPath,
		LockfilePath: project.lockfilePath,
		TargetValue:  "claude-code",
		ScopeValue:   "project",
		Mode:         ModeExecute,
		Timeout:      5 * time.Second,
	}, executor)
	if err == nil || !strings.Contains(err.Error(), "run lock") {
		t.Fatalf("runProbeCommand error = %v, want run lock guidance", err)
	}
	if executor.called {
		t.Fatal("missing lock called probe executor")
	}
}

func TestRunRejectsUnsupportedTargetAndScopeBeforeExecuting(t *testing.T) {
	project := newProbeWorkflowProject(t)
	writeProbeWorkflowManifest(t, project.root, "node", []string{"server.js"}, nil)
	writeProbeWorkflowLock(t, project)

	tests := []struct {
		name        string
		targetValue string
		scopeValue  string
		want        string
	}{
		{name: "codex target", targetValue: "codex", scopeValue: "project", want: "only --target claude-code or opencode"},
		{name: "pi target", targetValue: "pi", scopeValue: "project", want: "only --target claude-code or opencode"},
		{name: "antigravity target", targetValue: "antigravity-cli", scopeValue: "project", want: "only --target claude-code or opencode"},
		{name: "scope", targetValue: "claude-code", scopeValue: "global", want: "only --scope project"},
		{name: "opencode scope", targetValue: "opencode", scopeValue: "global", want: "only --scope project"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &fakeRuntimeProbeExecutor{facts: observedOKFacts()}
			_, err := runProbeCommand(context.Background(), CommandInput{
				ServerName:   "context7",
				ManifestPath: project.manifestPath,
				LockfilePath: project.lockfilePath,
				TargetValue:  test.targetValue,
				ScopeValue:   test.scopeValue,
				Mode:         ModeExecute,
				Timeout:      5 * time.Second,
			}, executor)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("runProbeCommand error = %v, want %q", err, test.want)
			}
			if executor.called {
				t.Fatal("unsupported target/scope called probe executor")
			}
		})
	}
}

func TestRunRequiresExplicitProjectManifestForProjectProbe(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Chdir(t.TempDir())
	paths, err := daempaths.Resolve("")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if err := os.MkdirAll(paths.ManifestRoot, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	project := probeWorkflowProject{root: paths.ManifestRoot, manifestPath: paths.ManifestPath, lockfilePath: paths.LockfilePath}
	writeProbeWorkflowManifest(t, project.root, "node", []string{"server.js"}, nil)
	writeProbeWorkflowLock(t, project)

	_, err = runProbeCommand(context.Background(), CommandInput{
		ServerName:  "context7",
		TargetValue: "claude-code",
		ScopeValue:  "project",
		Mode:        ModeDryRun,
		Timeout:     5 * time.Second,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "requires a project manifest") {
		t.Fatalf("runProbeCommand error = %v, want default-manifest project probe rejection", err)
	}
}

type probeWorkflowProject struct {
	root         string
	manifestPath string
	lockfilePath string
}

type fakeRuntimeProbeExecutor struct {
	called  bool
	request runtimeprobemcp.ProbeRequest
	workDir string
	facts   []runtimeprobe.Fact
	err     error
}

func (executor *fakeRuntimeProbeExecutor) Probe(
	ctx context.Context,
	request runtimeprobemcp.ProbeRequest,
	bind subprocess.WorkingDirectoryBinder,
) ([]runtimeprobe.Fact, error) {
	executor.called = true
	executor.request = request
	if bind == nil {
		return nil, errors.New("working-directory binder is missing")
	}
	binding, err := bind()
	if err != nil {
		return nil, err
	}
	defer binding.Close()
	if err := binding.Validate(); err != nil {
		return nil, err
	}
	directory, err := binding.OpenDirectory()
	if err != nil {
		return nil, err
	}
	executor.workDir = directory.Name()
	if err := directory.Close(); err != nil {
		return nil, err
	}
	return executor.facts, executor.err
}

func newProbeWorkflowProject(t *testing.T) probeWorkflowProject {
	t.Helper()
	root := t.TempDir()
	return probeWorkflowProject{
		root:         root,
		manifestPath: filepath.Join(root, "daem.toml"),
		lockfilePath: filepath.Join(root, "daem.lock.toml"),
	}
}

func writeProbeWorkflowManifest(t *testing.T, root string, command string, args []string, env map[string]string) {
	t.Helper()
	writeProbeWorkflowManifestForTarget(t, root, "claude-code", command, args, env)
}

func writeProbeWorkflowManifestForTarget(t *testing.T, root string, selectedTarget string, command string, args []string, env map[string]string) {
	t.Helper()
	envText := ""
	if len(env) != 0 {
		parts := make([]string, 0, len(env))
		for serverEnv, hostEnv := range env {
			parts = append(parts, serverEnv+` = { from_env = "`+hostEnv+`" }`)
		}
		envText = "\nenv = { " + strings.Join(parts, ", ") + " }"
	}
	if err := os.WriteFile(filepath.Join(root, "daem.toml"), []byte(`version = 1
targets = ["`+selectedTarget+`"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "`+command+`"
args = [`+quoteArgs(args)+`]`+envText+`
`), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func writeProbeWorkflowLock(t *testing.T, project probeWorkflowProject) {
	t.Helper()
	_, err := lockworkflow.RunLock(context.Background(), lockworkflow.LockInput{
		ManifestPath: project.manifestPath,
		LockfilePath: project.lockfilePath,
	})
	if err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
}

func tamperedClaudeLockedProjection(
	t *testing.T,
	project probeWorkflowProject,
	tamper func(string) string,
) lock.LockedSubjectContract {
	t.Helper()
	locked, err := lockfile.Load(project.lockfilePath)
	if err != nil {
		t.Fatalf("Load lockfile: %v", err)
	}
	if len(locked.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %d, want 1", len(locked.Locked.Subjects()))
	}
	record := locked.Locked.Subjects()[0]
	spec, ok := record.Realization()
	if !ok {
		t.Fatal("locked subject is missing realization")
	}
	contribution, ok := spec.ManagedAggregateContribution()
	if !ok {
		t.Fatal("locked subject is missing managed aggregate contribution")
	}
	tamperedRealization, err := realization.NewManagedAggregateContribution(aggregate.ManagedContributionInput{
		PlacementID:           contribution.PlacementID(),
		Target:                contribution.Target(),
		Scope:                 contribution.Scope(),
		AggregateRoot:         contribution.AggregateRoot(),
		ContentPath:           contribution.ContentPath(),
		MergeUnit:             contribution.MergeUnit(),
		Cardinality:           contribution.Cardinality(),
		SiblingRetention:      contribution.SiblingRetention(),
		SiblingPreservation:   contribution.SiblingPreservation(),
		Equivalence:           contribution.Equivalence(),
		CanonicalContribution: tamper(contribution.CanonicalContribution()),
		CodecContractID:       contribution.CodecContractID(),
		ComparedFields:        contribution.ComparedFields(),
	})
	if err != nil {
		t.Fatalf("NewManagedAggregateContribution: %v", err)
	}
	delegatePlan, ok := record.DelegatePlan()
	if !ok {
		t.Fatal("locked subject is missing delegate plan")
	}
	tampered, err := lock.NewLockedSubjectContract(lock.LockedSubjectContractInput{
		EntityID:           record.EntityID(),
		SubjectID:          record.SubjectID(),
		Realization:        &tamperedRealization,
		DelegatePlan:       &delegatePlan,
		Ownership:          record.Ownership(),
		OnAbsent:           record.OnAbsent(),
		Replay:             record.ReplayCoverage(),
		OperationContracts: probeOperationContractsFromRecord(record),
	})
	if err != nil {
		t.Fatalf("NewLockedSubjectContract: %v", err)
	}
	return tampered
}

func probeOperationContractsFromRecord(record lock.LockedSubjectContract) []lock.OperationContract {
	kinds := record.OperationKinds()
	contracts := make([]lock.OperationContract, 0, len(kinds))
	for _, kind := range kinds {
		contract, ok := record.OperationContract(kind)
		if ok {
			contracts = append(contracts, contract)
		}
	}
	return contracts
}

func quoteArgs(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, `"`+arg+`"`)
	}
	return strings.Join(quoted, ", ")
}

func observedOKFacts() []runtimeprobe.Fact {
	return []runtimeprobe.Fact{
		{
			Dimension: runtimeprobe.DimensionLauncher,
			State:     runtimeprobe.ObservedOK,
			Source:    runtimeprobe.SourceExplicit,
			Freshness: runtimeprobe.FreshnessCurrent,
		},
		{
			Dimension: runtimeprobe.DimensionProtocolInitialize,
			State:     runtimeprobe.ObservedOK,
			Source:    runtimeprobe.SourceExplicit,
			Freshness: runtimeprobe.FreshnessCurrent,
		},
		{
			Dimension: runtimeprobe.DimensionEndpointHealth,
			State:     runtimeprobe.NotApplicable,
			Reason:    runtimeprobe.ReasonNotApplicable,
			Source:    runtimeprobe.SourceExplicit,
			Freshness: runtimeprobe.FreshnessCurrent,
		},
		{
			Dimension: runtimeprobe.DimensionAuthentication,
			State:     runtimeprobe.Unsupported,
			Reason:    runtimeprobe.ReasonUnsupported,
			Source:    runtimeprobe.SourceExplicit,
			Freshness: runtimeprobe.FreshnessCurrent,
		},
		{
			Dimension: runtimeprobe.DimensionToolInventory,
			State:     runtimeprobe.Unsupported,
			Reason:    runtimeprobe.ReasonUnsupported,
			Source:    runtimeprobe.SourceExplicit,
			Freshness: runtimeprobe.FreshnessCurrent,
		},
	}
}
