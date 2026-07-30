package cli_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestPiExtensionImportLockApplyAndRetryConvergesRuntimeOrder(t *testing.T) {
	tempDir := t.TempDir()
	testkit.SetDefaultRootEnv(t, tempDir)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	settingsPath := filepath.Join(tempDir, ".pi", "settings.json")
	alpha := "npm:@acme/alpha@1.0.0"
	beta := "npm:@acme/beta@1.0.0"
	foreign := "npm:@acme/foreign@1.0.0"
	testkit.WriteFile(
		t,
		tempDir,
		".pi/settings.json",
		`{"packages":["`+alpha+`","`+beta+`"]}`,
	)

	runExtensionOrderCLI(
		t,
		[]string{
			"import", "--target", "pi", "--scope", "project",
			"--manifest", manifestPath,
		},
	)
	manifest := string(testkit.ReadFile(t, manifestPath))
	if strings.Count(manifest, "[[extension]]") != 2 ||
		strings.Index(manifest, alpha) >= strings.Index(manifest, beta) {
		t.Fatalf("imported manifest does not preserve Pi package order:\n%s", manifest)
	}
	lockPayload := runExtensionOrderLock(t, manifestPath)
	if lockPayload.EntryCounts.OrderConstraints != 1 ||
		len(lockPayload.OrderConstraintChanges) != 1 ||
		len(lockPayload.OrderConstraintChanges[0].After.Members) != 2 {
		t.Fatalf("lock order constraints = %#v", lockPayload)
	}
	if !strings.HasPrefix(string(testkit.ReadFile(t, filepath.Join(tempDir, "daem.lock.toml"))), "version = 4\n") {
		t.Fatal("written lockfile is not schema version 4")
	}

	testkit.WriteFile(
		t,
		tempDir,
		".pi/settings.json",
		`{"packages":["`+beta+`","`+foreign+`","`+alpha+`"]}`,
	)
	status := runExtensionOrderPlan(t, "status", manifestPath)
	assertOrderPlan(t, status, "pi", "runtime-precedence", 1)
	dryRun := runExtensionOrderPlan(t, "apply", manifestPath, "--manage-existing")
	if dryRun.HasErrors {
		t.Fatalf("manage-existing dry-run has errors: %#v", dryRun)
	}
	assertOrderPlan(t, dryRun, "pi", "runtime-precedence", 1)
	testkit.AssertFileContent(
		t,
		settingsPath,
		`{"packages":["`+beta+`","`+foreign+`","`+alpha+`"]}`,
	)

	applied := runExtensionOrderApply(t, manifestPath, "--manage-existing")
	assertOrderResults(t, applied, "pi", 1, "converged", true)
	assertOrderedText(t, string(testkit.ReadFile(t, settingsPath)), alpha, foreign, beta)

	retry := runExtensionOrderApply(t, manifestPath)
	assertOrderResults(t, retry, "pi", 1, "exact", false)
	assertOrderedText(t, string(testkit.ReadFile(t, settingsPath)), alpha, foreign, beta)
}

func TestOpenCodeExtensionImportLockApplyAndRetryConvergesBothConfigOrders(t *testing.T) {
	tempDir := t.TempDir()
	testkit.SetDefaultRootEnv(t, tempDir)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	configPath := filepath.Join(tempDir, ".opencode", "opencode.json")
	tuiPath := filepath.Join(tempDir, ".opencode", "tui.jsonc")
	alpha := "@acme/alpha@1.0.0"
	beta := "@acme/beta@1.0.0"
	foreign := "@acme/foreign@1.0.0"
	testkit.WriteFile(
		t,
		tempDir,
		".opencode/opencode.json",
		`{"plugin":["`+alpha+`","`+beta+`"],"theme":"night"}`,
	)
	testkit.WriteFile(
		t,
		tempDir,
		".opencode/tui.jsonc",
		`{"plugin":[["`+alpha+`",{"tag":"alpha"}],["`+beta+`",{"tag":"beta"}]],"scroll_speed":3}`,
	)

	runExtensionOrderCLI(
		t,
		[]string{
			"import", "--target", "opencode", "--scope", "project",
			"--manifest", manifestPath,
		},
	)
	lockPayload := runExtensionOrderLock(t, manifestPath)
	if lockPayload.EntryCounts.OrderConstraints != 1 ||
		len(lockPayload.OrderConstraintChanges) != 1 ||
		len(lockPayload.OrderConstraintChanges[0].After.Members) != 2 {
		t.Fatalf("lock order constraints = %#v", lockPayload)
	}

	testkit.WriteFile(
		t,
		tempDir,
		".opencode/opencode.json",
		`{"plugin":["`+beta+`","`+foreign+`","`+alpha+`"],"theme":"night"}`,
	)
	testkit.WriteFile(
		t,
		tempDir,
		".opencode/tui.jsonc",
		`{"plugin":[["`+beta+`",{"tag":"beta"}],"`+foreign+`",["`+alpha+`",{"tag":"alpha"}]],"scroll_speed":3}`,
	)
	dryRun := runExtensionOrderPlan(t, "apply", manifestPath, "--manage-existing")
	if dryRun.HasErrors {
		t.Fatalf("manage-existing dry-run has errors: %#v", dryRun)
	}
	assertOrderPlan(t, dryRun, "opencode", "config-order-only", 2)

	applied := runExtensionOrderApply(t, manifestPath, "--manage-existing")
	assertOrderResults(t, applied, "opencode", 2, "converged", true)
	config := string(testkit.ReadFile(t, configPath))
	tui := string(testkit.ReadFile(t, tuiPath))
	assertOrderedText(t, config, alpha, foreign, beta)
	assertOrderedText(t, tui, alpha, foreign, beta)
	for _, retained := range []string{`"theme":"night"`, `"scroll_speed":3`, `"tag":"alpha"`, `"tag":"beta"`} {
		if !strings.Contains(config+"\n"+tui, retained) {
			t.Fatalf("OpenCode sibling %q was not retained:\nconfig=%s\ntui=%s", retained, config, tui)
		}
	}

	retry := runExtensionOrderApply(t, manifestPath)
	assertOrderResults(t, retry, "opencode", 2, "exact", false)
	if string(testkit.ReadFile(t, configPath)) != config ||
		string(testkit.ReadFile(t, tuiPath)) != tui {
		t.Fatal("exact retry rewrote an OpenCode config document")
	}
}

func runExtensionOrderLock(t *testing.T, manifestPath string) clijson.Lock {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{"lock", "--manifest", manifestPath, "--json"},
		&stdout,
		&stderr,
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("lock exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	return clijson.DecodeLock(t, stdout.Bytes())
}

func runExtensionOrderPlan(
	t *testing.T,
	command string,
	manifestPath string,
	extra ...string,
) clijson.Plan {
	t.Helper()
	args := []string{command, "--manifest", manifestPath, "--json"}
	if command == "apply" {
		args = append(args, "--dry-run")
	}
	args = append(args, extra...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("%s exitCode=%d stdout=%q stderr=%q", command, exitCode, stdout.String(), stderr.String())
	}
	return clijson.DecodePlan(t, stdout.Bytes())
}

func runExtensionOrderApply(
	t *testing.T,
	manifestPath string,
	extra ...string,
) clijson.ApplyResult {
	t.Helper()
	args := []string{"apply", "--manifest", manifestPath, "--yes", "--json"}
	args = append(args, extra...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	payload := clijson.DecodeApplyResult(t, stdout.Bytes())
	if payload.SchemaVersion != 15 || payload.HasErrors || len(payload.Errors) != 0 {
		t.Fatalf("apply payload = %#v", payload)
	}
	return payload
}

func runExtensionOrderCLI(t *testing.T, args []string) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("%s exitCode=%d stdout=%q stderr=%q", args[0], exitCode, stdout.String(), stderr.String())
	}
}

func assertOrderPlan(
	t *testing.T,
	plan clijson.Plan,
	selectedTarget string,
	runtimeMeaning string,
	wantSequences int,
) {
	t.Helper()
	if len(plan.RelationOrders) != wantSequences {
		t.Fatalf("relation_order_actions = %#v", plan.RelationOrders)
	}
	for _, action := range plan.RelationOrders {
		if action.Target != selectedTarget ||
			action.RuntimeMeaning != runtimeMeaning ||
			action.Kind != "normalize" ||
			!action.RequiresMutation ||
			action.ForeignRowCount != 1 ||
			len(action.Risks) == 0 {
			t.Fatalf("relation order action = %#v", action)
		}
	}
}

func assertOrderResults(
	t *testing.T,
	payload clijson.ApplyResult,
	selectedTarget string,
	wantSequences int,
	wantOutcome string,
	wantChanged bool,
) {
	t.Helper()
	if len(payload.RelationOrderResults) != wantSequences {
		t.Fatalf("relation_order_results = %#v, want %d", payload.RelationOrderResults, wantSequences)
	}
	for _, result := range payload.RelationOrderResults {
		if result.Target != selectedTarget ||
			result.Outcome != wantOutcome ||
			result.Changed != wantChanged ||
			result.Detail != "" {
			t.Fatalf("relation order result = %#v", result)
		}
	}
}

func assertOrderedText(t *testing.T, content string, values ...string) {
	t.Helper()
	previous := -1
	for _, value := range values {
		index := strings.Index(content, value)
		if index <= previous {
			t.Fatalf("value %q is not after prior values in %s", value, content)
		}
		previous = index
	}
}
