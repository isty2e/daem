package mcp_test

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization/delegate"
	mcpdelegate "github.com/isty2e/daem/internal/realization/delegate/mcp"
	"github.com/isty2e/daem/internal/target"
)

func TestMCPBindingDelegatePlanPreservesCanonicalProjectionFields(t *testing.T) {
	server := validDelegateMCPServer(
		t,
		"npx",
		[]string{"-y", "@upstash/context7-mcp@1.2.3"},
		map[string]string{"API_TOKEN": "CONTEXT7_API_TOKEN"},
	)
	plan := mustMCPDelegatePlan(t, server)

	assertDelegatePlan(t, plan, delegate.RunnerNPX, "npx", []string{"-y", "@upstash/context7-mcp@1.2.3"}, "CONTEXT7_API_TOKEN")
	env := plan.Env().Bindings()
	if len(env) != 1 || env[0].Name() != "API_TOKEN" || env[0].SourceName() != "CONTEXT7_API_TOKEN" {
		t.Fatalf("delegate env bindings = %#v, want API_TOKEN <- CONTEXT7_API_TOKEN", env)
	}
	assertPackageRef(t, plan, delegate.EcosystemNPM, "@upstash/context7-mcp", "1.2.3", delegate.PinPinned)
}

func TestMCPBindingDelegatePlanAccountsForEveryNPXPackage(t *testing.T) {
	server := validDelegateMCPServer(
		t,
		"npx",
		[]string{
			"--package=server@1.2.3",
			"--package=helper@latest",
			"server",
		},
		nil,
	)
	plan := mustMCPDelegatePlan(t, server)

	if got := plan.PinPolicy(); got != delegate.PinFloating {
		t.Fatalf("PinPolicy() = %q, want floating because helper@latest also enters the execution environment", got)
	}
}

func TestMCPBindingDelegatePlanCoversSupportedCommandShapes(t *testing.T) {
	tests := []struct {
		name             string
		command          string
		args             []string
		wantRunner       delegate.RunnerKind
		wantPackage      bool
		wantEcosystem    delegate.PackageEcosystem
		wantPackageName  string
		wantSelector     string
		wantPinPolicy    delegate.PinPolicy
		wantFirstArgText string
	}{
		{name: "floating npx package", command: "npx", args: []string{"server"}, wantRunner: delegate.RunnerNPX, wantPackage: true, wantEcosystem: delegate.EcosystemNPM, wantPackageName: "server", wantPinPolicy: delegate.PinFloating},
		{name: "scoped pinned npx package", command: "npx", args: []string{"--yes", "@scope/server@2.0.0"}, wantRunner: delegate.RunnerNPX, wantPackage: true, wantEcosystem: delegate.EcosystemNPM, wantPackageName: "@scope/server", wantSelector: "2.0.0", wantPinPolicy: delegate.PinPinned},
		{name: "scoped ranged npx package", command: "npx", args: []string{"--yes", "@scope/server@^2.0.0"}, wantRunner: delegate.RunnerNPX, wantPackage: true, wantEcosystem: delegate.EcosystemNPM, wantPackageName: "@scope/server", wantSelector: "^2.0.0", wantPinPolicy: delegate.PinFloating},
		{name: "uvx package", command: "uvx", args: []string{"mcp-server==0.4.0"}, wantRunner: delegate.RunnerUVX, wantPackage: true, wantEcosystem: delegate.EcosystemPython, wantPackageName: "mcp-server", wantSelector: "0.4.0", wantPinPolicy: delegate.PinPinned},
		{name: "uvx range", command: "uvx", args: []string{"mcp-server>=0.4,<1"}, wantRunner: delegate.RunnerUVX, wantPackage: true, wantEcosystem: delegate.EcosystemPython, wantPackageName: "mcp-server", wantSelector: ">=0.4,<1", wantPinPolicy: delegate.PinFloating},
		{name: "uvx wildcard", command: "uvx", args: []string{"mcp-server==0.4.*"}, wantRunner: delegate.RunnerUVX, wantPackage: true, wantEcosystem: delegate.EcosystemPython, wantPackageName: "mcp-server", wantSelector: "==0.4.*", wantPinPolicy: delegate.PinFloating},
		{name: "docker tagged image", command: "docker", args: []string{"run", "--rm", "ghcr.io/acme/server:1.2.3"}, wantRunner: delegate.RunnerDocker, wantPackage: true, wantEcosystem: delegate.EcosystemContainer, wantPackageName: "ghcr.io/acme/server", wantSelector: "1.2.3", wantPinPolicy: delegate.PinFloating},
		{name: "plain node script", command: "node", args: []string{"scripts/mcp-server.js", "--stdio"}, wantRunner: delegate.RunnerPlain, wantPinPolicy: delegate.PinNotApplicable, wantFirstArgText: "scripts/mcp-server.js"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := mustMCPDelegatePlan(t, validDelegateMCPServer(t, test.command, test.args, nil))
			assertDelegatePlan(t, plan, test.wantRunner, test.command, test.args, "")
			if test.wantPackage {
				assertPackageRef(t, plan, test.wantEcosystem, test.wantPackageName, test.wantSelector, test.wantPinPolicy)
			} else if packageRefs := plan.PackageRefs(); len(packageRefs) != 0 {
				t.Fatalf("PackageRefs() = %#v, want absent", packageRefs)
			}
			if test.wantFirstArgText != "" && plan.Command().Args()[0] != test.wantFirstArgText {
				t.Fatalf("first arg = %q, want %q", plan.Command().Args()[0], test.wantFirstArgText)
			}
		})
	}
}

func TestMCPBindingDelegatePlanRejectsBindingFromAnotherServer(t *testing.T) {
	server := validDelegateMCPServer(t, "npx", []string{"server@1"}, nil)
	foreignServer := validDelegateMCPServer(t, "npx", []string{"server@2"}, nil)

	_, err := mcpdelegate.MCPBindingDelegatePlan(server, onlyBinding(t, foreignServer))
	if err == nil || errors.Unwrap(err) == nil || !strings.Contains(errors.Unwrap(err).Error(), "binding is not owned by MCP server") {
		t.Fatalf("error = %v, want foreign-binding ownership rejection", err)
	}
}

func TestMCPBindingDelegatePlanRejectsActionableUnsupportedForms(t *testing.T) {
	tests := []struct {
		name   string
		server desiredmcp.Server
		want   mcpdelegate.MCPDelegatePlanReasonCode
	}{
		{name: "unsupported target", server: validCodexMCPServer(t, "bad-target", target.ScopeProject), want: mcpdelegate.MCPDelegatePlanReasonUnsupportedServer},
		{name: "antigravity direct projection", server: validAntigravityMCPServer(t, "antigravity"), want: mcpdelegate.MCPDelegatePlanReasonUnsupportedServer},
		{name: "missing npx package", server: validDelegateMCPServer(t, "npx", []string{"-y"}, nil), want: mcpdelegate.MCPDelegatePlanReasonMissingPackage},
		{name: "invalid package", server: validDelegateMCPServer(t, "npx", []string{"scope/server"}, nil), want: mcpdelegate.MCPDelegatePlanReasonInvalidPackage},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMCPDelegateReason(t, test.want, test.server)
		})
	}
}

func TestMCPStdioDelegatePlanSeparatesInvocationIdentityFromHostAdmission(t *testing.T) {
	server := mustAmbientMCPServer(
		t,
		"portable",
		target.TargetCodex,
		target.ScopeGlobal,
		"npx",
		[]string{"-y", "@scope/server"},
		nil,
	)
	binding := onlyBinding(t, server)
	stdio, ok := binding.Transport().Stdio()
	if !ok {
		t.Fatal("binding transport is not stdio")
	}

	plan, err := mcpdelegate.MCPStdioDelegatePlan(stdio)
	if err != nil {
		t.Fatalf("MCPStdioDelegatePlan returned error: %v", err)
	}
	assertPackageRef(t, plan, delegate.EcosystemNPM, "@scope/server", "", delegate.PinFloating)

	_, err = mcpdelegate.MCPBindingDelegatePlan(server, binding)
	var delegateError *mcpdelegate.MCPDelegatePlanError
	if !errors.As(err, &delegateError) || delegateError.Code() != mcpdelegate.MCPDelegatePlanReasonUnsupportedServer {
		t.Fatalf("MCPBindingDelegatePlan error = %v, want host admission rejection", err)
	}
}

func validDelegateMCPServer(t *testing.T, command string, args []string, env map[string]string) desiredmcp.Server {
	t.Helper()
	return mustAmbientMCPServer(t, "delegate", target.TargetClaudeCode, target.ScopeProject, command, args, env)
}

func validCodexMCPServer(t *testing.T, id string, scope target.Scope) desiredmcp.Server {
	t.Helper()
	return mustAmbientMCPServer(t, id, target.TargetCodex, scope, "npx", nil, nil)
}

func validAntigravityMCPServer(t *testing.T, id string) desiredmcp.Server {
	t.Helper()
	return mustAmbientMCPServer(t, id, target.TargetAntigravityCLI, target.ScopeGlobal, "npx", nil, nil)
}

func mustAmbientMCPServer(
	t *testing.T,
	id string,
	selected target.Target,
	scope target.Scope,
	command string,
	args []string,
	env map[string]string,
) desiredmcp.Server {
	t.Helper()
	references := make(map[string]desiredmcp.EnvReference, len(env))
	for name, fromEnv := range env {
		references[name] = desiredtest.MCPEnvReference(t, fromEnv)
	}
	transport := desiredtest.MCPStdio(t, desiredtest.MCPCommand(t, command), args, references)
	binding := desiredtest.MCPBinding(t, selected, scope, transport, desiredmcp.OnAbsentRemoveBinding)
	return desiredtest.MCPServer(t, desiredmcp.Spec{Name: id, Bindings: []desiredmcp.Binding{binding}})
}

func onlyBinding(t *testing.T, server desiredmcp.Server) desiredmcp.Binding {
	t.Helper()
	bindings := server.Bindings()
	if len(bindings) != 1 {
		t.Fatalf("server bindings = %d, want 1", len(bindings))
	}
	return bindings[0]
}

func mustMCPDelegatePlan(t *testing.T, server desiredmcp.Server) delegate.DelegatePlan {
	t.Helper()
	plan, err := mcpdelegate.MCPBindingDelegatePlan(server, onlyBinding(t, server))
	if err != nil {
		t.Fatalf("MCPBindingDelegatePlan returned error: %v", err)
	}
	return plan
}

func assertMCPDelegateReason(t *testing.T, want mcpdelegate.MCPDelegatePlanReasonCode, server desiredmcp.Server) {
	t.Helper()
	_, err := mcpdelegate.MCPBindingDelegatePlan(server, onlyBinding(t, server))
	if err == nil {
		t.Fatalf("MCPBindingDelegatePlan returned nil error, want %q", want)
	}
	var delegateError *mcpdelegate.MCPDelegatePlanError
	if !errors.As(err, &delegateError) {
		t.Fatalf("error has no MCP delegate reason: %T %v", err, err)
	}
	got := delegateError.Code()
	if got != want {
		t.Fatalf("reason = %q, want %q; err = %v", got, want, err)
	}
}

func assertDelegatePlan(t *testing.T, plan delegate.DelegatePlan, wantRunner delegate.RunnerKind, wantCommand string, wantArgs []string, wantEnv string) {
	t.Helper()
	if plan.Runner().Kind() != wantRunner {
		t.Fatalf("Runner().Kind() = %q, want %q", plan.Runner().Kind(), wantRunner)
	}
	if plan.Command().Executable() != wantCommand {
		t.Fatalf("Command().Executable() = %q, want %q", plan.Command().Executable(), wantCommand)
	}
	if !reflect.DeepEqual(plan.Command().Args(), wantArgs) {
		t.Fatalf("Command().Args() = %#v, want %#v", plan.Command().Args(), wantArgs)
	}
	if wantEnv != "" && !slices.Contains(plan.Env().SourceNames(), wantEnv) {
		t.Fatalf("Env().SourceNames() = %#v, want %q", plan.Env().SourceNames(), wantEnv)
	}
}

func assertPackageRef(
	t *testing.T,
	plan delegate.DelegatePlan,
	wantEcosystem delegate.PackageEcosystem,
	wantName string,
	wantSelector string,
	wantPin delegate.PinPolicy,
) {
	t.Helper()
	packageRefs := plan.PackageRefs()
	if len(packageRefs) != 1 {
		t.Fatalf("PackageRefs() = %#v, want one %q %q", packageRefs, wantEcosystem, wantName)
	}
	packageRef := packageRefs[0]
	if packageRef.Ecosystem() != wantEcosystem || packageRef.Name() != wantName || packageRef.Selector() != wantSelector {
		t.Fatalf("PackageRefs()[0] = %q %q %q, want %q %q %q", packageRef.Ecosystem(), packageRef.Name(), packageRef.Selector(), wantEcosystem, wantName, wantSelector)
	}
	if plan.PinPolicy() != wantPin {
		t.Fatalf("PinPolicy() = %q, want %q", plan.PinPolicy(), wantPin)
	}
}
