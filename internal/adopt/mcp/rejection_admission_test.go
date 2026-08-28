package mcp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestCandidatesStopsAtFirstCodecRejectionOverflow(t *testing.T) {
	const maximumOperationSkips = 4096

	root := t.TempDir()
	withWorkingDirectory(t, root)
	if err := os.WriteFile(filepath.Join(root, aggregate.ClaudeProjectMCPConfigPath), []byte(`{
  "mcpServers": {
    "c": {"type": "http", "command": "node"},
    "a": {"type": "http", "command": "node"},
    "b": {"type": "http", "command": "node"}
  }
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	collector := adopt.NewSkippedCollector()
	if err := collector.Collect(func(skipped adopt.SkipEmitter) error {
		route := skipped.WithRoute(target.TargetClaudeCode, target.ScopeProject)
		for index := 0; index < maximumOperationSkips-1; index++ {
			if err := route.Add(adopt.Skipped{
				LivePath: fmt.Sprintf("prefill/%d", index),
				Reason:   "source_not_importable",
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	var servers []adopt.MCPServer
	var authorities []adopt.MCPSourceAuthority
	err := collector.Collect(func(skipped adopt.SkipEmitter) error {
		var candidateErr error
		servers, authorities, candidateErr = Candidates(
			t.Context(),
			target.TargetClaudeCode,
			target.ScopeProject,
			skipped.WithRoute(target.TargetClaudeCode, target.ScopeProject),
		)
		return candidateErr
	})
	if !errors.Is(err, adopt.ErrSkipObservationLimitExceeded) {
		t.Fatalf("Candidates error = %v, want operation skip limit", err)
	}
	if servers != nil || authorities != nil || len(collector.Skipped()) != maximumOperationSkips {
		t.Fatalf(
			"result = servers=%#v authorities=%#v skipped=%d, want no partial candidates and %d retained rows",
			servers,
			authorities,
			len(collector.Skipped()),
			maximumOperationSkips,
		)
	}
	last := collector.Skipped()[maximumOperationSkips-1]
	if last.LivePath != aggregate.ClaudeProjectMCPConfigPath+"#/mcpServers/a" {
		t.Fatalf("last retained skip = %#v, want first sorted codec rejection", last)
	}
}
