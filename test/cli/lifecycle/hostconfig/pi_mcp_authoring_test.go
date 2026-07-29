package cli_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunAddPiMCPServerDryRunJSONShowsPairedDeclarationsAndTrust(t *testing.T) {
	project := newMCPCLIProject(t)
	original := "version = 1\ntargets = [\"pi\"]\n"
	testkit.WriteFile(t, project.root, "daem.toml", original)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "mcp-server", "context7", "node",
		"--manifest", project.manifestPath,
		"--target", "pi",
		"--scope", "project",
		"--arg", "server.js",
		"--dry-run",
		"--json",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}

	var payload struct {
		Lockfile *struct {
			Path   string
			Status string
		}
		Changes []struct {
			ChangeKind    string `json:"change_kind"`
			ManifestBlock string `json:"manifest_block"`
		}
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v; stdout = %q", err, stdout.String())
	}
	if payload.Lockfile == nil ||
		payload.Lockfile.Path != project.lockfilePath ||
		payload.Lockfile.Status != "would_write" {
		t.Fatalf("Lockfile = %#v, want paired prospective lock", payload.Lockfile)
	}
	if len(payload.Changes) != 1 {
		t.Fatalf("Changes = %#v, want one atomic authoring change", payload.Changes)
	}
	change := payload.Changes[0]
	for _, want := range []string{
		"append extension and mcp_server resources",
		"[[extension]]",
		`id = "pi-mcp-adapter-project"`,
		`source = { host_source = "npm:pi-mcp-adapter@^2.13.0" }`,
		"[[mcp_server]]",
		`name = "context7"`,
		`scope = "project"`,
	} {
		if !strings.Contains(change.ChangeKind+"\n"+change.ManifestBlock, want) {
			t.Fatalf("change = %#v, want %q", change, want)
		}
	}
	if len(payload.Warnings) != 1 ||
		!strings.Contains(payload.Warnings[0], "until the project is trusted") {
		t.Fatalf("Warnings = %#v, want project trust warning", payload.Warnings)
	}
	testkit.AssertFileContent(t, project.manifestPath, original)
	testkit.AssertPathMissing(t, project.lockfilePath)
	testkit.AssertPathMissing(t, filepath.Join(project.root, aggregate.PiProjectMCPConfigPath))
}

func TestRunAddPiMCPServerWritesPairedManifestAndLockOnly(t *testing.T) {
	project := newMCPCLIProject(t)
	testkit.WriteFile(t, project.root, "daem.toml", "version = 1\ntargets = [\"pi\"]\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "mcp-server", "context7", "node",
		"--manifest", project.manifestPath,
		"--target", "pi",
		"--scope", "project",
		"--arg", "server.js",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"added: mcp_server/context7",
		"change: append extension and mcp_server resources",
		"lockfile: wrote " + project.lockfilePath,
		"until the project is trusted",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}

	normalized, err := declarationmanifest.Decode(testkit.ReadFile(t, project.manifestPath))
	if err != nil {
		t.Fatalf("declarationmanifest.Decode returned error: %v", err)
	}
	if extensions := normalized.Extensions(); len(extensions) != 1 ||
		extensions[0].ID().Name() != "pi-mcp-adapter-project" ||
		extensions[0].Source().Ref() != "npm:pi-mcp-adapter@^2.13.0" {
		t.Fatalf("Extensions = %#v, want one bounded Pi provider", extensions)
	}
	if servers := normalized.MCPServers(); len(servers) != 1 {
		t.Fatalf("MCPServers = %#v, want context7", servers)
	} else {
		testkit.AssertSingleMCPStdioBinding(
			t,
			servers[0],
			"context7",
			target.TargetPi,
			target.ScopeProject,
			"node",
			[]string{"server.js"},
		)
	}
	locked, err := lockfile.Load(project.lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	assertLockedPiProviderAndMCP(t, locked.Locked.Subjects())
	testkit.AssertPathMissing(t, filepath.Join(project.root, aggregate.PiProjectMCPConfigPath))
}

func TestRunAddPiGlobalMCPServerWritesBoundedPairedManifestAndLock(t *testing.T) {
	project := newMCPCLIProject(t)
	testkit.WriteFile(t, project.root, "daem.toml", "version = 1\ntargets = [\"pi\"]\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "mcp-server", "context7", "node",
		"--manifest", project.manifestPath,
		"--target", "pi",
		"--scope", "global",
		"--arg", "server.js",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	manifest := string(testkit.ReadFile(t, project.manifestPath))
	for _, want := range []string{
		`id = "pi-mcp-adapter-global"`,
		`source = { host_source = "npm:pi-mcp-adapter@^2.13.0" }`,
		`scope = "global"`,
		"shared across projects",
		"may read project MCP config before project trust",
	} {
		if !strings.Contains(stdout.String()+"\n"+manifest, want) {
			t.Fatalf("output/manifest = %q, want %q", stdout.String()+"\n"+manifest, want)
		}
	}
	locked, err := lockfile.Load(project.lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if !lockedEntitySubjectPresent(
		locked.Locked.Subjects(),
		"pi-mcp-adapter-global",
		topology.SubjectHostRelation,
	) {
		t.Fatalf("locked subjects = %#v, want global provider host relation", locked.Locked.Subjects())
	}
	foundGlobalBinding := false
	for _, subject := range locked.Locked.Subjects() {
		if subject.EntityID().Name() != "context7" {
			continue
		}
		if _, present := subject.MCPProviderContribution(); present {
			foundGlobalBinding = true
		}
	}
	if !foundGlobalBinding {
		t.Fatalf("locked subjects = %#v, want global MCP provider binding", locked.Locked.Subjects())
	}
}

func TestRunAddPiMCPServerReusesExplicitGlobalProviderWithWarning(t *testing.T) {
	project := newMCPCLIProject(t)
	original := `version = 1
targets = ["pi"]

[[extension]]
id = "shared-adapter"
carrier = "pi-package"
targets = ["pi"]
scope = "global"
source = { host_source = "npm:pi-mcp-adapter@2.15.0" }
`
	testkit.WriteFile(t, project.root, "daem.toml", original)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "mcp-server", "context7", "node",
		"--manifest", project.manifestPath,
		"--target", "pi",
		"--scope", "project",
		"--arg", "server.js",
		"--dry-run",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if strings.Count(stdout.String(), "[[extension]]") != 0 ||
		!strings.Contains(stdout.String(), "reuses the global pi-mcp-adapter package") ||
		!strings.Contains(stdout.String(), "shared across projects") {
		t.Fatalf("stdout = %q, want visible global reuse without duplicate provider", stdout.String())
	}
	testkit.AssertFileContent(t, project.manifestPath, original)
	testkit.AssertPathMissing(t, project.lockfilePath)
}

func TestRunRemovePiMCPServerRetainsExplicitProvider(t *testing.T) {
	project := newMCPCLIProject(t)
	testkit.WriteFile(t, project.root, "daem.toml", piMCPPairedManifest()+`
[[mcp_server]]
name = "context7"
targets = ["pi"]
scope = "project"
transport = "stdio"
command = "node"
args = ["server.js"]
`)
	runMCPLock(t, project)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"remove", "mcp-server", "context7",
		"--manifest", project.manifestPath,
		"--target", "pi",
		"--scope", "project",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	manifest := string(testkit.ReadFile(t, project.manifestPath))
	if strings.Contains(manifest, "[[mcp_server]]") ||
		!strings.Contains(manifest, "[[extension]]") ||
		!strings.Contains(manifest, `id = "pi-mcp-adapter-project"`) {
		t.Fatalf("manifest = %q, want provider retained and MCP binding removed", manifest)
	}
	locked, err := lockfile.Load(project.lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	for _, subject := range locked.Locked.Subjects() {
		if subject.EntityID().Name() == "context7" ||
			subject.MCPEnvironmentSources() != nil {
			t.Fatalf("locked subject = %#v, want no MCP binding after remove", subject)
		}
	}
	if !lockedEntitySubjectPresent(
		locked.Locked.Subjects(),
		"pi-mcp-adapter-project",
		topology.SubjectHostRelation,
	) {
		t.Fatalf("locked subjects = %#v, want retained provider host relation", locked.Locked.Subjects())
	}
}

func TestRunAddPiMCPServerLeavesPairedFilesUnchangedWhenProspectiveLockFails(t *testing.T) {
	project := newMCPCLIProject(t)
	original := `version = 1
targets = ["pi"]

[[skill]]
name = "missing-skill"
source = { path = "skills/missing-skill", mode = "vendor" }
targets = ["pi"]
`
	testkit.WriteFile(t, project.root, "daem.toml", original)
	testkit.WriteFile(t, project.root, "daem.lock.toml", "lock stays\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "mcp-server", "context7", "node",
		"--manifest", project.manifestPath,
		"--target", "pi",
		"--scope", "project",
		"--arg", "server.js",
	}, &stdout, &stderr)
	if exitCode == 0 ||
		!strings.Contains(stderr.String(), "add failed: lock prospective manifest") {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q, want prospective lock failure", exitCode, stderr.String(), stdout.String())
	}
	testkit.AssertFileContent(t, project.manifestPath, original)
	testkit.AssertFileContent(t, project.lockfilePath, "lock stays\n")
	testkit.AssertPathMissing(t, testkit.AuthoringTransactionDir(filepath.Join(project.root, ".daem")))
}

func assertLockedPiProviderAndMCP(
	t *testing.T,
	subjects []lock.LockedSubjectContract,
) {
	t.Helper()
	if !lockedEntitySubjectPresent(
		subjects,
		"pi-mcp-adapter-project",
		topology.SubjectHostRelation,
	) {
		t.Fatalf("locked subjects = %#v, want provider host relation", subjects)
	}
	foundBinding := false
	for _, subject := range subjects {
		if subject.EntityID().Name() != "context7" {
			continue
		}
		if _, present := subject.MCPProviderContribution(); present {
			foundBinding = true
			break
		}
	}
	if !foundBinding {
		t.Fatalf("locked subjects = %#v, want context7 bound to explicit provider", subjects)
	}
}

func lockedEntitySubjectPresent(
	subjects []lock.LockedSubjectContract,
	entityName string,
	kind topology.SubjectKind,
) bool {
	for _, subject := range subjects {
		if subject.EntityID().Name() == entityName &&
			subject.SubjectID().Kind() == kind {
			return true
		}
	}
	return false
}

func piMCPPairedManifest() string {
	return `version = 1
targets = ["pi"]

[[extension]]
id = "pi-mcp-adapter-project"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "npm:pi-mcp-adapter@^2.13.0" }
`
}
