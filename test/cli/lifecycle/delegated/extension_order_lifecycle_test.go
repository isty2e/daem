package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/realization/lockfile"
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
	if !strings.HasPrefix(
		string(testkit.ReadFile(t, filepath.Join(tempDir, "daem.lock.toml"))),
		fmt.Sprintf("version = %d\n", contractversion.LockfileSchema),
	) {
		t.Fatalf("written lockfile is not schema version %d", contractversion.LockfileSchema)
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

	testkit.WriteFile(
		t,
		tempDir,
		".pi/settings.json",
		`{"packages":["`+beta+`","`+foreign+`","`+alpha+`"]}`,
	)
	check := runExtensionOrderStatusCheck(t, manifestPath)
	assertOrderPlan(t, check, "pi", "runtime-precedence", 1)
	reapplied := runExtensionOrderApply(t, manifestPath)
	assertOrderResults(t, reapplied, "pi", 1, "converged", true)

	retry := runExtensionOrderApply(t, manifestPath)
	assertOrderResults(t, retry, "pi", 1, "exact", false)
	assertOrderedText(t, string(testkit.ReadFile(t, settingsPath)), alpha, foreign, beta)
}

func TestExtensionAuthoringRefreshesOrderConstraintsAcrossSingletonTransitions(
	t *testing.T,
) {
	tempDir := t.TempDir()
	testkit.SetDefaultRootEnv(t, tempDir)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"pi\"]\n")

	runExtensionOrderCLI(t, []string{
		"add", "extension", "alpha", "npm:@acme/alpha@1.0.0",
		"--manifest", manifestPath, "--target", "pi",
	})
	assertLockedOrderMemberNames(t, lockfilePath, nil)

	runExtensionOrderCLI(t, []string{
		"add", "extension", "beta", "npm:@acme/beta@1.0.0",
		"--manifest", manifestPath, "--target", "pi",
	})
	assertLockedOrderMemberNames(t, lockfilePath, []string{"alpha", "beta"})

	runExtensionOrderCLI(t, []string{
		"remove", "extension", "alpha",
		"--manifest", manifestPath, "--target", "pi",
	})
	assertLockedOrderMemberNames(t, lockfilePath, nil)

	runExtensionOrderCLI(t, []string{
		"add", "extension", "alpha", "npm:@acme/alpha@1.0.0",
		"--manifest", manifestPath, "--target", "pi",
	})
	assertLockedOrderMemberNames(t, lockfilePath, []string{"beta", "alpha"})
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

func TestOpenCodeTUIOnlyExtensionOrderDoesNotCreateServerConfig(t *testing.T) {
	tempDir := t.TempDir()
	testkit.SetDefaultRootEnv(t, tempDir)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	serverPath := filepath.Join(tempDir, ".opencode", "opencode.json")
	tuiPath := filepath.Join(tempDir, ".opencode", "tui.json")
	alpha := "@acme/alpha@1.0.0"
	beta := "@acme/beta@1.0.0"
	foreign := "@acme/foreign@1.0.0"
	testkit.WriteFile(
		t,
		tempDir,
		".opencode/tui.json",
		`{"plugin":["`+alpha+`","`+beta+`"],"retained":true}`,
	)
	runExtensionOrderCLI(
		t,
		[]string{
			"import", "--target", "opencode", "--scope", "project",
			"--manifest", manifestPath,
		},
	)
	runExtensionOrderLock(t, manifestPath)

	testkit.WriteFile(
		t,
		tempDir,
		".opencode/tui.json",
		`{"plugin":["`+beta+`","`+foreign+`","`+alpha+`"],"retained":true}`,
	)
	dryRun := runExtensionOrderPlan(t, "apply", manifestPath, "--manage-existing")
	if dryRun.HasErrors ||
		len(dryRun.RelationOrders) != 1 ||
		dryRun.RelationOrders[0].SequenceID != "opencode:project:tui.json.plugins" ||
		dryRun.RelationOrders[0].Kind != "normalize" {
		t.Fatalf("TUI-only dry-run = %#v", dryRun.RelationOrders)
	}

	applied := runExtensionOrderApply(t, manifestPath, "--manage-existing")
	if len(applied.RelationOrderResults) != 1 ||
		applied.RelationOrderResults[0].SequenceID != "opencode:project:tui.json.plugins" ||
		applied.RelationOrderResults[0].Outcome != "converged" {
		t.Fatalf("TUI-only apply = %#v", applied.RelationOrderResults)
	}
	testkit.AssertPathMissing(t, serverPath)
	tui := string(testkit.ReadFile(t, tuiPath))
	assertOrderedText(t, tui, alpha, foreign, beta)
	if !strings.Contains(tui, `"retained":true`) {
		t.Fatalf("TUI sibling field was not retained: %s", tui)
	}
}

func TestOpenCodeJSONAndJSONCExtensionOrdersConvergeIndependently(t *testing.T) {
	tempDir := t.TempDir()
	testkit.SetDefaultRootEnv(t, tempDir)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	jsonPath := filepath.Join(tempDir, ".opencode", "opencode.json")
	jsoncPath := filepath.Join(tempDir, ".opencode", "opencode.jsonc")
	alpha := "@acme/alpha@1.0.0"
	beta := "@acme/beta@1.0.0"
	testkit.WriteFile(
		t,
		tempDir,
		".opencode/opencode.json",
		`{"plugin":["`+alpha+`","`+beta+`"],"variant":"json"}`,
	)
	testkit.WriteFile(
		t,
		tempDir,
		".opencode/opencode.jsonc",
		`{"plugin":["`+alpha+`","`+beta+`"],"variant":"jsonc"}`,
	)
	runExtensionOrderCLI(
		t,
		[]string{
			"import", "--target", "opencode", "--scope", "project",
			"--manifest", manifestPath,
		},
	)
	runExtensionOrderLock(t, manifestPath)

	testkit.WriteFile(
		t,
		tempDir,
		".opencode/opencode.json",
		`{"plugin":["`+beta+`","json-foreign","`+alpha+`"],"variant":"json"}`,
	)
	testkit.WriteFile(
		t,
		tempDir,
		".opencode/opencode.jsonc",
		`{"plugin":["`+beta+`","jsonc-foreign","`+alpha+`"],"variant":"jsonc"}`,
	)
	dryRun := runExtensionOrderPlan(t, "apply", manifestPath, "--manage-existing")
	if dryRun.HasErrors || len(dryRun.RelationOrders) != 2 {
		t.Fatalf("dual-config dry-run = %#v", dryRun.RelationOrders)
	}
	if dryRun.RelationOrders[0].SequenceID != "opencode:project:server.json.plugins" ||
		dryRun.RelationOrders[1].SequenceID != "opencode:project:server.jsonc.plugins" {
		t.Fatalf("dual-config sequence IDs = %#v", dryRun.RelationOrders)
	}

	applied := runExtensionOrderApply(t, manifestPath, "--manage-existing")
	if len(applied.RelationOrderResults) != 2 {
		t.Fatalf("dual-config results = %#v", applied.RelationOrderResults)
	}
	assertOrderedText(t, string(testkit.ReadFile(t, jsonPath)), alpha, "json-foreign", beta)
	assertOrderedText(t, string(testkit.ReadFile(t, jsoncPath)), alpha, "jsonc-foreign", beta)
	testkit.AssertPathMissing(t, filepath.Join(tempDir, ".opencode", "tui.json"))
	testkit.AssertPathMissing(t, filepath.Join(tempDir, ".opencode", "tui.jsonc"))
}

func TestPiGlobalExtensionImportLockApplyAndRetryConvergesRuntimeOrder(t *testing.T) {
	tempDir := t.TempDir()
	testkit.SetDefaultRootEnv(t, tempDir)
	agentRoot := filepath.Join(tempDir, "pi-agent")
	t.Setenv("PI_CODING_AGENT_DIR", agentRoot)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	settingsPath := filepath.Join(agentRoot, "settings.json")
	alpha := "npm:@acme/alpha@1.0.0"
	beta := "npm:@acme/beta@1.0.0"
	foreign := "npm:@acme/foreign@1.0.0"
	testkit.WriteFile(
		t,
		agentRoot,
		"settings.json",
		`{"packages":["`+alpha+`","`+beta+`"]}`,
	)

	runExtensionOrderCLI(
		t,
		[]string{
			"import", "--target", "pi", "--scope", "global",
			"--manifest", manifestPath,
		},
	)
	runExtensionOrderLock(t, manifestPath)
	testkit.WriteFile(
		t,
		agentRoot,
		"settings.json",
		`{"packages":["`+beta+`","`+foreign+`","`+alpha+`"]}`,
	)

	dryRun := runExtensionOrderPlan(t, "apply", manifestPath, "--manage-existing")
	if dryRun.HasErrors {
		t.Fatalf("global Pi manage-existing dry-run has errors: %#v", dryRun)
	}
	assertOrderPlan(t, dryRun, "pi", "runtime-precedence", 1)

	applied := runExtensionOrderApply(t, manifestPath, "--manage-existing")
	assertOrderResults(t, applied, "pi", 1, "converged", true)
	assertOrderedText(t, string(testkit.ReadFile(t, settingsPath)), alpha, foreign, beta)

	retry := runExtensionOrderApply(t, manifestPath)
	assertOrderResults(t, retry, "pi", 1, "exact", false)
}

func TestOpenCodeGlobalExtensionImportLockApplyAndRetryConvergesBothConfigOrders(
	t *testing.T,
) {
	tempDir := t.TempDir()
	testkit.SetDefaultRootEnv(t, tempDir)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	configRoot := filepath.Join(tempDir, "config", "opencode")
	configPath := filepath.Join(configRoot, "opencode.json")
	tuiPath := filepath.Join(configRoot, "tui.jsonc")
	alpha := "@acme/alpha@1.0.0"
	beta := "@acme/beta@1.0.0"
	foreign := "@acme/foreign@1.0.0"
	testkit.WriteFile(
		t,
		configRoot,
		"opencode.json",
		`{"plugin":["`+alpha+`","`+beta+`"],"theme":"night"}`,
	)
	testkit.WriteFile(
		t,
		configRoot,
		"tui.jsonc",
		`{"plugin":["`+alpha+`","`+beta+`"],"scroll_speed":3}`,
	)

	runExtensionOrderCLI(
		t,
		[]string{
			"import", "--target", "opencode", "--scope", "global",
			"--manifest", manifestPath,
		},
	)
	runExtensionOrderLock(t, manifestPath)
	testkit.WriteFile(
		t,
		configRoot,
		"opencode.json",
		`{"plugin":["`+beta+`","`+foreign+`","`+alpha+`"],"theme":"night"}`,
	)
	testkit.WriteFile(
		t,
		configRoot,
		"tui.jsonc",
		`{"plugin":["`+beta+`","`+foreign+`","`+alpha+`"],"scroll_speed":3}`,
	)

	dryRun := runExtensionOrderPlan(t, "apply", manifestPath, "--manage-existing")
	if dryRun.HasErrors {
		t.Fatalf("global OpenCode manage-existing dry-run has errors: %#v", dryRun)
	}
	assertOrderPlan(t, dryRun, "opencode", "config-order-only", 2)

	applied := runExtensionOrderApply(t, manifestPath, "--manage-existing")
	assertOrderResults(t, applied, "opencode", 2, "converged", true)
	assertOrderedText(t, string(testkit.ReadFile(t, configPath)), alpha, foreign, beta)
	assertOrderedText(t, string(testkit.ReadFile(t, tuiPath)), alpha, foreign, beta)

	retry := runExtensionOrderApply(t, manifestPath)
	assertOrderResults(t, retry, "opencode", 2, "exact", false)
}

func TestApplyRejectsManifestOrderChangedAfterLock(t *testing.T) {
	tempDir := t.TempDir()
	testkit.SetDefaultRootEnv(t, tempDir)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	settingsPath := filepath.Join(tempDir, ".pi", "settings.json")
	alpha := "npm:@acme/alpha@1.0.0"
	beta := "npm:@acme/beta@1.0.0"
	testkit.WriteFile(t, tempDir, "daem.toml", piOrderManifest(alpha, beta))
	runExtensionOrderLock(t, manifestPath)

	testkit.WriteFile(t, tempDir, "daem.toml", piOrderManifest(beta, alpha))
	hostContent := `{"packages":["` + beta + `","` + alpha + `"]}`
	testkit.WriteFile(t, tempDir, ".pi/settings.json", hostContent)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{
			"apply", "--manifest", manifestPath, "--yes", "--manage-existing",
		},
		&stdout,
		&stderr,
	)
	if exitCode != 1 ||
		!strings.Contains(stderr.String(), "locked extension order is stale") ||
		!strings.Contains(stderr.String(), "run daem lock") {
		t.Fatalf(
			"stale apply exitCode=%d stdout=%q stderr=%q",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
	testkit.AssertFileContent(t, settingsPath, hostContent)
}

func TestApplyRejectsOversizedExtensionOrderWithoutMutation(t *testing.T) {
	tempDir := t.TempDir()
	testkit.SetDefaultRootEnv(t, tempDir)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	settingsPath := filepath.Join(tempDir, ".pi", "settings.json")
	alpha := "npm:@acme/alpha@1.0.0"
	beta := "npm:@acme/beta@1.0.0"
	testkit.WriteFile(t, tempDir, "daem.toml", piOrderManifest(alpha, beta))
	runExtensionOrderLock(t, manifestPath)

	packages := make([]string, 0, 4_097)
	packages = append(packages, alpha, beta)
	for index := 0; len(packages) <= 4_096; index++ {
		packages = append(packages, fmt.Sprintf("npm:@foreign/pkg-%04d@1.0.0", index))
	}
	hostContent, err := json.Marshal(struct {
		Packages []string `json:"packages"`
	}{Packages: packages})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	testkit.WriteFile(t, tempDir, ".pi/settings.json", string(hostContent))

	status := runExtensionOrderPlan(t, "status", manifestPath)
	if len(status.RelationOrders) != 1 ||
		status.RelationOrders[0].Kind != "blocked" ||
		status.RelationOrders[0].Reason != "resource_limit_exceeded" ||
		!strings.Contains(
			status.RelationOrders[0].Detail,
			"observed_rows observed=4097 limit=4096",
		) {
		t.Fatalf("oversized status relation order = %#v", status.RelationOrders)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{
			"apply", "--manifest", manifestPath, "--yes", "--manage-existing",
		},
		&stdout,
		&stderr,
	)
	if exitCode != 1 ||
		!strings.Contains(stderr.String(), "extension order resource limit exceeded") ||
		!strings.Contains(stderr.String(), "observed_rows") {
		t.Fatalf(
			"oversized apply exitCode=%d stdout=%q stderr=%q",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
	testkit.AssertFileContent(t, settingsPath, string(hostContent))
}

func piOrderManifest(first string, second string) string {
	return `version = 1
targets = ["pi"]

[[extension]]
id = "first"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "` + first + `" }

[[extension]]
id = "second"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "` + second + `" }
`
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

func runExtensionOrderStatusCheck(t *testing.T, manifestPath string) clijson.Plan {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{"status", "--manifest", manifestPath, "--check", "--json"},
		&stdout,
		&stderr,
	)
	if exitCode != 1 || stderr.Len() != 0 {
		t.Fatalf(
			"status --check exitCode=%d stdout=%q stderr=%q",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
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
	if payload.HasErrors || len(payload.Errors) != 0 {
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

func assertLockedOrderMemberNames(
	t *testing.T,
	lockfilePath string,
	want []string,
) {
	t.Helper()
	file, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("load lockfile: %v", err)
	}
	constraints := file.Locked.OrderConstraints()
	if len(want) == 0 {
		if len(constraints) != 0 {
			t.Fatalf("order constraints = %#v, want none", constraints)
		}
		return
	}
	if len(constraints) != 1 {
		t.Fatalf("order constraints = %#v, want one", constraints)
	}
	members := constraints[0].Members()
	if len(members) != len(want) {
		t.Fatalf("order members = %#v, want %v", members, want)
	}
	for index, name := range want {
		if got := members[index].Subject().Key(); got != name {
			t.Fatalf("order member[%d] = %q, want %q", index, got, name)
		}
	}
}
