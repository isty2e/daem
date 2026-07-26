package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/execcheck"
)

func TestRunRemoveExtensionYesWritesManifestAndLockOnly(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", claudeExtensionManifest())

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("lock exitCode = %d, stderr = %q", exitCode, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{
		"remove", "extension", "context7-managed",
		"--manifest", manifestPath,
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"removed: extension/context7-managed",
		"change: remove extension resource",
		"lockfile: wrote " + lockfilePath,
		"note: remove updates the manifest and lockfile only; carrier uninstall, package cleanup, and bundled contribution deletion are not performed",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	assertNoCarrierSuccessClaims(t, stdout.String())
	normalized, err := declarationmanifest.Decode(testkit.ReadFile(t, manifestPath))
	if err != nil {
		t.Fatalf("declarationmanifest.Decode returned error: %v", err)
	}
	if len(normalized.Extensions()) != 0 {
		t.Fatalf("Extensions = %#v, want removed", normalized.Extensions())
	}
	locked, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(locked.Locked.Subjects()) != 0 {
		t.Fatalf("locked subjects = %#v, want none", locked.Locked.Subjects())
	}
}

func TestRunRemoveExtensionProjectRowPreservesSamePluginGlobalRelation(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", claudeProjectAndGlobalSamePluginManifest())

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("lock exitCode = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	canary := execcheck.New(t, "claude")

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{
		"remove", "extension", "context7-managed",
		"--manifest", manifestPath,
		"--target", "claude-code",
		"--scope", "project",
	}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("remove exitCode = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "next_steps") {
		t.Fatalf("stdout = %q, did not want human next steps in JSON", stdout.String())
	}
	execcheck.AssertClean(t, canary, "remove project extension with same-plugin global sibling")

	normalized, err := declarationmanifest.Decode(testkit.ReadFile(t, manifestPath))
	if err != nil {
		t.Fatalf("declarationmanifest.Decode returned error: %v", err)
	}
	if len(normalized.Extensions()) != 1 ||
		normalized.Extensions()[0].ID().Name() != "context7-global" ||
		string(normalized.Extensions()[0].Scope()) != "global" {
		t.Fatalf("Extensions = %#v, want only context7-global", normalized.Extensions())
	}
	assertCLIClaudeExtensionLockedSubjectWithScope(t, lockfilePath, "context7-global", "global")
}

func TestRunRemoveExtensionGlobalOmissionNeverUninstallsOrPrunes(t *testing.T) {
	fixture := writeCLIClaudeGlobalExtensionCarrierLockFixture(t)
	writeCLIClaudeGlobalObservedPresentAttemptStatefile(t, fixture)
	statefilePath := filepath.Join(fixture.root, ".daem", "state.json")
	stateBeforeRemove, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatalf("read statefile before remove: %v", err)
	}

	hostRoot := filepath.Join(fixture.root, "claude-host")
	t.Setenv("CLAUDE_CONFIG_DIR", hostRoot)
	retainedFiles := map[string]string{
		"plugins/installed_plugins.json":               `{"version":2,"plugins":{"context7":[]}}`,
		"plugins/cache/context7/package.json":          `{"name":"context7","version":"1.0.0"}`,
		"plugins/data/context7/retained-state.json":    `{"keep":true}`,
		"credentials/context7.json":                    `{"token":"must-stay"}`,
		"trust/context7.json":                          `{"approved":true}`,
		"sessions/context7/current/session-state.json": `{"session":"host-owned"}`,
	}
	for path, content := range retainedFiles {
		testkit.WriteFile(t, hostRoot, path, content)
	}
	canary := execcheck.New(t, "claude")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"remove", "extension", "context7-global",
		"--manifest", fixture.manifestPath,
		"--target", "claude-code",
		"--scope", "global",
		"--json",
	}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("remove exitCode = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "next_steps") {
		t.Fatalf("stdout = %q, did not want human next steps in JSON", stdout.String())
	}
	execcheck.AssertClean(t, canary, "remove global extension")
	stateAfterRemove, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatalf("read statefile after remove: %v", err)
	}
	if !bytes.Equal(stateAfterRemove, stateBeforeRemove) {
		t.Fatalf("remove extension changed history-only statefile")
	}
	assertRetainedClaudePluginFiles(t, hostRoot, retainedFiles)

	locked, err := lockfile.Load(fixture.lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(locked.Locked.Subjects()) != 0 {
		t.Fatalf("locked subjects = %#v, want none after declaration authoring", locked.Locked.Subjects())
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{
		"apply",
		"--manifest", fixture.manifestPath,
		"--target", "claude-code",
		"--dry-run",
		"--json",
	}, &stdout, &stderr)
	if exitCode != 1 || !strings.Contains(stderr.String(), `target "claude-code" does not match any manifest resource`) {
		t.Fatalf("targeted apply after remove exitCode = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	execcheck.AssertClean(t, canary, "targeted apply after global extension omission")
	assertRetainedClaudePluginFiles(t, hostRoot, retainedFiles)

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{
		"apply",
		"--manifest", fixture.manifestPath,
		"--dry-run",
		"--json",
	}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("dry-run apply after remove exitCode = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	dryRunPayload := clijson.DecodePlan(t, stdout.Bytes())
	if dryRunPayload.HasErrors || dryRunPayload.ActionCount != 0 || len(dryRunPayload.Actions) != 0 ||
		len(dryRunPayload.RelationActions) != 0 || len(dryRunPayload.HostRouteAttempts) != 0 {
		t.Fatalf("dry-run apply payload = %#v, want no destructive or delegated carrier work", dryRunPayload)
	}
	execcheck.AssertClean(t, canary, "dry-run apply after global extension omission")
	assertRetainedClaudePluginFiles(t, hostRoot, retainedFiles)

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{
		"apply",
		"--manifest", fixture.manifestPath,
		"--yes",
		"--json",
	}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("unfiltered apply after remove exitCode = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	payload := clijson.DecodeApplyResult(t, stdout.Bytes())
	if payload.HasErrors || payload.ActionCount != 0 || len(payload.Actions) != 0 ||
		len(payload.RelationActions) != 0 || len(payload.HostRouteAttempts) != 0 {
		t.Fatalf("apply payload = %#v, want no destructive or delegated carrier work", payload)
	}
	execcheck.AssertClean(t, canary, "apply after global extension omission")
	assertRetainedClaudePluginFiles(t, hostRoot, retainedFiles)
	stateAfterApply, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatalf("read statefile after apply: %v", err)
	}
	if !bytes.Equal(stateAfterApply, stateBeforeRemove) {
		t.Fatalf("apply after extension omission changed history-only statefile")
	}
}

func TestRunRemoveExtensionRetainsManagedCarrierAuthorityForReconciliation(t *testing.T) {
	fixture := writeCLIClaudeGlobalExtensionCarrierLockFixture(t)
	writeCLIClaudeGlobalManagedCarrierState(t, fixture)
	manifestBefore := testkit.ReadFile(t, fixture.manifestPath)
	lockfileBefore := testkit.ReadFile(t, fixture.lockfilePath)
	statefilePath := filepath.Join(fixture.root, ".daem", "state.json")
	stateBefore := testkit.ReadFile(t, statefilePath)
	canary := execcheck.New(t, "claude")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"remove", "extension", "context7-global",
		"--manifest", fixture.manifestPath,
		"--target", "claude-code",
		"--scope", "global",
		"--json",
	}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("remove exitCode = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	manifestAfter := testkit.ReadFile(t, fixture.manifestPath)
	if bytes.Equal(manifestAfter, manifestBefore) ||
		bytes.Contains(manifestAfter, []byte("context7-global")) {
		t.Fatalf("remove did not delete the extension declaration:\n%s", manifestAfter)
	}
	lockfileAfter := testkit.ReadFile(t, fixture.lockfilePath)
	if bytes.Equal(lockfileAfter, lockfileBefore) ||
		bytes.Contains(lockfileAfter, []byte("context7-global")) {
		t.Fatalf("remove did not refresh the lockfile without the extension:\n%s", lockfileAfter)
	}
	if !bytes.Equal(testkit.ReadFile(t, statefilePath), stateBefore) {
		t.Fatal("remove authoring changed durable managed-carrier authority")
	}
	execcheck.AssertClean(t, canary, "managed extension authoring remove")
}

func TestRunRemoveThenReAddExtensionCancelsPendingAbsence(t *testing.T) {
	fixture := writeCLIClaudeGlobalExtensionCarrierLockFixture(t)
	writeCLIClaudeGlobalManagedCarrierState(t, fixture)
	statefilePath := filepath.Join(fixture.root, ".daem", "state.json")
	stateBefore := testkit.ReadFile(t, statefilePath)
	configRoot := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configRoot)
	testkit.WriteFile(
		t,
		configRoot,
		"plugins/installed_plugins.json",
		`{"version":2,"plugins":{"context7@market":[{"scope":"user"}]}}`,
	)
	canary := execcheck.New(t, "claude")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"remove", "extension", "context7-global",
		"--manifest", fixture.manifestPath,
		"--target", "claude-code",
		"--scope", "global",
	}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("remove exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{
		"add", "extension", "context7-global", "context7@market",
		"--manifest", fixture.manifestPath,
		"--scope", "global",
	}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("re-add exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if !bytes.Equal(testkit.ReadFile(t, statefilePath), stateBefore) {
		t.Fatal("remove then re-add changed durable managed-carrier authority")
	}
	if claims := loadCLIGlobalCarrierClaims(t, fixture.manifestPath); len(claims) != 1 {
		t.Fatalf("claims after remove then re-add = %#v, want original active claim", claims)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{
		"apply",
		"--manifest", fixture.manifestPath,
		"--target", "claude-code",
		"--dry-run",
		"--json",
	}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("dry-run apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	payload := clijson.DecodePlan(t, stdout.Bytes())
	if payload.HasErrors ||
		len(payload.RelationActions) != 1 ||
		payload.RelationActions[0].Kind != "no_op" ||
		payload.RelationActions[0].InvokesHostRoute ||
		len(payload.CarrierAbsences) != 1 ||
		payload.CarrierAbsences[0].SelectedAction != "retain" ||
		payload.CarrierAbsences[0].InvokesHostRoute ||
		payload.CarrierAbsences[0].RetiresClaim ||
		payload.CarrierAbsences[0].BlocksOrdinaryApply ||
		len(payload.HostRouteAttempts) != 0 {
		t.Fatalf("dry-run apply payload = %#v, want retained relation without host work", payload)
	}
	if !bytes.Equal(testkit.ReadFile(t, statefilePath), stateBefore) {
		t.Fatal("dry-run apply after re-add changed durable managed-carrier authority")
	}
	execcheck.AssertClean(t, canary, "remove then re-add")
}
