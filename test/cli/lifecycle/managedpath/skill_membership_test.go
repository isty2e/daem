package cli_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestHashOnlySkillBaselineUsesAuthenticatedReleasePaths(t *testing.T) {
	targets := []string{"codex"}
	root, manifest, state, legacy := prepareManagedSkillProject(t, targets)
	const destination = ".agents/skills/oracle"
	installed := filepath.Join(root, destination)
	source := filepath.Join(root, "skills", "oracle")
	installedHash := testkit.InstalledHash(t, installed)
	testkit.WriteFile(t, installed, "runtime/local", "keep until replacement\n")
	for _, digest := range []string{legacy, installedHash} {
		baseline := testkit.SkillPathState(t, "oracle", targets, "project", destination, digest)
		testkit.WriteStatefile(t, state, testkit.Snapshot(t, baseline))
		before := string(testkit.ReadFile(t, state))
		assertManagedSkillCLI(t, 0, "status", "--check", "--manifest", manifest)
		assertManagedSkillCLI(t, 0, "apply", "--yes", "--manifest", manifest)
		testkit.AssertFileContent(t, state, before)
	}

	const updated = "---\nname: oracle\ndescription: updated\n---\n"
	testkit.WriteFile(t, source, "SKILL.md", updated)
	testkit.WriteFile(t, source, "new-file", "new\n")
	assertManagedSkillCLI(t, 0, "lock", "--manifest", manifest)
	assertManagedSkillCLI(t, 1, "apply", "--yes", "--json", "--manifest", manifest)
	testkit.AssertFileContent(t, filepath.Join(installed, "runtime/local"), "keep until replacement\n")

	if err := os.Remove(filepath.Join(source, "new-file")); err != nil {
		t.Fatal(err)
	}
	assertManagedSkillCLI(t, 0, "lock", "--manifest", manifest)
	assertManagedSkillCLI(t, 0, "apply", "--yes", "--manifest", manifest)
	testkit.AssertFileContent(t, filepath.Join(installed, "SKILL.md"), updated)
}

func TestCopiedSkillPayloadMembership(t *testing.T) {
	root, manifest, _, _ := prepareManagedSkillProject(t, []string{"codex"})
	installed := filepath.Join(root, ".agents", "skills", "oracle")
	source := filepath.Join(root, "skills", "oracle")
	testkit.WriteFile(t, installed, "notes.txt", "not shipped\n")
	testkit.WriteFile(t, installed, "runtime/cache/data", "cache\n")
	if err := os.Symlink("missing", filepath.Join(installed, "runtime", "link")); err != nil {
		t.Fatal(err)
	}
	assertManagedSkillCLI(t, 0, "status", "--check", "--manifest", manifest)
	assertManagedSkillCLI(t, 0, "apply", "--yes", "--manifest", manifest)
	if err := os.Rename(source, source+".away"); err != nil {
		t.Fatal(err)
	}
	if inventory := assertManagedSkillCLI(t, 0, "list", "outputs", "--manifest", manifest); !strings.Contains(inventory, ".agents/skills/oracle") {
		t.Fatalf("missing installed skill without its source: %s", inventory)
	}
	if err := os.Rename(source+".away", source); err != nil {
		t.Fatal(err)
	}

	// Updating another skill must not traverse or replace this skill's runtime tree.
	declaration := string(testkit.ReadFile(t, manifest)) + "\n[[skill]]\nname='other'\nsource={path='skills/other',mode='vendor'}\n"
	testkit.WriteFile(t, root, "daem.toml", declaration)
	testkit.WriteFile(t, root, "skills/other/SKILL.md", "---\nname: other\ndescription: second skill\n---\n")
	assertManagedSkillCLI(t, 0, "lock", "--manifest", manifest)
	assertManagedSkillCLI(t, 0, "apply", "--yes", "--manifest", manifest)
	testkit.AssertFileContent(t, filepath.Join(root, ".agents/skills/other/SKILL.md"), "---\nname: other\ndescription: second skill\n---\n")
	testkit.AssertFileContent(t, filepath.Join(installed, "notes.txt"), "not shipped\n")
	if err := os.Chmod(filepath.Join(installed, "notes.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	testkit.WriteFile(t, source, "notes.txt", "now shipped\n")
	assertManagedSkillCLI(t, 0, "lock", "--manifest", manifest)
	if _, _, code := runManagedSkillCLI("apply", "--yes", "--manifest", manifest); code == 0 {
		t.Fatal("replacement discarded an unsupported link")
	}
	if err := os.Remove(filepath.Join(installed, "runtime", "link")); err != nil {
		t.Fatal(err)
	}
	var interactive bytes.Buffer
	if code := cli.RunWithOptions([]string{"apply", "--manifest", manifest}, cli.RunOptions{
		Context: t.Context(), Stdin: strings.NewReader(""), Stdout: &interactive, Stderr: io.Discard,
		StdinIsTerminal: true, StdoutIsTerminal: true, StderrIsTerminal: true,
		ReadConfirmationLine: func(context.Context, io.Reader, int) (string, error) {
			if !strings.Contains(interactive.String(), "not shipped") {
				t.Fatalf("no disclosure before confirmation: %s", &interactive)
			}
			testkit.WriteFile(t, installed, "runtime/cache/data", "changed at confirmation\n")
			return "yes", nil
		},
	}); code == 0 {
		t.Fatal("replaced an installation whose extra file changed after planning")
	}
	testkit.AssertFileContent(t, filepath.Join(installed, "runtime/cache/data"), "changed at confirmation\n")
	testkit.AssertFileContent(t, filepath.Join(installed, "notes.txt"), "not shipped\n")

	preview, stderr, code := runManagedSkillCLI("apply", "--dry-run", "--json", "--manifest", manifest)
	plan := clijson.DecodePlan(t, []byte(preview))
	disclosures := map[string]bool{}
	for _, action := range plan.Actions {
		disclosures[action.Destination] = action.RemovesAdditionalEntries
	}
	if code != 0 || !disclosures[".agents/skills/oracle"] || disclosures[".agents/skills/other"] {
		t.Fatalf("missing replacement disclosure: %d %s %s", code, preview, stderr)
	}
	assertManagedSkillCLI(t, 0, "apply", "--yes", "--manifest", manifest)
	testkit.AssertFileContent(t, filepath.Join(installed, "notes.txt"), "now shipped\n")
	info, err := os.Stat(filepath.Join(installed, "notes.txt"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("new file inherited extra permissions: %v %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(installed, "runtime")); !os.IsNotExist(err) {
		t.Fatalf("extra directory survived: %v", err)
	}
	testkit.WriteFile(t, installed, "notes.txt", "different\n")
	if err := os.Remove(filepath.Join(source, "notes.txt")); err != nil {
		t.Fatal(err)
	}
	assertManagedSkillCLI(t, 0, "lock", "--manifest", manifest)
	if _, _, code := runManagedSkillCLI("apply", "--yes", "--manifest", manifest); code == 0 {
		t.Fatal("discarded a changed file dropped by the next release")
	}
	testkit.AssertFileContent(t, filepath.Join(installed, "notes.txt"), "different\n")
}

func TestNewlyShippedFileWithIdenticalLocalBytesGetsInstallationMode(t *testing.T) {
	root, manifest, _, _ := prepareManagedSkillProject(t, []string{"codex"})
	installed := filepath.Join(root, ".agents", "skills", "oracle")
	source := filepath.Join(root, "skills", "oracle")
	testkit.WriteFile(t, installed, "notes", "same bytes\n")
	extra := filepath.Join(installed, "notes")
	if err := os.Chmod(extra, 0o755); err != nil {
		t.Fatal(err)
	}
	testkit.WriteFile(t, source, "notes", "same bytes\n")
	assertManagedSkillCLI(t, 0, "lock", "--manifest", manifest)
	result := assertManagedSkillCLI(t, 0, "apply", "--yes", "--json", "--manifest", manifest)
	actions := clijson.DecodeApplyResult(t, []byte(result)).Actions
	if len(actions) != 1 || !actions[0].RemovesAdditionalEntries {
		t.Fatalf("replacement not disclosed: %+v", actions)
	}
	if info, err := os.Stat(extra); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("newly shipped file retained extra permissions: %v, %v", info, err)
	}
	testkit.WriteFile(t, installed, "notes", "changed\n")
	assertManagedSkillCLI(t, 1, "status", "--check", "--manifest", manifest)
}
