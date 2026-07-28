package build

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestBuildLocksMCPServersAsExactProjectionSubjectsWithoutSourceTasks(t *testing.T) {
	lockEvents := make([]Event, 0)
	sourceEvents := make([]acquisition.Event, 0)
	file, err := buildWithTestOptions(
		context.Background(),
		lockEnvironment(t, desired.Spec{
			MCPServers: []desiredmcp.Server{
				testMCPServer(t, "zeta", "node", []string{"server.js"}, nil),
				testMCPServer(t, "context7", "npx", []string{"-y", "@upstash/context7-mcp"}, map[string]string{
					"API_TOKEN": "CONTEXT7_API_TOKEN",
				}),
			},
		}),
		nil,
		Options{
			Events: func(event Event) {
				lockEvents = append(lockEvents, event)
			},
			SourceEvents: func(event acquisition.Event) {
				sourceEvents = append(sourceEvents, event)
			},
		},
	)
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	if len(lockedSubjectsOfKind(file, entity.KindSkill)) != 0 ||
		len(lockedSubjectsOfKind(file, entity.KindInstructions)) != 0 {
		t.Fatalf("locked subjects = %#v, want MCP subjects only", file.Locked.Subjects())
	}
	if len(file.Locked.Subjects()) != 2 {
		t.Fatalf("locked subjects = %#v, want two MCP subjects", file.Locked.Subjects())
	}
	if file.Locked.Subjects()[0].SubjectID().Key() != "context7" || file.Locked.Subjects()[1].SubjectID().Key() != "zeta" {
		t.Fatalf("subjects are not sorted by identity: %#v", file.Locked.Subjects())
	}

	record := file.Locked.Subjects()[0]
	assertMCPSubjectRecord(t, record, "context7")
	delegatePlan, ok := record.DelegatePlan()
	if !ok {
		t.Fatal("MCP subject is missing delegate plan")
	}
	delegatePackage, hasDelegatePackage := delegatePlan.PackageRef()
	if delegatePlan.Runner().Kind() != delegate.RunnerNPX ||
		delegatePlan.Command().Executable() != "npx" ||
		delegatePlan.PinPolicy() != delegate.PinFloating ||
		!hasDelegatePackage ||
		delegatePackage.Ecosystem() != delegate.EcosystemNPM ||
		delegatePackage.Name() != "@upstash/context7-mcp" {
		t.Fatalf("delegate plan identity = %q", delegatePlan.IdentityKey())
	}
	projection := mustAggregateContribution(t, record)
	if projection.AggregateRoot().String() != aggregate.ClaudeProjectMCPConfigPath ||
		projection.ContentPath() != mcpcodec.ClaudeProjectMCPContentPath("context7") ||
		projection.Equivalence() != aggregate.EquivalenceCanonicalSemantic ||
		string(projection.CodecContractID()) != aggregate.ClaudeProjectMCPStdioAdapterV1 {
		t.Fatalf("projection = %#v, want Claude project MCP aggregate contribution", projection)
	}
	var entry mcpcodec.ClaudeProjectMCPServerEntry
	if err := json.Unmarshal([]byte(projection.CanonicalContribution()), &entry); err != nil {
		t.Fatalf("canonical projection is not JSON: %v", err)
	}
	if entry.Type != "stdio" ||
		entry.Command != "npx" ||
		len(entry.Args) != 2 ||
		entry.Args[0] != "-y" ||
		entry.Args[1] != "@upstash/context7-mcp" ||
		entry.Env["API_TOKEN"] != "${CONTEXT7_API_TOKEN}" {
		t.Fatalf("canonical projection entry = %#v, want MCP server shape with env reference", entry)
	}
	for _, forbidden := range []string{"CONTEXT7_API_TOKEN_VALUE", "literal-secret", "access_token"} {
		if strings.Contains(projection.CanonicalContribution(), forbidden) {
			t.Fatalf("canonical projection leaked forbidden value %q: %s", forbidden, projection.CanonicalContribution())
		}
	}
	if len(sourceEvents) != 0 {
		t.Fatalf("source events = %#v, want none for MCP-only lock", sourceEvents)
	}
	assertOnlySnapshotValidatedEvent(t, lockEvents, 2)
}

func TestBuildLocksMCPDelegatePlanStableAndDrifts(t *testing.T) {
	baseServer := testMCPServer(t, "context7", "npx", []string{"-y", "@upstash/context7-mcp@1.2.3"}, map[string]string{
		"API_TOKEN": "CONTEXT7_API_TOKEN",
	})
	baseFile := mustBuildMCPFile(t, baseServer)
	basePlan := mustMCPDelegatePlan(t, baseFile)
	samePlan := mustMCPDelegatePlan(t, mustBuildMCPFile(t, baseServer))
	if basePlan.IdentityKey() != samePlan.IdentityKey() {
		t.Fatalf("same manifest delegate identity = %q, want %q", samePlan.IdentityKey(), basePlan.IdentityKey())
	}
	if basePlan.PinPolicy() != delegate.PinPinned {
		t.Fatalf("base pin policy = %q, want pinned", basePlan.PinPolicy())
	}

	cases := []struct {
		name       string
		server     desiredmcp.Server
		wantRunner delegate.RunnerKind
		wantPin    delegate.PinPolicy
	}{
		{
			name:       "command and runner drift",
			server:     testMCPServer(t, "context7", "uvx", []string{"context7-mcp==1.2.3"}, map[string]string{"API_TOKEN": "CONTEXT7_API_TOKEN"}),
			wantRunner: delegate.RunnerUVX,
			wantPin:    delegate.PinPinned,
		},
		{
			name:       "args and package selector drift",
			server:     testMCPServer(t, "context7", "npx", []string{"-y", "@upstash/context7-mcp@2.0.0"}, map[string]string{"API_TOKEN": "CONTEXT7_API_TOKEN"}),
			wantRunner: delegate.RunnerNPX,
			wantPin:    delegate.PinPinned,
		},
		{
			name:       "env ref drift",
			server:     testMCPServer(t, "context7", "npx", []string{"-y", "@upstash/context7-mcp@1.2.3"}, map[string]string{"API_TOKEN": "OTHER_CONTEXT7_TOKEN"}),
			wantRunner: delegate.RunnerNPX,
			wantPin:    delegate.PinPinned,
		},
		{
			name:       "pin policy drift",
			server:     testMCPServer(t, "context7", "npx", []string{"-y", "@upstash/context7-mcp"}, map[string]string{"API_TOKEN": "CONTEXT7_API_TOKEN"}),
			wantRunner: delegate.RunnerNPX,
			wantPin:    delegate.PinFloating,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changedFile := mustBuildMCPFile(t, tc.server)
			changedPlan := mustMCPDelegatePlan(t, changedFile)
			if changedPlan.IdentityKey() == basePlan.IdentityKey() {
				t.Fatalf("delegate identity did not drift: %q", changedPlan.IdentityKey())
			}
			if changedPlan.Runner().Kind() != tc.wantRunner || changedPlan.PinPolicy() != tc.wantPin {
				t.Fatalf("changed delegate plan identity = %q", changedPlan.IdentityKey())
			}
			delta := lock.BuildDelta(baseFile, changedFile)
			changedSubjects := delta.EntriesWithStatus(lock.DeltaStatusChanged)
			if len(changedSubjects) != 1 || changedSubjects[0].Key.Key() != "context7" {
				t.Fatalf("subject delta = %#v, want context7 changed", changedSubjects)
			}
		})
	}
}

func TestBuildLocksAntigravityGlobalMCPServerWithoutDelegatePlan(t *testing.T) {
	file, err := buildWithTestOptions(
		context.Background(),
		lockEnvironment(t, desired.Spec{
			MCPServers: []desiredmcp.Server{
				testAntigravityGlobalMCPServer(t, "context7", "npx", []string{"-y", "@upstash/context7-mcp"}),
			},
		}),
		nil,
		Options{},
	)
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	if len(file.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one Antigravity MCP subject", file.Locked.Subjects())
	}

	record := file.Locked.Subjects()[0]
	subject := record.SubjectID()
	if subject.Kind() != topology.SubjectProjection ||
		subject.Namespace() != "antigravity-cli.global.mcp-server" ||
		subject.Key() != "context7" {
		t.Fatalf("subject = %#v, want Antigravity global MCP projection subject", subject)
	}
	if _, ok := record.DelegatePlan(); ok {
		t.Fatal("Antigravity global MCP projection unexpectedly carried delegate plan")
	}
	projection := mustAggregateContribution(t, record)
	if projection.Target() != target.TargetAntigravityCLI ||
		projection.Scope() != target.ScopeGlobal ||
		projection.AggregateRoot().String() != aggregate.AntigravityGlobalMCPConfigPath ||
		projection.ContentPath() != mcpcodec.AntigravityGlobalMCPContentPath("context7") ||
		string(projection.CodecContractID()) != aggregate.AntigravityGlobalMCPCommandAdapterV1 {
		t.Fatalf("projection = %#v, want Antigravity global MCP aggregate contribution", projection)
	}
	var entry mcpcodec.AntigravityGlobalMCPServerEntry
	if err := json.Unmarshal([]byte(projection.CanonicalContribution()), &entry); err != nil {
		t.Fatalf("canonical projection is not JSON: %v", err)
	}
	if entry.Command != "npx" ||
		len(entry.Args) != 2 ||
		entry.Args[0] != "-y" ||
		entry.Args[1] != "@upstash/context7-mcp" {
		t.Fatalf("canonical projection entry = %#v, want Antigravity command/args shape", entry)
	}
	for _, forbidden := range []string{`"type"`, `"env"`, "CONTEXT7_API_TOKEN"} {
		if strings.Contains(projection.CanonicalContribution(), forbidden) {
			t.Fatalf("canonical projection leaked forbidden value %q: %s", forbidden, projection.CanonicalContribution())
		}
	}
}

func TestBuildLocksClaudeGlobalMCPServerWithoutDelegatePlan(t *testing.T) {
	file, err := buildWithTestOptions(
		context.Background(),
		lockEnvironment(t, desired.Spec{
			MCPServers: []desiredmcp.Server{
				testClaudeGlobalMCPServer(t, "context7", "npx", []string{"-y", "@upstash/context7-mcp"}),
			},
		}),
		nil,
		Options{},
	)
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	if len(file.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one Claude global MCP subject", file.Locked.Subjects())
	}

	record := file.Locked.Subjects()[0]
	subject := record.SubjectID()
	if subject.Kind() != topology.SubjectProjection ||
		subject.Namespace() != "claude-code.global.mcp-server" ||
		subject.Key() != "context7" {
		t.Fatalf("subject = %#v, want Claude global MCP projection subject", subject)
	}
	if _, ok := record.DelegatePlan(); ok {
		t.Fatal("Claude global MCP projection unexpectedly carried delegate plan")
	}
	projection := mustAggregateContribution(t, record)
	if projection.Target() != target.TargetClaudeCode ||
		projection.Scope() != target.ScopeGlobal ||
		projection.AggregateRoot().String() != aggregate.ClaudeGlobalMCPConfigPath ||
		projection.ContentPath() != mcpcodec.ClaudeGlobalMCPContentPath("context7") ||
		string(projection.CodecContractID()) != aggregate.ClaudeGlobalMCPStdioEnvAdapterV1 {
		t.Fatalf("projection = %#v, want Claude global MCP aggregate contribution", projection)
	}
	var entry mcpcodec.ClaudeGlobalMCPServerEntry
	if err := json.Unmarshal([]byte(projection.CanonicalContribution()), &entry); err != nil {
		t.Fatalf("canonical projection is not JSON: %v", err)
	}
	if entry.Type != "stdio" ||
		entry.Command != "npx" ||
		len(entry.Args) != 2 ||
		entry.Args[0] != "-y" ||
		entry.Args[1] != "@upstash/context7-mcp" ||
		len(entry.Env) != 0 {
		t.Fatalf("canonical projection entry = %#v, want Claude global stdio command/args shape with empty env", entry)
	}
	for _, forbidden := range []string{"CONTEXT7_API_TOKEN", "literal-secret", "access_token"} {
		if strings.Contains(projection.CanonicalContribution(), forbidden) {
			t.Fatalf("canonical projection leaked forbidden value %q: %s", forbidden, projection.CanonicalContribution())
		}
	}
}

func TestBuildLocksOpenCodeProjectMCPServerWithoutDelegatePlan(t *testing.T) {
	file, err := buildWithTestOptions(
		context.Background(),
		lockEnvironment(t, desired.Spec{
			MCPServers: []desiredmcp.Server{
				testOpenCodeProjectMCPServer(t, "context7", "npx", []string{"-y", "@upstash/context7-mcp"}),
			},
		}),
		nil,
		Options{},
	)
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	if len(file.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one OpenCode MCP subject", file.Locked.Subjects())
	}

	record := file.Locked.Subjects()[0]
	subject := record.SubjectID()
	if subject.Kind() != topology.SubjectProjection ||
		subject.Namespace() != "opencode.project.mcp-server" ||
		subject.Key() != "context7" {
		t.Fatalf("subject = %#v, want OpenCode project MCP projection subject", subject)
	}
	if _, ok := record.DelegatePlan(); ok {
		t.Fatal("OpenCode project MCP projection unexpectedly carried delegate plan")
	}
	projection := mustAggregateContribution(t, record)
	if projection.Target() != target.TargetOpenCode ||
		projection.Scope() != target.ScopeProject ||
		projection.AggregateRoot().String() != aggregate.OpenCodeProjectMCPConfigPath ||
		projection.ContentPath() != mcpcodec.OpenCodeProjectMCPContentPath("context7") ||
		string(projection.CodecContractID()) != aggregate.OpenCodeProjectMCPLocalCommandV1 {
		t.Fatalf("projection = %#v, want OpenCode project MCP aggregate contribution", projection)
	}
	var entry mcpcodec.OpenCodeProjectMCPServerEntry
	if err := json.Unmarshal([]byte(projection.CanonicalContribution()), &entry); err != nil {
		t.Fatalf("canonical projection is not JSON: %v", err)
	}
	if entry.Type != "local" ||
		len(entry.Command) != 3 ||
		entry.Command[0] != "npx" ||
		entry.Command[1] != "-y" ||
		entry.Command[2] != "@upstash/context7-mcp" {
		t.Fatalf("canonical projection entry = %#v, want OpenCode local command argv shape", entry)
	}
	for _, forbidden := range []string{`"args"`, `"env"`, `"cwd"`, `"enabled"`, `"timeout"`, "CONTEXT7_API_TOKEN"} {
		if strings.Contains(projection.CanonicalContribution(), forbidden) {
			t.Fatalf("canonical projection leaked forbidden value %q: %s", forbidden, projection.CanonicalContribution())
		}
	}
}

func TestBuildLocksOpenCodeGlobalMCPServerWithoutDelegatePlan(t *testing.T) {
	file, err := buildWithTestOptions(
		context.Background(),
		lockEnvironment(t, desired.Spec{
			MCPServers: []desiredmcp.Server{
				testOpenCodeGlobalMCPServer(t, "context7", "npx", []string{"-y", "@upstash/context7-mcp"}),
			},
		}),
		nil,
		Options{},
	)
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	if len(file.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one OpenCode global MCP subject", file.Locked.Subjects())
	}

	record := file.Locked.Subjects()[0]
	subject := record.SubjectID()
	if subject.Kind() != topology.SubjectProjection ||
		subject.Namespace() != "opencode.global.mcp-server" ||
		subject.Key() != "context7" {
		t.Fatalf("subject = %#v, want OpenCode global MCP projection subject", subject)
	}
	if _, ok := record.DelegatePlan(); ok {
		t.Fatal("OpenCode global MCP projection unexpectedly carried delegate plan")
	}
	projection := mustAggregateContribution(t, record)
	if projection.Target() != target.TargetOpenCode ||
		projection.Scope() != target.ScopeGlobal ||
		projection.AggregateRoot().String() != aggregate.OpenCodeGlobalMCPConfigPath ||
		projection.ContentPath() != mcpcodec.OpenCodeGlobalMCPContentPath("context7") ||
		string(projection.CodecContractID()) != aggregate.OpenCodeGlobalMCPLocalEnvV1 {
		t.Fatalf("projection = %#v, want OpenCode global MCP aggregate contribution", projection)
	}
	var entry mcpcodec.OpenCodeProjectMCPServerEntry
	if err := json.Unmarshal([]byte(projection.CanonicalContribution()), &entry); err != nil {
		t.Fatalf("canonical projection is not JSON: %v", err)
	}
	if entry.Type != "local" ||
		len(entry.Command) != 3 ||
		entry.Command[0] != "npx" ||
		entry.Command[1] != "-y" ||
		entry.Command[2] != "@upstash/context7-mcp" {
		t.Fatalf("canonical projection entry = %#v, want OpenCode global local command argv shape", entry)
	}
	for _, forbidden := range []string{`"args"`, `"env"`, `"environment"`, `"cwd"`, `"enabled"`, `"timeout"`, "CONTEXT7_API_TOKEN"} {
		if strings.Contains(projection.CanonicalContribution(), forbidden) {
			t.Fatalf("canonical projection leaked forbidden value %q: %s", forbidden, projection.CanonicalContribution())
		}
	}
}

func TestBuildLocksCodexGlobalMCPServerWithoutDelegatePlan(t *testing.T) {
	const resolvedSecret = "must-not-enter-lock"
	t.Setenv("CONTEXT7_API_TOKEN", resolvedSecret)
	file, err := buildWithTestOptions(
		context.Background(),
		lockEnvironment(t, desired.Spec{
			MCPServers: []desiredmcp.Server{
				testMCPServerForPlacement(
					t,
					"context7",
					target.TargetCodex,
					target.ScopeGlobal,
					"npx",
					[]string{"-y", "@upstash/context7-mcp"},
					map[string]string{"CONTEXT7_API_TOKEN": "CONTEXT7_API_TOKEN"},
				),
			},
		}),
		nil,
		Options{},
	)
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	if len(file.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one Codex global MCP subject", file.Locked.Subjects())
	}

	record := file.Locked.Subjects()[0]
	subject := record.SubjectID()
	if subject.Kind() != topology.SubjectProjection ||
		subject.Namespace() != "codex.global.mcp-server" ||
		subject.Key() != "context7" {
		t.Fatalf("subject = %#v, want Codex global MCP projection subject", subject)
	}
	if _, ok := record.DelegatePlan(); ok {
		t.Fatal("Codex global MCP projection unexpectedly carried delegate plan")
	}
	projection := mustAggregateContribution(t, record)
	if projection.Target() != target.TargetCodex ||
		projection.Scope() != target.ScopeGlobal ||
		projection.AggregateRoot().String() != aggregate.CodexGlobalMCPConfigPath ||
		projection.ContentPath() != mcpcodec.CodexGlobalMCPContentPath("context7") ||
		string(projection.CodecContractID()) != aggregate.CodexGlobalMCPStdioEnvVarsV1 {
		t.Fatalf("projection = %#v, want Codex global MCP aggregate contribution", projection)
	}
	for _, want := range []string{
		`command = "npx"`,
		`args = ["-y", "@upstash/context7-mcp"]`,
		`env_vars = ["CONTEXT7_API_TOKEN"]`,
	} {
		if !strings.Contains(projection.CanonicalContribution(), want) {
			t.Fatalf("canonical projection = %s, want %q", projection.CanonicalContribution(), want)
		}
	}
	for _, forbidden := range []string{`"type"`, `env =`, `cwd`, `url`, `headers`, resolvedSecret} {
		if strings.Contains(projection.CanonicalContribution(), forbidden) {
			t.Fatalf("canonical projection leaked forbidden value %q: %s", forbidden, projection.CanonicalContribution())
		}
	}
}

func TestBuildSeparatesMCPProjectionSubjectNamespacesByTargetAndScope(t *testing.T) {
	file, err := buildWithTestOptions(
		context.Background(),
		lockEnvironment(t, desired.Spec{
			MCPServers: []desiredmcp.Server{
				testMCPServer(t, "shared", "node", nil, nil),
				testClaudeGlobalMCPServer(t, "shared-claude-global", "node", nil),
				testAntigravityGlobalMCPServer(t, "shared-agy", "node", nil),
				testOpenCodeProjectMCPServer(t, "shared-oc", "node", nil),
				testOpenCodeGlobalMCPServer(t, "shared-oc-global", "node", nil),
				testCodexGlobalMCPServer(t, "shared-codex-global", "node", nil),
			},
		}),
		nil,
		Options{},
	)
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	if len(file.Locked.Subjects()) != 6 {
		t.Fatalf("locked subjects = %#v, want six target-scoped projection subjects", file.Locked.Subjects())
	}
	got := make([]string, 0, len(file.Locked.Subjects()))
	for _, record := range file.Locked.Subjects() {
		subject := record.SubjectID()
		got = append(got, subject.Namespace()+"/"+subject.Key())
	}
	want := []string{
		"claude-code.project.mcp-server/shared",
		"antigravity-cli.global.mcp-server/shared-agy",
		"claude-code.global.mcp-server/shared-claude-global",
		"codex.global.mcp-server/shared-codex-global",
		"opencode.project.mcp-server/shared-oc",
		"opencode.global.mcp-server/shared-oc-global",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("subjects = %#v, want %#v", got, want)
	}
}

func TestBuildRejectsAntigravityGlobalMCPEnvBeforeSnapshotWrite(t *testing.T) {
	server := testMCPServerForPlacement(
		t, "context7", target.TargetAntigravityCLI, target.ScopeGlobal,
		"npx", []string{"-y", "@upstash/context7-mcp"},
		map[string]string{"API_TOKEN": "CONTEXT7_API_TOKEN"},
	)

	_, err := buildWithTestOptions(context.Background(), lockEnvironment(t, desired.Spec{
		MCPServers: []desiredmcp.Server{server},
	}), nil, Options{})
	if err == nil || !strings.Contains(err.Error(), "does not support env") {
		t.Fatalf("BuildWithOptions error = %v, want Antigravity env rejection", err)
	}
}

func TestBuildLocksClaudeGlobalMCPEnvReferencesWithoutValues(t *testing.T) {
	const secret = "claude-global-lock-secret"
	t.Setenv("CONTEXT7_API_TOKEN", secret)
	server := testMCPServerForPlacement(
		t, "context7", target.TargetClaudeCode, target.ScopeGlobal,
		"npx", []string{"-y", "@upstash/context7-mcp"},
		map[string]string{"API_TOKEN": "CONTEXT7_API_TOKEN"},
	)

	file, err := buildWithTestOptions(context.Background(), lockEnvironment(t, desired.Spec{
		MCPServers: []desiredmcp.Server{server},
	}), nil, Options{})
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	if len(file.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one Claude global MCP subject", file.Locked.Subjects())
	}
	projection := mustAggregateContribution(t, file.Locked.Subjects()[0])
	var entry mcpcodec.ClaudeGlobalMCPServerEntry
	if err := json.Unmarshal([]byte(projection.CanonicalContribution()), &entry); err != nil {
		t.Fatalf("canonical projection is not JSON: %v", err)
	}
	if len(entry.Env) != 1 || entry.Env["API_TOKEN"] != "${CONTEXT7_API_TOKEN}" {
		t.Fatalf("canonical projection entry = %#v, want exact host environment reference", entry)
	}
	if strings.Contains(projection.CanonicalContribution(), secret) {
		t.Fatalf("canonical projection leaked environment value: %s", projection.CanonicalContribution())
	}
}

func TestBuildLocksOpenCodeGlobalMCPEnvReferencesWithoutValues(t *testing.T) {
	const secret = "opencode-global-lock-secret"
	t.Setenv("SOURCE_TOKEN", secret)
	server := testMCPServerForPlacement(
		t, "context7", target.TargetOpenCode, target.ScopeGlobal,
		"npx", []string{"-y", "@upstash/context7-mcp"},
		map[string]string{"CHILD_TOKEN": "SOURCE_TOKEN"},
	)

	file, err := buildWithTestOptions(context.Background(), lockEnvironment(t, desired.Spec{
		MCPServers: []desiredmcp.Server{server},
	}), nil, Options{})
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	if len(file.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one OpenCode global MCP subject", file.Locked.Subjects())
	}
	projection := mustAggregateContribution(t, file.Locked.Subjects()[0])
	var entry mcpcodec.OpenCodeGlobalMCPServerEntry
	if err := json.Unmarshal([]byte(projection.CanonicalContribution()), &entry); err != nil {
		t.Fatalf("canonical projection is not JSON: %v", err)
	}
	if len(entry.Environment) != 1 || entry.Environment["CHILD_TOKEN"] != "{env:SOURCE_TOKEN}" {
		t.Fatalf("canonical projection entry = %#v, want exact OpenCode host environment reference", entry)
	}
	if strings.Contains(projection.CanonicalContribution(), secret) {
		t.Fatalf("canonical projection leaked environment value: %s", projection.CanonicalContribution())
	}
}

func TestBuildRejectsOpenCodeProjectMCPEnvBeforeSnapshotWrite(t *testing.T) {
	server := testMCPServerForPlacement(
		t, "context7", target.TargetOpenCode, target.ScopeProject,
		"npx", []string{"-y", "@upstash/context7-mcp"},
		map[string]string{"API_TOKEN": "CONTEXT7_API_TOKEN"},
	)

	_, err := buildWithTestOptions(context.Background(), lockEnvironment(t, desired.Spec{
		MCPServers: []desiredmcp.Server{server},
	}), nil, Options{})
	if err == nil || !strings.Contains(err.Error(), "does not support env") {
		t.Fatalf("BuildWithOptions error = %v, want OpenCode env rejection", err)
	}
}

func TestBuildLocksSameMCPServerIDAcrossDifferentPlacements(t *testing.T) {
	transport := desiredtest.MCPStdio(
		t,
		desiredtest.MCPCommand(t, "npx"),
		[]string{"-y", "@upstash/context7-mcp"},
		nil,
	)
	server := desiredtest.MCPServer(t, desiredmcp.Spec{
		Name: "context7",
		Bindings: []desiredmcp.Binding{
			desiredtest.MCPBinding(t, target.TargetClaudeCode, target.ScopeProject, transport, desiredmcp.OnAbsentRemoveBinding),
			desiredtest.MCPBinding(t, target.TargetCodex, target.ScopeProject, transport, desiredmcp.OnAbsentRemoveBinding),
		},
	})

	file, err := buildWithTestOptions(context.Background(), lockEnvironment(t, desired.Spec{
		MCPServers: []desiredmcp.Server{server},
	}), nil, Options{})
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	if len(file.Locked.Subjects()) != 2 {
		t.Fatalf("locked subjects = %#v, want two placement-specific subjects", file.Locked.Subjects())
	}
	subjects := []topology.SubjectID{
		file.Locked.Subjects()[0].SubjectID(),
		file.Locked.Subjects()[1].SubjectID(),
	}
	want := []topology.SubjectID{
		mustBuildSubjectID(t, topology.SubjectProjection, "claude-code.project.mcp-server", "context7"),
		mustBuildSubjectID(t, topology.SubjectProjection, "codex.project.mcp-server", "context7"),
	}
	if subjects[0] != want[0] || subjects[1] != want[1] {
		t.Fatalf("subjects = %#v, want %#v", subjects, want)
	}
}

func mustBuildSubjectID(t *testing.T, kind topology.SubjectKind, namespace string, key string) topology.SubjectID {
	t.Helper()
	subject, err := topology.NewSubjectID(kind, namespace, key)
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	return subject
}

func TestBuildRetainsBindingLocalMCPCommandAndArgs(t *testing.T) {
	claudeTransport := desiredtest.MCPStdio(
		t,
		desiredtest.MCPCommand(t, "claude-runner"),
		[]string{"--profile", "claude"},
		nil,
	)
	codexTransport := desiredtest.MCPStdio(
		t,
		desiredtest.MCPCommand(t, "codex-runner"),
		[]string{"--profile", "codex"},
		nil,
	)
	server := desiredtest.MCPServer(t, desiredmcp.Spec{
		Name: "shared",
		Bindings: []desiredmcp.Binding{
			desiredtest.MCPBinding(t, target.TargetClaudeCode, target.ScopeProject, claudeTransport, desiredmcp.OnAbsentRemoveBinding),
			desiredtest.MCPBinding(t, target.TargetCodex, target.ScopeProject, codexTransport, desiredmcp.OnAbsentRemoveBinding),
		},
	})

	file, err := buildWithTestOptions(context.Background(), lockEnvironment(t, desired.Spec{
		MCPServers: []desiredmcp.Server{server},
	}), nil, Options{})
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	if len(file.Locked.Subjects()) != 2 {
		t.Fatalf("locked subjects = %#v, want two binding-local projections", file.Locked.Subjects())
	}

	for _, record := range file.Locked.Subjects() {
		projection := mustAggregateContribution(t, record)
		switch projection.Target() {
		case target.TargetClaudeCode:
			var entry mcpcodec.ClaudeProjectMCPServerEntry
			if err := json.Unmarshal([]byte(projection.CanonicalContribution()), &entry); err != nil {
				t.Fatalf("decode Claude projection: %v", err)
			}
			if entry.Command != "claude-runner" || len(entry.Args) != 2 || entry.Args[1] != "claude" {
				t.Fatalf("Claude projection = %#v, want Claude binding transport", entry)
			}
			if strings.Contains(projection.CanonicalContribution(), "codex-runner") || strings.Contains(projection.CanonicalContribution(), `"codex"`) {
				t.Fatalf("Claude projection contains Codex binding facts: %s", projection.CanonicalContribution())
			}
		case target.TargetCodex:
			if !strings.Contains(projection.CanonicalContribution(), `command = "codex-runner"`) ||
				!strings.Contains(projection.CanonicalContribution(), `args = ["--profile", "codex"]`) {
				t.Fatalf("Codex projection = %q, want Codex binding transport", projection.CanonicalContribution())
			}
			if strings.Contains(projection.CanonicalContribution(), "claude-runner") || strings.Contains(projection.CanonicalContribution(), `"claude"`) {
				t.Fatalf("Codex projection contains Claude binding facts: %s", projection.CanonicalContribution())
			}
		default:
			t.Fatalf("unexpected projection target %q", projection.Target())
		}
	}
}

func mustAggregateContribution(
	t *testing.T,
	contract lock.LockedSubjectContract,
) aggregate.ManagedContribution {
	t.Helper()
	realization, ok := contract.Realization()
	if !ok {
		t.Fatalf("MCP contract %q is missing realization", contract.SubjectID())
	}
	projection, ok := realization.ManagedAggregateContribution()
	if !ok {
		t.Fatalf("MCP contract %q realization kind = %q, want aggregate contribution", contract.SubjectID(), realization.Kind())
	}
	return projection
}

func testMCPServer(t *testing.T, id string, command string, args []string, env map[string]string) desiredmcp.Server {
	t.Helper()
	return testMCPServerForPlacement(t, id, target.TargetClaudeCode, target.ScopeProject, command, args, env)
}

func testAntigravityGlobalMCPServer(t *testing.T, id string, command string, args []string) desiredmcp.Server {
	t.Helper()
	return testMCPServerForPlacement(t, id, target.TargetAntigravityCLI, target.ScopeGlobal, command, args, nil)
}

func testClaudeGlobalMCPServer(t *testing.T, id string, command string, args []string) desiredmcp.Server {
	t.Helper()
	return testMCPServerForPlacement(t, id, target.TargetClaudeCode, target.ScopeGlobal, command, args, nil)
}

func testOpenCodeProjectMCPServer(t *testing.T, id string, command string, args []string) desiredmcp.Server {
	t.Helper()
	return testMCPServerForPlacement(t, id, target.TargetOpenCode, target.ScopeProject, command, args, nil)
}

func testOpenCodeGlobalMCPServer(t *testing.T, id string, command string, args []string) desiredmcp.Server {
	t.Helper()
	return testMCPServerForPlacement(t, id, target.TargetOpenCode, target.ScopeGlobal, command, args, nil)
}

func testCodexGlobalMCPServer(t *testing.T, id string, command string, args []string) desiredmcp.Server {
	t.Helper()
	return testMCPServerForPlacement(t, id, target.TargetCodex, target.ScopeGlobal, command, args, nil)
}

func testMCPServerForPlacement(
	t *testing.T,
	id string,
	selectedTarget target.Target,
	scope target.Scope,
	command string,
	args []string,
	env map[string]string,
) desiredmcp.Server {
	t.Helper()
	envReferences := make(map[string]desiredmcp.EnvReference, len(env))
	for name, fromEnv := range env {
		envReferences[name] = desiredtest.MCPEnvReference(t, fromEnv)
	}
	transport := desiredtest.MCPStdio(t, desiredtest.MCPCommand(t, command), args, envReferences)
	binding := desiredtest.MCPBinding(t, selectedTarget, scope, transport, desiredmcp.OnAbsentRemoveBinding)
	return desiredtest.MCPServer(t, desiredmcp.Spec{Name: id, Bindings: []desiredmcp.Binding{binding}})
}

func mustBuildMCPFile(t *testing.T, server desiredmcp.Server) lock.File {
	t.Helper()
	file, err := buildWithTestOptions(context.Background(), lockEnvironment(t, desired.Spec{
		MCPServers: []desiredmcp.Server{server},
	}), nil, Options{})
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	if len(file.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one", file.Locked.Subjects())
	}
	return file
}

func mustMCPDelegatePlan(t *testing.T, file lock.File) delegate.DelegatePlan {
	t.Helper()
	plan, ok := file.Locked.Subjects()[0].DelegatePlan()
	if !ok {
		t.Fatal("locked MCP subject is missing delegate plan")
	}
	return plan
}

func assertMCPSubjectRecord(t *testing.T, record lock.LockedSubjectContract, serverID string) {
	t.Helper()
	subject := record.SubjectID()
	if subject.Kind() != topology.SubjectProjection ||
		subject.Namespace() != "claude-code.project.mcp-server" ||
		subject.Key() != serverID {
		t.Fatalf("subject = %#v, want Claude project MCP projection subject %q", subject, serverID)
	}
	realization, ok := record.Realization()
	if !ok {
		t.Fatal("MCP subject is missing aggregate realization")
	}
	contribution, ok := realization.ManagedAggregateContribution()
	if !ok {
		t.Fatal("MCP subject realization is not an aggregate contribution")
	}
	if record.Ownership() != lock.OwnershipManifest ||
		record.OnAbsent() != lock.OnAbsentRemoveBinding ||
		string(contribution.CodecContractID()) != aggregate.ClaudeProjectMCPStdioAdapterV1 {
		t.Fatalf("record metadata = ownership %q on_absent %q codec %q",
			record.Ownership(), record.OnAbsent(), contribution.CodecContractID())
	}
}

func assertOnlySnapshotValidatedEvent(t *testing.T, events []Event, count int) {
	t.Helper()
	if len(events) != 1 {
		t.Fatalf("events = %#v, want only snapshot validation", events)
	}
	event := events[0]
	if event.Kind != EventSnapshotValidated || event.Stage != EventStageSnapshot || event.Count != count {
		t.Fatalf("event = %#v, want snapshot validated count %d", event, count)
	}
}
