package cli_test

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestHumanResourceOrderAcrossLockPlanAndApply(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "PI_CODING_AGENT_DIR"} {
		t.Setenv(key, filepath.Join(home, key))
	}
	testkit.WithWorkingDirectory(t, root)
	manifest := filepath.Join(root, "daem.toml")
	testkit.WriteFile(t, root, "daem.toml", `version = 1
targets = ["codex", "claude-code", "antigravity-cli"]

[[skill]]
name = "zeta"
source = { path = "skills/zeta", mode = "vendor" }
scope = "project"

[[skill]]
name = "alpha"
source = { path = "skills/alpha", mode = "vendor" }
scope = "project"
`)
	for _, name := range []string{"alpha", "zeta"} {
		testkit.WriteFile(t, root, "skills/"+name+"/SKILL.md", fmt.Sprintf("---\nname: %s\ndescription: fixture\n---\n%s\n", name, name))
	}
	locked := string(runCountUnitCLI(t, manifest, "lock"))
	assertHumanReferenceOrder(t, locked, []string{
		`resource="skill/alpha"` + "\n",
		`resource="skill/alpha" targets=antigravity-cli,codex scope=project`,
		`resource="skill/alpha" target=claude-code scope=project`,
		`resource="skill/zeta"` + "\n",
		`resource="skill/zeta" targets=antigravity-cli,codex scope=project`,
		`resource="skill/zeta" target=claude-code scope=project`,
	})
	jsonBefore := runCountUnitCLI(t, manifest, "status", "--json")
	want := []string{
		`resource="skill/alpha" targets=antigravity-cli,codex scope=project`,
		`resource="skill/alpha" target=claude-code scope=project`,
		`resource="skill/zeta" targets=antigravity-cli,codex scope=project`,
		`resource="skill/zeta" target=claude-code scope=project`,
	}
	for _, command := range [][]string{{"status"}, {"apply", "--dry-run"}} {
		assertHumanReferenceOrder(t, string(runCountUnitCLI(t, manifest, command...)), want)
	}
	if !bytes.Equal(jsonBefore, runCountUnitCLI(t, manifest, "status", "--json")) {
		t.Fatal("human status or dry-run changed canonical JSON or host state")
	}
	applied := string(runCountUnitCLI(t, manifest, "apply", "--yes"))
	_, results, found := strings.Cut(applied, "output results: 4 output entries\n")
	if !found || !strings.Contains(applied, "applied: 4 actions\n") {
		t.Fatalf("missing result boundary/counts:\n%s", applied)
	}
	assertHumanReferenceOrder(t, results, want)
	for _, name := range []string{"alpha", "zeta"} {
		for _, directory := range []string{".agents/skills", ".claude/skills"} {
			testkit.AssertFileContent(t, filepath.Join(root, directory, name, "SKILL.md"), fmt.Sprintf("---\nname: %s\ndescription: fixture\n---\n%s\n", name, name))
		}
	}
	testkit.AssertNoRecoveryArtifacts(t, root)
}

func TestHumanLockDisambiguatesSameNameMCPPlacements(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	testkit.WithWorkingDirectory(t, root)
	testkit.WriteFile(t, root, "daem.toml", `version = 1
targets = ["claude-code", "codex"]

[[mcp_server]]
name = "ctx"
command = "must-not-run-human-view-fixture"
transport = "stdio"
targets = ["claude-code"]
scope = "project"

[[mcp_server]]
name = "ctx"
command = "must-not-run-human-view-fixture"
transport = "stdio"
targets = ["codex"]
scope = "project"
`)
	manifest := filepath.Join(root, "daem.toml")
	output := string(runCountUnitCLI(t, manifest, "lock", "--dry-run"))
	assertHumanReferenceOrder(t, output, []string{
		`resource="mcp-server/ctx" target=codex scope=project placement=`,
		`resource="mcp-server/ctx" target=claude-code scope=project placement=`,
	})
	if strings.Count(output, `resource="mcp-server/ctx"`) != 2 {
		t.Fatalf("same-name placements were merged or lost:\n%s", output)
	}
}

func TestLocalExtensionReferenceMatchesPublicInventoryAndJSONIdentity(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "PI_CODING_AGENT_DIR"} {
		t.Setenv(key, filepath.Join(home, key))
	}
	testkit.WithWorkingDirectory(t, root)
	testkit.WriteFile(t, root, "pkgs/some-dir/package.json", `{"name":"private-fixture","version":"1.0.0"}`)
	testkit.WriteFile(t, root, "daem.toml", `version = 1
targets = ["pi"]

[[extension]]
id = "my-ext-id"
carrier = "pi-package"
scope = "project"
source = { host_source = "./pkgs/some-dir" }
`)
	manifest := filepath.Join(root, "daem.toml")
	structured := runCountUnitCLI(t, manifest, "lock", "--dry-run", "--json")
	payload := clijson.DecodeLock(t, structured)
	if len(payload.SubjectChanges) != 1 || payload.SubjectChanges[0].After == nil {
		t.Fatalf("wrong lock entry count: %#v", payload.SubjectChanges)
	}
	change := payload.SubjectChanges[0]
	if change.After.EntityID != "extension:my-ext-id" || !change.Subject.NameRedacted ||
		!change.After.Subject.NameRedacted || change.After.Realization == nil ||
		!change.After.Realization.SourceNamespaceRedacted || !change.After.Realization.RelationSubjectKeyRedacted {
		t.Fatalf("public declaration identity and private carrier facts were conflated: %#v", change)
	}
	if bytes.Contains(structured, []byte("./pkgs/some-dir")) {
		t.Fatalf("JSON disclosed private source: %s", structured)
	}
	for _, verbose := range []bool{false, true} {
		args := []string{"lock", "--dry-run"}
		if verbose {
			args = append(args, "--verbose")
		}
		output := string(runCountUnitCLI(t, manifest, args...))
		if !strings.Contains(output, `resource="extension/my-ext-id"`) ||
			!strings.Contains(output, "target=pi scope=project") {
			t.Fatalf("verbose=%t lost declaration identity: %s", verbose, output)
		}
		if !verbose && strings.Contains(output, "./pkgs/some-dir") {
			t.Fatalf("default human output disclosed private source: %s", output)
		}
	}
	resources := string(runCountUnitCLI(t, manifest, "list", "resources"))
	if !strings.Contains(resources, `key="my-ext-id"`) || strings.Contains(resources, "./pkgs/some-dir") {
		t.Fatalf("public inventory identity or source privacy changed: %s", resources)
	}
	if !bytes.Equal(structured, runCountUnitCLI(t, manifest, "lock", "--dry-run", "--json")) {
		t.Fatal("human rendering changed canonical lock JSON")
	}
}

func assertHumanReferenceOrder(t *testing.T, output string, rows []string) {
	t.Helper()
	previous := -1
	for _, row := range rows {
		position := strings.Index(output, row)
		if position <= previous {
			t.Fatalf("missing or out-of-order row %s:\n%s", row, output)
		}
		previous = position
	}
}
