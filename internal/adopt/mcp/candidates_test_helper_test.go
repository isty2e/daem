package mcp

import (
	"context"

	"github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/target"
)

func collectCandidates(
	ctx context.Context,
	selected target.Target,
	scope target.Scope,
) ([]adopt.MCPServer, []adopt.MCPSourceAuthority, []adopt.Skipped, error) {
	return collectCandidatesWithHooks(ctx, selected, scope, candidateHooks{})
}

func collectCandidatesWithHooks(
	ctx context.Context,
	selected target.Target,
	scope target.Scope,
	hooks candidateHooks,
) ([]adopt.MCPServer, []adopt.MCPSourceAuthority, []adopt.Skipped, error) {
	collector := adopt.NewSkippedCollector()
	var servers []adopt.MCPServer
	var authorities []adopt.MCPSourceAuthority
	err := collector.Collect(func(skipped adopt.SkipEmitter) error {
		var err error
		servers, authorities, err = candidatesWithHooks(
			ctx,
			selected,
			scope,
			skipped.WithRoute(selected, scope),
			hooks,
		)
		return err
	})
	return servers, authorities, collector.Skipped(), err
}

func collectAdmittedArguments(
	ctx context.Context,
	servers []adopt.MCPServer,
	before func(int),
) ([]adopt.MCPServer, []adopt.Skipped, error) {
	collector := adopt.NewSkippedCollector()
	var admitted []adopt.MCPServer
	err := collector.Collect(func(skipped adopt.SkipEmitter) error {
		var err error
		route := skipped
		if len(servers) != 0 {
			route = skipped.WithRoute(servers[0].Target, servers[0].Scope)
		}
		admitted, err = admitMCPArgumentCandidates(ctx, servers, route, before)
		return err
	})
	return admitted, collector.Skipped(), err
}
