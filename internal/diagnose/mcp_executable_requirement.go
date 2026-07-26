package diagnose

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/findings"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

// MCPExecutableRequirementChecks reports ambient executable prerequisites for selected MCP servers.
func MCPExecutableRequirementChecks(servers []desiredmcp.Server, selection targetselection.Selection) []findings.Check {
	return mcpExecutableRequirementChecks(servers, selection, mcpExecutableEnvironment{
		lookPath:  exec.LookPath,
		lookupEnv: os.LookupEnv,
	})
}

type mcpExecutableEnvironment struct {
	lookPath  func(string) (string, error)
	lookupEnv func(string) (string, bool)
}

func mcpExecutableRequirementChecks(
	servers []desiredmcp.Server,
	selection targetselection.Selection,
	environment mcpExecutableEnvironment,
) []findings.Check {
	checks := make([]findings.Check, 0, len(servers)*2)
	for _, server := range servers {
		for _, binding := range server.Bindings() {
			if !selection.Includes(binding.Target()) {
				continue
			}

			facts, err := collectMCPExecutableRequirementFacts(server, binding)
			if err != nil {
				checks = append(checks, mcpExecutableProjectionCheck(server, binding, err))
				continue
			}
			checks = append(checks, mcpExecutableCommandCheck(server, binding, facts.command, environment.lookPath))
			checks = append(checks, mcpExecutableEnvCheck(server, binding, facts.envRefs, environment.lookupEnv))
		}
	}

	return checks
}

type mcpExecutableRequirementFacts struct {
	command string
	envRefs []string
}

func mcpExecutableRequirementFactsForGraph(graph topology.Graph) (mcpExecutableRequirementFacts, error) {
	var facts mcpExecutableRequirementFacts
	var projection topology.SubjectID
	projectionCount := 0
	for _, subject := range graph.Subjects() {
		if subject.Kind() == topology.SubjectProjection {
			projection = subject
			projectionCount++
		}
	}
	if projectionCount != 1 {
		return facts, fmt.Errorf("MCP executable requirements need exactly one structural projection, got %d", projectionCount)
	}

	launchers := graph.LauncherDependenciesOf(projection)
	if len(launchers) != 1 {
		return facts, fmt.Errorf("MCP structural projection requires exactly one ambient executable launcher")
	}
	for _, dependency := range launchers {
		command, ok := topologymcp.ExecutableCommand(dependency)
		if !ok {
			return facts, fmt.Errorf("MCP structural projection has unsupported launcher dependency %q", dependency)
		}
		facts.command = command
	}

	for _, dependency := range graph.DependenciesOf(projection) {
		if name, ok := topologymcp.EnvironmentReferenceName(dependency); ok {
			facts.envRefs = append(facts.envRefs, name)
			continue
		}
		return facts, fmt.Errorf("MCP structural projection has unsupported dependency %q", dependency)
	}

	sort.Strings(facts.envRefs)
	facts.envRefs = dedupeSortedStrings(facts.envRefs)
	return facts, nil
}

func collectMCPExecutableRequirementFacts(server desiredmcp.Server, binding desiredmcp.Binding) (mcpExecutableRequirementFacts, error) {
	graph, err := topologymcp.Binding(server, binding)
	if err != nil {
		return mcpExecutableRequirementFacts{}, err
	}
	return mcpExecutableRequirementFactsForGraph(graph)
}

func mcpExecutableProjectionCheck(server desiredmcp.Server, binding desiredmcp.Binding, err error) findings.Check {
	return errorCheck(
		mcpExecutableRequirementCheckName(server, binding, "projection"),
		fmt.Sprintf("MCP executable prerequisites cannot be evaluated because the MCP declaration does not lower to an admitted structural projection: %v", err),
	)
}

func mcpExecutableCommandCheck(
	server desiredmcp.Server,
	binding desiredmcp.Binding,
	command string,
	lookPath func(string) (string, error),
) findings.Check {
	if _, err := lookPath(command); err != nil {
		return warnCheck(
			mcpExecutableRequirementCheckName(server, binding, "command"),
			fmt.Sprintf("MCP executable prerequisite command %q is not discoverable on PATH; explicit MCP execution may fail until this command is available", command),
		)
	}

	return okCheck(
		mcpExecutableRequirementCheckName(server, binding, "command"),
		fmt.Sprintf("MCP executable prerequisite command %q is discoverable on PATH", command),
	)
}

func mcpExecutableEnvCheck(
	server desiredmcp.Server,
	binding desiredmcp.Binding,
	names []string,
	lookupEnv func(string) (string, bool),
) findings.Check {
	if len(names) == 0 {
		return okCheck(
			mcpExecutableRequirementCheckName(server, binding, "env_refs"),
			"MCP executable prerequisite requires no environment references",
		)
	}

	missing := make([]string, 0)
	for _, name := range names {
		if _, ok := lookupEnv(name); !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return warnCheck(
			mcpExecutableRequirementCheckName(server, binding, "env_refs"),
			fmt.Sprintf("MCP executable prerequisite missing environment references by name: %s; explicit MCP execution may fail until they are present", strings.Join(missing, ", ")),
		)
	}

	return okCheck(
		mcpExecutableRequirementCheckName(server, binding, "env_refs"),
		fmt.Sprintf("MCP executable prerequisite environment references are present by name: %s", strings.Join(names, ", ")),
	)
}

func mcpExecutableRequirementCheckName(server desiredmcp.Server, binding desiredmcp.Binding, dimension string) string {
	return fmt.Sprintf("target=%s scope=%s mcp_server=%s executable_requirement=%s", binding.Target(), binding.Scope(), server.ID().Name(), dimension)
}

func dedupeSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	deduped := values[:0]
	for _, value := range values {
		if len(deduped) == 0 || deduped[len(deduped)-1] != value {
			deduped = append(deduped, value)
		}
	}
	return deduped
}
