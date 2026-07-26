package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/execcheck"
)

func TestRunUnmanageExtensionReleasesExactGlobalClaimAndRetainsHostState(t *testing.T) {
	fixture := writeCLIClaudeGlobalExtensionCarrierLockFixture(t)
	writeCLIClaudeGlobalManagedCarrierState(t, fixture)
	hostRoot, retainedFiles := writeCLIUnmanageHostInventory(t)
	t.Setenv("CLAUDE_CONFIG_DIR", hostRoot)
	canary := execcheck.New(t, "claude")
	statefilePath := filepath.Join(fixture.root, ".daem", "state.json")
	stateBefore := testkit.ReadFile(t, statefilePath)
	manifestBefore := testkit.ReadFile(t, fixture.manifestPath)
	lockfileBefore := testkit.ReadFile(t, fixture.lockfilePath)
	if claims := loadCLIGlobalCarrierClaims(t, fixture.manifestPath); len(claims) != 1 {
		t.Fatalf("claims before unmanage = %#v, want one", claims)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"unmanage", "extension", "context7-global",
		"--manifest", fixture.manifestPath,
		"--target", "claude-code",
		"--scope", "global",
		"--dry-run",
		"--json",
	}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("dry-run exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	dryRun := clijson.DecodeManifestAuthoring(t, stdout.Bytes())
	assertCLIUnmanageJSON(
		t,
		dryRun,
		"dry-run",
		"would_remove",
		"would_release",
		"would_write",
	)
	testkit.AssertFileContent(t, fixture.manifestPath, string(manifestBefore))
	testkit.AssertFileContent(t, fixture.lockfilePath, string(lockfileBefore))
	testkit.AssertFileContent(t, statefilePath, string(stateBefore))
	if claims := loadCLIGlobalCarrierClaims(t, fixture.manifestPath); len(claims) != 1 {
		t.Fatalf("claims after dry-run = %#v, want one", claims)
	}
	assertRetainedClaudePluginFiles(t, hostRoot, retainedFiles)
	execcheck.AssertClean(t, canary, "unmanage dry-run")

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{
		"unmanage", "extension", "context7-global",
		"--manifest", fixture.manifestPath,
		"--target", "claude-code",
		"--scope", "global",
		"--json",
	}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("write exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	written := clijson.DecodeManifestAuthoring(t, stdout.Bytes())
	assertCLIUnmanageJSON(t, written, "write", "removed", "released", "written")
	normalized, err := declarationmanifest.Decode(testkit.ReadFile(t, fixture.manifestPath))
	if err != nil {
		t.Fatalf("decode manifest after unmanage: %v", err)
	}
	if len(normalized.Extensions()) != 0 {
		t.Fatalf("extensions after unmanage = %#v, want none", normalized.Extensions())
	}
	locked, err := lockfile.Load(fixture.lockfilePath)
	if err != nil {
		t.Fatalf("load lockfile after unmanage: %v", err)
	}
	if len(locked.Locked.Subjects()) != 0 {
		t.Fatalf("locked subjects after unmanage = %#v, want none", locked.Locked.Subjects())
	}
	testkit.AssertFileContent(t, statefilePath, string(stateBefore))
	if claims := loadCLIGlobalCarrierClaims(t, fixture.manifestPath); len(claims) != 0 {
		t.Fatalf("claims after unmanage = %#v, want none", claims)
	}
	assertRetainedClaudePluginFiles(t, hostRoot, retainedFiles)
	execcheck.AssertClean(t, canary, "unmanage write")
}

func TestRunUnmanageExtensionReleasesClaimAfterManualManifestOmission(t *testing.T) {
	fixture := writeCLIClaudeGlobalExtensionCarrierLockFixture(t)
	writeCLIClaudeGlobalManagedCarrierState(t, fixture)
	hostRoot, retainedFiles := writeCLIUnmanageHostInventory(t)
	t.Setenv("CLAUDE_CONFIG_DIR", hostRoot)
	canary := execcheck.New(t, "claude")
	statefilePath := filepath.Join(fixture.root, ".daem", "state.json")
	stateBefore := testkit.ReadFile(t, statefilePath)

	manualManifest := "version = 1\ntargets = [\"claude-code\"]\n"
	testkit.WriteFile(t, fixture.root, "daem.toml", manualManifest)
	runExtensionAuthoringLock(t, fixture.manifestPath, fixture.lockfilePath)
	manifestBefore := testkit.ReadFile(t, fixture.manifestPath)
	lockfileBefore := testkit.ReadFile(t, fixture.lockfilePath)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"unmanage", "extension", "context7-global",
		"--manifest", fixture.manifestPath,
		"--target", "claude-code",
		"--scope", "global",
		"--json",
	}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("write exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	written := clijson.DecodeManifestAuthoring(t, stdout.Bytes())
	assertCLIUnmanageJSON(t, written, "write", "unchanged", "released", "written")
	testkit.AssertFileContent(t, fixture.manifestPath, string(manifestBefore))
	testkit.AssertFileContent(t, fixture.lockfilePath, string(lockfileBefore))
	testkit.AssertFileContent(t, statefilePath, string(stateBefore))
	if claims := loadCLIGlobalCarrierClaims(t, fixture.manifestPath); len(claims) != 0 {
		t.Fatalf("claims after manual-omission unmanage = %#v, want none", claims)
	}
	assertRetainedClaudePluginFiles(t, hostRoot, retainedFiles)
	execcheck.AssertClean(t, canary, "manual-omission unmanage")
}

func assertCLIUnmanageJSON(
	t *testing.T,
	payload clipresent.ManifestAuthoringJSONOutput,
	mode string,
	manifestStatus string,
	managementStatus string,
	registryStatus string,
) {
	t.Helper()
	if payload.Command != "unmanage" ||
		payload.Mode != mode ||
		payload.Operation != "unmanage" ||
		len(payload.Changes) != 1 {
		t.Fatalf("payload = %#v, want one %s unmanage change", payload, mode)
	}
	change := payload.Changes[0]
	if change.ResourceID != "extension/context7-global" ||
		change.Resource.Kind != "extension" ||
		change.Resource.Name != "context7-global" ||
		change.ChangeKind != manifestStatus ||
		change.Status != managementStatus ||
		change.Target != "claude-code" ||
		change.Scope != "global" {
		t.Fatalf("change = %#v, want exact global unmanage result", change)
	}
	if payload.Management == nil ||
		payload.Management.Status != managementStatus ||
		payload.Management.Statefile.Status != "unchanged" ||
		payload.Management.Registry.Status != registryStatus ||
		payload.Host == nil ||
		payload.Host.State != "retained" ||
		payload.Host.AmbientConsumers != "unobservable" {
		t.Fatalf(
			"management/host = %#v %#v, want host-retaining exact claim release",
			payload.Management,
			payload.Host,
		)
	}
}

func writeCLIUnmanageHostInventory(t *testing.T) (string, map[string]string) {
	t.Helper()
	hostRoot := filepath.Join(t.TempDir(), "claude-host")
	files := map[string]string{
		"plugins/installed_plugins.json":               `{"version":2,"plugins":{"context7":[]}}`,
		"plugins/cache/context7/package.json":          `{"name":"context7","version":"1.0.0"}`,
		"plugins/data/context7/retained-state.json":    `{"keep":true}`,
		"credentials/context7.json":                    `{"token":"must-stay"}`,
		"trust/context7.json":                          `{"approved":true}`,
		"sessions/context7/current/session-state.json": `{"session":"host-owned"}`,
	}
	for path, content := range files {
		testkit.WriteFile(t, hostRoot, path, content)
	}
	return hostRoot, files
}

func TestRunUnmanageExtensionAlreadyAbsentLeavesAllOwnersUnchanged(t *testing.T) {
	fixture := writeCLIClaudeGlobalExtensionCarrierLockFixture(t)
	testkit.WriteFile(
		t,
		fixture.root,
		"daem.toml",
		"version = 1\ntargets = [\"claude-code\"]\n",
	)
	runExtensionAuthoringLock(t, fixture.manifestPath, fixture.lockfilePath)
	manifestBefore := testkit.ReadFile(t, fixture.manifestPath)
	lockfileBefore := testkit.ReadFile(t, fixture.lockfilePath)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"unmanage", "extension", "context7-global",
		"--manifest", fixture.manifestPath,
		"--target", "claude-code",
		"--scope", "global",
	}, &stdout, &stderr)
	if exitCode != 1 ||
		!strings.Contains(stderr.String(), "extension management not found") {
		t.Fatalf(
			"exitCode=%d stdout=%q stderr=%q, want already-absent failure",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
	testkit.AssertFileContent(t, fixture.manifestPath, string(manifestBefore))
	testkit.AssertFileContent(t, fixture.lockfilePath, string(lockfileBefore))
	if claims := loadCLIGlobalCarrierClaims(t, fixture.manifestPath); len(claims) != 0 {
		t.Fatalf("claims after already-absent unmanage = %#v, want none", claims)
	}
	if _, err := os.Stat(filepath.Join(fixture.root, ".daem", "state.json")); !os.IsNotExist(err) {
		t.Fatalf("statefile stat error = %v, want absent", err)
	}
}
