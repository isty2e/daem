package normalize

import (
	"fmt"
	"sort"
	"strings"

	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	"github.com/isty2e/daem/internal/desired"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/target"
)

// ExplicitMCPServer normalizes one fully specified declaration row into its
// canonical Desired server and binding. Manifest-default resolution remains in
// the whole-manifest normalizer.
func ExplicitMCPServer(server declarationcodec.MCPServer) (desiredmcp.Server, desiredmcp.Binding, error) {
	if len(server.Targets) != 1 {
		return desiredmcp.Server{}, desiredmcp.Binding{}, fmt.Errorf("mcp-server requires exactly one target")
	}
	if server.Transport != string(desiredmcp.TransportKindStdio) {
		return desiredmcp.Server{}, desiredmcp.Binding{}, fmt.Errorf("mcp-server transport: unsupported MCP transport %q", server.Transport)
	}
	selectedTarget, err := target.ParseTarget(server.Targets[0])
	if err != nil {
		return desiredmcp.Server{}, desiredmcp.Binding{}, fmt.Errorf("mcp-server target: %w", err)
	}
	selectedScope, err := target.ParseScope(server.Scope)
	if err != nil {
		return desiredmcp.Server{}, desiredmcp.Binding{}, fmt.Errorf("mcp-server scope: %w", err)
	}
	command, err := normalizeMCPCommand(server.Command)
	if err != nil {
		return desiredmcp.Server{}, desiredmcp.Binding{}, fmt.Errorf("mcp-server command: %w", err)
	}
	env, err := explicitMCPEnv(server.Env)
	if err != nil {
		return desiredmcp.Server{}, desiredmcp.Binding{}, err
	}
	transport, err := desiredmcp.NewStdioTransport(command, server.Args, env)
	if err != nil {
		return desiredmcp.Server{}, desiredmcp.Binding{}, fmt.Errorf("mcp-server: %w", err)
	}
	binding, err := desiredmcp.NewBinding(
		selectedTarget,
		selectedScope,
		transport,
		desiredmcp.OnAbsentRemoveBinding,
	)
	if err != nil {
		return desiredmcp.Server{}, desiredmcp.Binding{}, fmt.Errorf("mcp-server: %w", err)
	}
	canonical, err := desiredmcp.New(desiredmcp.Spec{Name: server.Name, Bindings: []desiredmcp.Binding{binding}})
	if err != nil {
		return desiredmcp.Server{}, desiredmcp.Binding{}, fmt.Errorf("mcp-server: %w", err)
	}
	return canonical, binding, nil
}

func explicitMCPEnv(raw map[string]declarationcodec.MCPEnvReference) (map[string]desiredmcp.EnvReference, error) {
	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	sort.Strings(names)

	env := make(map[string]desiredmcp.EnvReference, len(raw))
	for _, name := range names {
		reference, err := desiredmcp.NewEnvReference(raw[name].FromEnv)
		if err != nil {
			return nil, fmt.Errorf("mcp-server env %q: %w", name, err)
		}
		env[name] = reference
	}
	return env, nil
}

func normalizeMCPServers(rawServers []declaration.MCPServer, defaultTargets []target.Target, defaults desired.Defaults) ([]desiredmcp.Server, error) {
	servers := make([]desiredmcp.Server, 0, len(rawServers))
	serverIndexes := make(map[string]int, len(rawServers))

	for index, raw := range rawServers {
		context := fmt.Sprintf("mcp_server[%d]", index)

		name, err := requiredExactString(raw.Name, context+".name")
		if err != nil {
			return nil, err
		}
		serverTargets, err := targetsWithDefault(raw.Targets, defaultTargets, context+".targets")
		if err != nil {
			return nil, err
		}
		if len(serverTargets) != 1 {
			return nil, fmt.Errorf("%s.targets: MCP server projection supports exactly one target, got [%s]", context, targetList(serverTargets))
		}
		serverTarget := serverTargets[0]

		scope, err := scopeWithDefault(raw.Scope, defaults.Scope(), context+".scope")
		if err != nil {
			return nil, err
		}
		if err := validateExplicitMCPGlobalScope(scope, raw.Scope, context); err != nil {
			return nil, err
		}

		transportKind, err := requiredExactString(raw.Transport, context+".transport")
		if err != nil {
			return nil, err
		}
		if transportKind != string(desiredmcp.TransportKindStdio) {
			return nil, fmt.Errorf("%s.transport: unsupported MCP transport %q", context, transportKind)
		}

		command, err := normalizeMCPCommand(raw.Command)
		if err != nil {
			return nil, fmt.Errorf("%s.command: %w", context, err)
		}
		env, err := normalizeMCPEnv(raw.Env, context+".env")
		if err != nil {
			return nil, err
		}
		transport, err := desiredmcp.NewStdioTransport(command, raw.Args, env)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", context, err)
		}
		binding, err := desiredmcp.NewBinding(serverTarget, scope, transport, desiredmcp.OnAbsentRemoveBinding)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", context, err)
		}

		serverIndex, exists := serverIndexes[name]
		bindings := []desiredmcp.Binding{binding}
		if exists {
			bindings = append(servers[serverIndex].Bindings(), binding)
		}
		server, err := desiredmcp.New(desiredmcp.Spec{Name: name, Bindings: bindings})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", context, err)
		}
		if exists {
			servers[serverIndex] = server
		} else {
			serverIndexes[name] = len(servers)
			servers = append(servers, server)
		}
	}

	return servers, nil
}

func normalizeMCPCommand(raw declaration.MCPCommand) (desiredmcp.Command, error) {
	switch raw.Kind() {
	case declaration.MCPCommandKindAmbient:
		return desiredmcp.NewAmbientCommand(raw.Value())
	case declaration.MCPCommandKindAbsolutePath:
		return desiredmcp.NewAbsolutePathCommand(raw.Value())
	default:
		return desiredmcp.Command{}, fmt.Errorf("command is required")
	}
}

func validateExplicitMCPGlobalScope(scope target.Scope, rawScope string, context string) error {
	if scope != target.ScopeGlobal {
		return nil
	}
	if strings.TrimSpace(rawScope) != "" {
		return nil
	}

	return fmt.Errorf("%s.scope: global MCP projection requires explicit scope = %q on the [[mcp_server]] block; defaults.scope = %q does not authorize global MCP config mutation", context, target.ScopeGlobal, target.ScopeGlobal)
}

func normalizeMCPEnv(raw map[string]declaration.MCPEnvReference, context string) (map[string]desiredmcp.EnvReference, error) {
	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	sort.Strings(names)

	env := make(map[string]desiredmcp.EnvReference, len(raw))
	for _, name := range names {
		reference, err := desiredmcp.NewEnvReference(raw[name].FromEnv)
		if err != nil {
			return nil, fmt.Errorf("%s.%s.from_env: %w", context, name, err)
		}
		env[name] = reference
	}
	return env, nil
}
