package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/platformsupport"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
)

func TestExtensionSummaryPreservesPlansTransitionsAndRefusal(t *testing.T) {
	root := t.TempDir()
	testkit.SetDefaultRootEnv(t, root)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(root, "agent"))
	t.Setenv("PATH", "")
	manifest := filepath.Join(root, "daem.toml")
	settings := filepath.Join(root, ".pi", "settings.json")
	const initial = `{"packages":["npm:@acme/alpha@1.0.0","npm:@acme/beta@1.0.0"]}`
	testkit.WriteFile(t, root, ".pi/settings.json", initial)
	testkit.WriteFile(t, root, "daem.toml", piOrderManifest("npm:@acme/alpha@1.0.0", "npm:@acme/beta@1.0.0"))
	version, err := platformsupport.ParseMacOSProductVersion("26.0")
	if err != nil {
		t.Fatal(err)
	}
	platform, err := platformsupport.NewRuntimeObservation(version)
	if err != nil {
		t.Fatal(err)
	}
	var attempts atomic.Int32
	deny := func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
		attempts.Add(1)
		return subprocess.CommandResult{}
	}
	options := clipkg.RunOptions{
		PlatformObserver: func(context.Context) (platformsupport.RuntimeObservation, error) { return platform, nil },
		ApplyExecuteOptions: applyworkflow.ExecuteOptions{
			HostRouteExecutor: subprocess.NewCommandExecutor(subprocess.CommandOptions{Runner: deny}),
			DelegateExecutor:  delegate.NewExecutor(delegate.Options{Runner: deny}),
		},
	}
	run := func(want int, args ...string) (string, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		invocation := options
		invocation.Stdout, invocation.Stderr = &stdout, &stderr
		exit := testkit.RunCLIWithOptions(append(args, "--manifest", manifest), invocation)
		if exit != want || attempts.Load() != 0 {
			t.Fatalf("%v exit=%d want=%d stdout=%s stderr=%s attempts=%d", args, exit, want, stdout.String(), stderr.String(), attempts.Load())
		}
		return stdout.String(), stderr.String()
	}
	if _, stderr := run(0, "lock"); stderr != "" {
		t.Fatal(stderr)
	}
	adoption, stderr := run(0, "apply", "--manage-existing", "--yes")
	if stderr != "" || !strings.Contains(adoption, "carrier adoption results: 2 entries") ||
		strings.Count(adoption, "recorded external carrier claim") != 2 || !strings.Contains(adoption, "provenance=explicitly_adopted_observed") {
		t.Fatalf("state-only transitions were hidden: stdout=%s stderr=%s", adoption, stderr)
	}
	testkit.AssertFileContent(t, settings, initial)

	currentJSON, stderr := run(0, "status", "--json")
	if stderr != "" {
		t.Fatal(stderr)
	}
	var report struct {
		Actions   []json.RawMessage `json:"relation_actions"`
		Adoptions []json.RawMessage `json:"carrier_adoption_actions"`
		Absences  []json.RawMessage `json:"carrier_absence_actions"`
		Orders    []json.RawMessage `json:"relation_order_actions"`
	}
	if err := json.Unmarshal([]byte(currentJSON), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Actions) != 2 || len(report.Adoptions) != 2 || len(report.Absences) != 2 || len(report.Orders) != 1 {
		t.Fatalf("JSON lost normal facts: %s", currentJSON)
	}
	for _, args := range [][]string{{"status"}, {"apply", "--dry-run"}} {
		stdout, stderr := run(0, args...)
		if stderr != "" {
			t.Fatal(stderr)
		}
		for _, header := range []string{
			"relation actions: 2 entries (2 unchanged)",
			"carrier adoption actions: 2 entries (2 unchanged)",
			"carrier absence actions: 2 entries (2 unchanged)",
			"extension order: 1 sequence (1 unchanged)",
		} {
			if !strings.Contains(stdout, header+"\n") {
				t.Fatalf("missing normal-section summary %q: %s", header, stdout)
			}
		}
		for _, passive := range []string{"no change planned for extension relation", "external carrier already claimed", "retain managed carrier relation", "runtime extension precedence is exact"} {
			if strings.Contains(stdout, passive) {
				t.Fatalf("normal plan repeated %q: %s", passive, stdout)
			}
		}
	}
	verbose, stderr := run(0, "status", "--verbose")
	if stderr != "" {
		t.Fatal(stderr)
	}
	for _, fact := range []string{"kind=no_op", "result=already_claimed_current", "selected_action=retain", "kind=exact"} {
		if !strings.Contains(verbose, fact) {
			t.Fatalf("verbose lost %q: %s", fact, verbose)
		}
	}
	retry, stderr := run(0, "apply", "--yes")
	if stderr != "" || !strings.Contains(retry, "extension order results: 1 sequence (1 unchanged)\n") || strings.Contains(retry, "  - exact") {
		t.Fatalf("unchanged execution result was not summarized: stdout=%s stderr=%s", retry, stderr)
	}
	testkit.WriteFile(t, root, "skills/output/SKILL.md", "---\nname: output\ndescription: fixture\n---\n")
	_, stderr = run(0, "add", "skill", filepath.Join(root, "skills", "output"), "--target", "codex", "--scope", "project")
	if stderr != "" {
		t.Fatal(stderr)
	}
	testkit.WriteFile(t, root, ".pi/settings.json", `{"packages":["npm:@acme/beta@1.0.0","npm:@acme/alpha@1.0.0"]}`)
	preview, stderr := run(0, "apply", "--dry-run")
	if stderr != "" || !strings.Contains(preview, "1 output entry (current=0 pending=1 blocked=0)") {
		t.Fatalf("preview unit changed: stdout=%s stderr=%s", preview, stderr)
	}
	changed, stderr := run(0, "apply", "--yes")
	if stderr != "" || !strings.Contains(changed, "applied: 2 actions\n") ||
		!strings.Contains(changed, "output results: 1 output entry\n") ||
		!strings.Contains(changed, "  - converged ") || !strings.Contains(changed, "changed=true") {
		t.Fatalf("mixed results lost their independent units: stdout=%s stderr=%s", changed, stderr)
	}
	orderHeading := strings.Index(changed, "extension order results:")
	outputHeading := strings.Index(changed, "output results:")
	outputRow := strings.Index(changed, ` resource="skill/output"`)
	if orderHeading < 0 || outputHeading <= orderHeading || outputRow <= outputHeading {
		t.Fatalf("output rows look nested under extension order: %s", changed)
	}
	assertOrderedText(t, string(testkit.ReadFile(t, settings)), "npm:@acme/alpha@1.0.0", "npm:@acme/beta@1.0.0")

	_, stderr = run(0, "add", "extension", "gamma", "npm:@acme/gamma@1.0.0", "--target", "pi", "--scope", "project")
	if stderr != "" {
		t.Fatal(stderr)
	}
	testkit.WriteFile(t, root, ".pi/settings.json", `{"packages":["npm:@acme/alpha@1.0.0","npm:@acme/beta@1.0.0","npm:@acme/gamma@1.0.0"]}`)
	state := filepath.Join(root, ".daem", "state.json")
	paths := []string{manifest, filepath.Join(root, "daem.lock.toml"), settings, state}
	before := make(map[string]string)
	for _, name := range paths {
		before[name] = string(testkit.ReadFile(t, name))
	}
	stdout, refusal := run(1, "apply", "--yes")
	if stdout != "" || !strings.Contains(refusal, "relation actions: 3 entries (2 unchanged)") ||
		!strings.Contains(refusal, "external carrier present but unclaimed") || !strings.Contains(refusal, "/gamma") {
		t.Fatalf("preflight refusal lost its blocker: stdout=%s stderr=%s", stdout, refusal)
	}
	for _, name := range paths {
		if string(testkit.ReadFile(t, name)) != before[name] {
			t.Fatalf("refused apply changed %s", name)
		}
	}
}
