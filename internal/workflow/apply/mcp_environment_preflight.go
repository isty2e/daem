package apply

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/realization/aggregate"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

const maximumReportedMissingMCPEnvironmentSources = 8

type environmentSourcePresence func(string) bool

type missingMCPEnvironmentSourcesError struct {
	names   []string
	omitted int
}

func (err missingMCPEnvironmentSourcesError) Error() string {
	detail := strings.Join(err.names, ", ")
	if err.omitted == 0 {
		return "missing MCP environment sources: " + detail
	}
	return fmt.Sprintf(
		"missing MCP environment sources: %s (%d more omitted)",
		detail,
		err.omitted,
	)
}

func processEnvironmentSourcePresent(name string) bool {
	_, present := os.LookupEnv(name)
	return present
}

func preflightMCPEnvironmentSources(
	ctx context.Context,
	environment desired.Environment,
	selection targetselection.Selection,
	present environmentSourcePresence,
) error {
	if ctx == nil {
		return fmt.Errorf("MCP environment preflight context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if present == nil {
		present = processEnvironmentSourcePresent
	}
	names, err := selectedMCPEnvironmentSourceNames(environment, selection)
	if err != nil {
		return err
	}
	missing := make([]string, 0)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !present(name) {
			missing = append(missing, name)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(missing) == 0 {
		return nil
	}
	reported := min(len(missing), maximumReportedMissingMCPEnvironmentSources)
	return missingMCPEnvironmentSourcesError{
		names:   append([]string(nil), missing[:reported]...),
		omitted: len(missing) - reported,
	}
}

func selectedMCPEnvironmentSourceNames(
	environment desired.Environment,
	selection targetselection.Selection,
) ([]string, error) {
	seen := make(map[string]struct{})
	for _, server := range environment.MCPServers() {
		for _, binding := range server.Bindings() {
			if !selection.Includes(binding.Target()) {
				continue
			}
			stdio, ok := binding.Transport().Stdio()
			if !ok {
				return nil, fmt.Errorf(
					"MCP server %q binding %s/%s has unsupported transport %q",
					server.ID().Name(),
					binding.Target(),
					binding.Scope(),
					binding.Transport().Kind(),
				)
			}
			sourceNames := stdio.EnvironmentSourceNames()
			if len(sourceNames) == 0 {
				continue
			}
			placement, err := aggregate.MCPPlacementForBinding(binding)
			if err != nil {
				return nil, fmt.Errorf(
					"MCP server %q binding %s/%s environment references: %w",
					server.ID().Name(),
					binding.Target(),
					binding.Scope(),
					err,
				)
			}
			if placement.EnvReferenceContract().Resolution() != aggregate.MCPEnvResolutionHostRuntime {
				return nil, fmt.Errorf(
					"MCP placement %q cannot resolve admitted environment references at host runtime",
					placement.ID(),
				)
			}
			for _, name := range sourceNames {
				seen[name] = struct{}{}
			}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
