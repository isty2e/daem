package mcp

import (
	"context"
	"errors"

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

func collectProjectionAdmissions(
	ctx context.Context,
	servers []adopt.MCPServer,
	before func(int),
) ([]adopt.MCPServer, []adopt.Skipped, error) {
	collector := adopt.NewSkippedCollector()
	var admission *mcpProjectionAdmission
	err := collector.Collect(func(skipped adopt.SkipEmitter) error {
		if len(servers) == 0 {
			return nil
		}
		first := servers[0]
		admission = &mcpProjectionAdmission{
			ctx:    ctx,
			target: first.Target,
			scope:  first.Scope,
			source: newImportSource(
				first.SourceRoute.PrimaryPath,
				first.SourceRoute.RequiredAbsentPaths...,
			),
			document: importDocument{
				revision: first.SourceRoute.PrimaryRevision,
			},
			maximumBytes: first.SourceRoute.MaximumBytes,
			skipped:      skipped.WithRoute(first.Target, first.Scope),
			before:       before,
		}
		for _, server := range servers {
			if err := admission.admit(
				server.SourceRoute.ContentPath,
				server.ResourceName,
				server.Command,
				server.Args,
				func() map[string]string { return server.Env },
			); err != nil {
				var sinkError *mcpImportSinkError
				if errors.As(err, &sinkError) {
					return sinkError.cause
				}
				return err
			}
		}
		return nil
	})
	if admission == nil {
		return nil, collector.Skipped(), err
	}
	return admission.servers, collector.Skipped(), err
}
