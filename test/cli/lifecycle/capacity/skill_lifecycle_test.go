package capacity_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	testscale "github.com/isty2e/daem/test/scale"
	"github.com/isty2e/daem/test/testkit"
)

func TestMain(m *testing.M) {
	os.Exit(testkit.RunWithIsolatedDefaultRoots(m))
}

func TestMultiTargetSkillBatchLifecycle(t *testing.T) {
	testscale.Require(t)
	const skillCount = 100
	root := t.TempDir()
	testkit.WithWorkingDirectory(t, root)

	var manifest strings.Builder
	manifest.WriteString("version = 1\ntargets = [\"codex\", \"claude-code\"]\n")
	for index := range skillCount {
		name := fmt.Sprintf("skill-%03d", index)
		fmt.Fprintf(&manifest, "\n[[skill]]\nname = %q\nsource = { path = %q, mode = \"vendor\" }\n", name, "skills/"+name)
		testkit.WriteFile(t, root, "skills/"+name+"/SKILL.md", batchSkillDocument(name, "Initial content."))
		for reference := range 7 {
			testkit.WriteFile(t, root, fmt.Sprintf("skills/%s/references/%d.md", name, reference), "Reference content.\n")
		}
	}
	testkit.WriteFile(t, root, "daem.toml", manifest.String())
	testkit.WriteFile(t, root, "unrelated.txt", "Keep this file.\n")
	runBatchCLI(t, "lock")
	runBatchCLI(t, "apply", "--dry-run")
	testkit.AssertPathMissing(t, filepath.Join(root, ".agents", "skills"))
	testkit.AssertPathMissing(t, filepath.Join(root, ".claude", "skills"))
	assertBatchApply(t, 2*skillCount)
	assertBatchSkillOutputs(t, root, skillCount, "Initial content.")
	runBatchCLI(t, "status", "--check")
	state := testkit.ReadFile(t, filepath.Join(root, ".daem", "state.json"))
	assertBatchApply(t, 0)
	if after := testkit.ReadFile(t, filepath.Join(root, ".daem", "state.json")); !bytes.Equal(after, state) {
		t.Fatal("no-op apply changed managed state")
	}

	testkit.WriteFile(t, root, "skills/skill-000/SKILL.md", batchSkillDocument("skill-000", "Updated content."))
	runBatchCLI(t, "lock")
	assertBatchApply(t, 2)
	assertBatchSkillOutputs(t, root, skillCount, "Updated content.")
	runBatchCLI(t, "status", "--check")

	testkit.WriteFile(t, root, "daem.toml", "version = 1\ntargets = [\"codex\", \"claude-code\"]\n")
	runBatchCLI(t, "lock")
	assertBatchApply(t, 2*skillCount)
	for _, host := range []string{".agents", ".claude"} {
		for index := range skillCount {
			testkit.AssertPathMissing(t, filepath.Join(root, host, "skills", fmt.Sprintf("skill-%03d", index)))
		}
	}
	runBatchCLI(t, "status", "--check")
	assertBatchApply(t, 0)
	if got := string(testkit.ReadFile(t, filepath.Join(root, "unrelated.txt"))); got != "Keep this file.\n" {
		t.Fatalf("unrelated file = %q", got)
	}
	for index := range skillCount {
		name := fmt.Sprintf("skill-%03d", index)
		body := "Initial content."
		if index == 0 {
			body = "Updated content."
		}
		if got := string(testkit.ReadFile(t, filepath.Join(root, "skills", name, "SKILL.md"))); got != batchSkillDocument(name, body) {
			t.Fatalf("source %s changed after removal", name)
		}
	}
}

func batchSkillDocument(name, body string) string {
	return fmt.Sprintf("---\nname: %s\ndescription: Batch lifecycle fixture.\n---\n\n%s\n", name, body)
}

func runBatchCLI(t *testing.T, args ...string) []byte {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := testkit.RunCLI(args, &stdout, &stderr); code != 0 {
		t.Fatalf("%v exit=%d\nstdout=%s\nstderr=%s", args, code, &stdout, &stderr)
	}
	return stdout.Bytes()
}

func assertBatchApply(t *testing.T, expectedActions int) {
	t.Helper()
	var result struct {
		ActionCount int  `json:"action_count"`
		HasErrors   bool `json:"has_errors"`
	}
	if err := json.Unmarshal(runBatchCLI(t, "apply", "--yes", "--json"), &result); err != nil {
		t.Fatal(err)
	}
	if result.HasErrors || result.ActionCount != expectedActions {
		t.Fatalf("apply result = %+v, want %d actions without errors", result, expectedActions)
	}
}

func assertBatchSkillOutputs(t *testing.T, root string, count int, firstBody string) {
	t.Helper()
	for _, host := range []string{".agents", ".claude"} {
		for index := range count {
			name := fmt.Sprintf("skill-%03d", index)
			body := "Initial content."
			if index == 0 {
				body = firstBody
			}
			if got := string(testkit.ReadFile(t, filepath.Join(root, host, "skills", name, "SKILL.md"))); got != batchSkillDocument(name, body) {
				t.Fatalf("%s/%s content differs", host, name)
			}
			for reference := range 7 {
				file := filepath.Join(root, host, "skills", name, "references", fmt.Sprintf("%d.md", reference))
				if got := string(testkit.ReadFile(t, file)); got != "Reference content.\n" {
					t.Fatalf("reference %s = %q", file, got)
				}
			}
		}
	}
}
