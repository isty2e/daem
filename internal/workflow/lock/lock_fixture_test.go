package lock

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func assertWorkflowMCPSubject(t *testing.T, file lock.File, serverID string) mcpcodec.ClaudeProjectMCPServerEntry {
	t.Helper()
	if len(file.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one MCP subject", file.Locked.Subjects())
	}
	record := file.Locked.Subjects()[0]
	subject := record.SubjectID()
	if subject.Kind() != topology.SubjectProjection ||
		subject.Namespace() != "claude-code.project.mcp-server" ||
		subject.Key() != serverID {
		t.Fatalf("subject = %#v, want Claude project MCP projection subject %q", subject, serverID)
	}
	realization, ok := record.Realization()
	if !ok {
		t.Fatal("MCP subject is missing realization")
	}
	claim, ok := realization.ManagedAggregateContribution()
	if !ok {
		t.Fatal("MCP subject realization is not a managed aggregate contribution")
	}
	if record.OnAbsent() != lock.OnAbsentRemoveBinding ||
		claim.AggregateRoot() != aggregate.ClaudeProjectMCPConfigPath ||
		claim.ContentPath() != mcpcodec.ClaudeProjectMCPContentPath(serverID) ||
		claim.Equivalence() != aggregate.EquivalenceCanonicalSemantic ||
		string(claim.CodecContractID()) != aggregate.ClaudeProjectMCPStdioAdapterV1 {
		t.Fatalf("claim = %#v, want Claude project MCP exact projection", claim)
	}
	var entry mcpcodec.ClaudeProjectMCPServerEntry
	if err := json.Unmarshal([]byte(claim.CanonicalContribution()), &entry); err != nil {
		t.Fatalf("canonical projection is not JSON: %v", err)
	}
	if entry.Type != "stdio" || entry.Command != "npx" {
		t.Fatalf("canonical projection = %#v, want stdio npx", entry)
	}
	return entry
}

func assertWorkflowClaudePluginCarrierSubject(
	t *testing.T,
	file lock.File,
	declarationID string,
	pluginKey string,
) {
	t.Helper()
	assertWorkflowClaudePluginCarrierSubjectWithScope(t, file, declarationID, pluginKey, target.ScopeProject)
}

func assertWorkflowClaudePluginCarrierSubjectWithScope(
	t *testing.T,
	file lock.File,
	declarationID string,
	pluginKey string,
	scope target.Scope,
) {
	t.Helper()
	route := mustWorkflowDelegatedOperationRoute(t, target.TargetClaudeCode, desiredextension.CarrierClaudeCodePlugin, profile.OperationInstall)
	if len(file.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one Claude plugin carrier subject", file.Locked.Subjects())
	}
	record := file.Locked.Subjects()[0]
	subject := record.SubjectID()
	if subject.Kind() != topology.SubjectHostRelation ||
		subject.Namespace() != "claude-code.plugin-carrier" ||
		subject.Key() != declarationID {
		t.Fatalf("subject = %#v, want Claude plugin carrier subject %q", subject, declarationID)
	}
	realization, hasRealization := record.Realization()
	relation, hasRelation := realization.DelegatedRelation()
	if record.OnAbsent() != lock.OnAbsentBlock ||
		!hasRealization || !hasRelation || relation.RouteContractVersion() != route.AdapterContractVersion() {
		t.Fatalf("record metadata = on_absent %q realization %#v", record.OnAbsent(), realization)
	}
	carrier, ok, err := lock.DelegatedRelationCarrier(record)
	if err != nil {
		t.Fatalf("DelegatedRelationCarrier returned error: %v", err)
	}
	if !ok {
		t.Fatal("DelegatedRelationCarrier returned ok=false")
	}
	source, err := desiredextension.ParseSourceRef(relation.SourceNamespace())
	if err != nil {
		t.Fatalf("ParseSourceRef returned error: %v", err)
	}
	if carrier != desiredextension.CarrierClaudeCodePlugin ||
		relation.Target() != target.TargetClaudeCode ||
		relation.Scope() != scope ||
		source.Kind() != desiredextension.SourceKindMarketplace ||
		source.Ref() != pluginKey ||
		string(relation.ExpectedRelation().SubjectKey()) != pluginKey {
		t.Fatalf("carrier = %#v, want Claude marketplace plugin carrier %q/%q scope %q", carrier, declarationID, pluginKey, scope)
	}
}

func assertWorkflowCodexPluginCarrierSubject(
	t *testing.T,
	file lock.File,
	declarationID string,
	pluginKey string,
) {
	t.Helper()
	route := mustWorkflowDelegatedOperationRoute(t, target.TargetCodex, desiredextension.CarrierCodexPlugin, profile.OperationInstall)
	if len(file.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one Codex plugin carrier subject", file.Locked.Subjects())
	}
	record := file.Locked.Subjects()[0]
	subject := record.SubjectID()
	if subject.Kind() != topology.SubjectHostRelation ||
		subject.Namespace() != "codex.plugin-carrier" ||
		subject.Key() != declarationID {
		t.Fatalf("subject = %#v, want Codex plugin carrier subject %q", subject, declarationID)
	}
	realization, hasRealization := record.Realization()
	relation, hasRelation := realization.DelegatedRelation()
	if record.OnAbsent() != lock.OnAbsentBlock ||
		!hasRealization || !hasRelation || relation.RouteContractVersion() != route.AdapterContractVersion() {
		t.Fatalf("record metadata = on_absent %q realization %#v", record.OnAbsent(), realization)
	}
	carrier, ok, err := lock.DelegatedRelationCarrier(record)
	if err != nil {
		t.Fatalf("DelegatedRelationCarrier returned error: %v", err)
	}
	if !ok {
		t.Fatal("DelegatedRelationCarrier returned ok=false")
	}
	source, err := desiredextension.ParseSourceRef(relation.SourceNamespace())
	if err != nil {
		t.Fatalf("ParseSourceRef returned error: %v", err)
	}
	if carrier != desiredextension.CarrierCodexPlugin ||
		relation.Target() != target.TargetCodex ||
		relation.Scope() != target.ScopeGlobal ||
		source.Kind() != desiredextension.SourceKindMarketplace ||
		source.Ref() != pluginKey ||
		string(relation.ExpectedRelation().SubjectKey()) != pluginKey {
		t.Fatalf("carrier = %#v, want Codex marketplace plugin carrier %q/%q", carrier, declarationID, pluginKey)
	}
}

func mustWorkflowDelegatedOperationRoute(
	t *testing.T,
	selectedTarget target.Target,
	carrier desiredextension.Carrier,
	operation profile.Operation,
) profile.OperationRoute {
	t.Helper()
	routeProfile, ok := profile.Profile(selectedTarget).DelegatedRoute(carrier)
	if !ok {
		t.Fatalf("target profile %q is missing delegated route %q", selectedTarget, carrier)
	}
	route, ok := routeProfile.OperationRoute(operation)
	if !ok {
		t.Fatalf("delegated route %q is missing operation %q", carrier, operation)
	}
	return route
}

func writeWorkflowTestFile(t *testing.T, root string, relativePath string, content string) {
	t.Helper()

	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func writeLockCommandParallelismFixture(t *testing.T, root string, name string) string {
	t.Helper()

	fixtureRoot := filepath.Join(root, name)
	manifestPath := filepath.Join(fixtureRoot, "daem.toml")
	writeWorkflowTestFile(t, fixtureRoot, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
	writeWorkflowTestFile(t, fixtureRoot, "instructions/project.md", "project instructions\n")
	writeWorkflowTestFile(t, fixtureRoot, "daem.toml", `version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]

[instructions.project]
source = "instructions/project.md"
targets = ["codex"]
`)

	return manifestPath
}
