package cli_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestMCPPublicCLILockReportsSubjectProjection(t *testing.T) {
	project := newMCPCLIProject(t)
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Command: "must-not-run-daem-test",
		Args:    []string{"--serve", "context7"},
		Env:     map[string]string{"API_TOKEN": "CONTEXT7_API_TOKEN"},
	})

	exitCode, stdout, stderr := runMCPCLI(t, "lock", "--manifest", project.manifestPath, "--dry-run", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("lock dry-run json exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	var payload clijson.Lock
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("decode lock json: %v\nstdout=%s", err, stdout)
	}
	if payload.EntryCounts.Subjects != 1 {
		t.Fatalf("entry counts = %#v, want one subject only", payload.EntryCounts)
	}
	if len(payload.SubjectChanges) != 1 {
		t.Fatalf("subject_changes = %#v, want one", payload.SubjectChanges)
	}
	change := payload.SubjectChanges[0]
	if change.Status != "added" ||
		change.Subject.Kind != string(topology.SubjectProjection) ||
		change.Subject.Namespace != "claude-code.project.mcp-server" ||
		change.Subject.Name != "context7" {
		t.Fatalf("subject change = %#v, want added Claude MCP projection subject", change)
	}
	if change.After == nil || change.After.Realization == nil {
		t.Fatalf("subject change after = %#v, want managed aggregate realization details", change.After)
	}
	view := change.After.Realization
	if view.Kind != string(realization.RealizationManagedAggregateContribution) ||
		view.Target != string(target.TargetClaudeCode) ||
		view.Scope != string(target.ScopeProject) ||
		view.AggregateRoot != aggregate.ClaudeProjectMCPConfigPath ||
		view.ContentPath != mcpcodec.ClaudeProjectMCPContentPath("context7") ||
		view.AdapterContractVersion != aggregate.ClaudeProjectMCPStdioAdapterV1 {
		t.Fatalf("managed aggregate realization = %#v", view)
	}
	assertNoPublicMCPOutputLeaks(t, stdout)

	exitCode, stdout, stderr = runMCPCLI(t, "lock", "--manifest", project.manifestPath, "--dry-run")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("lock dry-run human exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	for _, want := range []string{
		"lockfile.subject.added:",
		"projection/claude-code.project.mcp-server/context7",
		`target="claude-code" scope="project" aggregate_root=".mcp.json" content_path="/mcpServers/context7"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout = %q, want %q", stdout, want)
		}
	}
	assertNoPublicMCPOutputLeaks(t, stdout)

	runMCPLock(t, project)
	locked, err := lockfile.Load(t.Context(), project.lockfilePath)
	if err != nil {
		t.Fatalf("load lockfile: %v", err)
	}
	assertLockedMCPSubject(t, locked, "context7")
}
