//go:build darwin || linux

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	ownershipobserve "github.com/isty2e/daem/internal/assurance/observe/ownership"
	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/contractversion"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	recoverworkflow "github.com/isty2e/daem/internal/workflow/recover"
	refreshworkflow "github.com/isty2e/daem/internal/workflow/refresh"
	"github.com/isty2e/daem/internal/workflow/workspacecleanup"
	"github.com/isty2e/daem/test/ownershiptest"
)

func TestLostStateReleasePreservesSharedHostAndLetsUserReadopt(t *testing.T) {
	paths, _, config := prepareAgentConfigUnmanageCLI(t)
	writeApplyConfirmationFile(t, paths.ManifestRoot, "profile.toml", "model='profile-fixture'\n")
	manifest := readCleanupFile(t, paths.ManifestPath) + fmt.Sprintf(`
[[agent_config]]
id = "profile"
scope = "global"
path = "work.config.toml"
source = %q
ownership = "keys"
`, filepath.ToSlash(filepath.Join(paths.ManifestRoot, "profile.toml")))
	writeApplyConfirmationFile(t, paths.ManifestRoot, filepath.Base(paths.ManifestPath), manifest)
	runStateLossCommand(t, paths.ManifestPath, 0, "lock")
	runStateLossCommand(t, paths.ManifestPath, 0, "apply", "--yes")

	foreignRoot := t.TempDir()
	foreignManifest := filepath.Join(foreignRoot, "other.toml")
	writeApplyConfirmationFile(t, foreignRoot, "settings.toml", "review_model='foreign-fixture'\n")
	writeApplyConfirmationFile(t, foreignRoot, filepath.Base(foreignManifest), fmt.Sprintf(`version = 1
targets = ["codex"]
[[agent_config]]
id = "foreign"
scope = "global"
path = "config.toml"
source = %q
ownership = "keys"
`, filepath.ToSlash(filepath.Join(foreignRoot, "settings.toml"))))
	runStateLossCommand(t, foreignManifest, 0, "lock")
	runStateLossCommand(t, foreignManifest, 0, "apply", "--yes")
	before := readReleaseRegistry(t, paths)
	if len(before.Claims()) != 3 || len(before.DocumentBindings()) != 3 {
		t.Fatalf("invalid fixture: %d claims and %d bindings", len(before.Claims()), len(before.DocumentBindings()))
	}
	backup := filepath.Join(t.TempDir(), "original")
	if err := os.Rename(paths.StateDir, backup); err != nil {
		t.Fatal(err)
	}
	sharedSelection := filepath.Join(paths.ManifestRoot, "same-state.toml")
	writeApplyConfirmationFile(t, paths.ManifestRoot, filepath.Base(sharedSelection), manifest)
	registryBytes := readCleanupFile(t, paths.OwnershipRegistryPath)
	profile := filepath.Join(filepath.Dir(config), "work.config.toml")
	set := map[string]string{config: readCleanupFile(t, config), profile: readCleanupFile(t, profile), paths.ManifestPath: manifest, paths.LockfilePath: readCleanupFile(t, paths.LockfilePath)}

	refusal := runStateLossCommand(t, sharedSelection, 1, "status", "--check", "--json")
	assertLossReleaseGuidance(t, refusal, sharedSelection)
	preview := readOutputRelease(t, runStateLossCommand(t, sharedSelection, 0, "recover", "--release-output-ownership", "--dry-run", "--json"))
	if preview.Phase != "planned" || preview.Release.ClaimCount != 2 || preview.Release.BindingCount != 2 ||
		!slices.Equal(preview.Release.RecordedManifests, []string{paths.ManifestPath}) {
		t.Fatalf("wrong release scope: %+v", preview)
	}
	if preview.Release.WithheldManifestCount != 0 || len(preview.Release.Locations) != 2 {
		t.Fatalf("did not disclose both output locations: %+v", preview)
	}
	var stdout, stderr bytes.Buffer
	args := []string{"recover", "--release-output-ownership", "--manifest", sharedSelection}
	if code := RunWithOptions(args, interactiveRunOptions(strings.NewReader("no\n"), &stdout, &stderr)); code != 1 || !strings.Contains(stderr.String(), "Proceed with recover?") {
		t.Fatalf("declining release: exit=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	assertApplyConfirmationFileContent(t, paths.OwnershipRegistryPath, registryBytes)
	assertCLIPathMissing(t, paths.StatefilePath)

	released := readOutputRelease(t, runStateLossCommand(t, sharedSelection, 0, "recover", "--release-output-ownership", "--yes", "--json"))
	if released.Phase != "released" {
		t.Fatalf("release was not confirmed: %+v", released)
	}
	current := readReleaseRegistry(t, paths)
	remainingClaims, remainingBindings := current.Claims(), current.DocumentBindings()
	if len(remainingClaims) != 1 || len(remainingBindings) != 1 ||
		remainingClaims[0].Owner().ManifestPath() != foreignManifest || remainingBindings[0].Owner().ManifestPath() != foreignManifest ||
		!slices.ContainsFunc(before.Claims(), remainingClaims[0].Equal) || !slices.ContainsFunc(before.DocumentBindings(), remainingBindings[0].Equal) {
		t.Fatalf("release changed unrelated ownership: claims=%v bindings=%v", remainingClaims, remainingBindings)
	}
	assertCLIPathMissing(t, paths.StatefilePath)
	for path, content := range set {
		assertApplyConfirmationFileContent(t, path, content)
	}
	idempotent := readOutputRelease(t, runStateLossCommand(t, sharedSelection, 0, "recover", "--release-output-ownership", "--yes", "--json"))
	if idempotent.Phase != "nothing_to_release" || idempotent.Release.ClaimCount+idempotent.Release.BindingCount != 0 {
		t.Fatalf("unexpected repeated release: %+v", idempotent)
	}
	runStateLossCommand(t, paths.ManifestPath, 1, "apply", "--dry-run")
	runStateLossCommand(t, paths.ManifestPath, 0, "apply", "--yes", "--manage-existing")
	if content := readCleanupFile(t, paths.StatefilePath); content == "" {
		t.Fatal("explicit adoption did not record a new state")
	}
	for path, content := range set {
		assertApplyConfirmationFileContent(t, path, content)
	}
}

func TestStateLossStopsUnrelatedApplyAndRefresh(t *testing.T) {
	paths, _, config := prepareAgentConfigUnmanageCLI(t)
	runStateLossCommand(t, paths.ManifestPath, 0, "lock")
	runStateLossCommand(t, paths.ManifestPath, 0, "apply", "--yes")
	if err := os.Rename(paths.StatefilePath, filepath.Join(t.TempDir(), "state.json")); err != nil {
		t.Fatal(err)
	}
	assertLossReleaseGuidance(t, runStateLossCommand(t, paths.ManifestPath, 1, "unmanage", "agent-config", "preferences", "--dry-run", "--json"), paths.ManifestPath)
	writeApplyConfirmationFile(t, paths.ManifestRoot, "instruction.md", "wanted instructions\n")
	writeApplyConfirmationFile(t, paths.ManifestRoot, filepath.Base(paths.ManifestPath), `version = 1
targets = ["codex"]
[instructions.project]
source = "instruction.md"
targets = ["codex"]
`)
	runStateLossCommand(t, paths.ManifestPath, 0, "lock")
	registry := readCleanupFile(t, paths.OwnershipRegistryPath)
	assertLossReleaseGuidance(t, runStateLossCommand(t, paths.ManifestPath, 1, "apply", "--yes", "--json"), paths.ManifestPath)
	assertCLIPathMissing(t, filepath.Join(paths.ManifestRoot, "AGENTS.md"))
	marker := stageApplyPair(t, paths, true)
	markerBytes := readCleanupFile(t, marker)
	lockCommand := []string{"daem", "lock", "--manifest", paths.ManifestPath}
	for _, mode := range []string{"--dry-run", "--yes"} {
		blocked := readStateLossFailure(t, runStateLossCommand(t, paths.ManifestPath, 1, "apply", mode, "--json"))
		if !slices.ContainsFunc(blocked.Guidance, func(hint stateLossGuidance) bool { return hint.Kind == "note" }) ||
			!slices.ContainsFunc(blocked.Guidance, func(hint stateLossGuidance) bool { return slices.Equal(hint.Argv, lockCommand) }) ||
			slices.ContainsFunc(blocked.Guidance, func(hint stateLossGuidance) bool { return slices.Contains(hint.Argv, "--release-output-ownership") }) {
			t.Fatalf("pending recovery hid the loss or offered an unavailable release: %+v", blocked.Guidance)
		}
	}
	readStateLossFailure(t, runStateLossCommand(t, paths.ManifestPath, 1, "recover", "--release-output-ownership", "--yes", "--json"))
	assertApplyConfirmationFileContent(t, marker, markerBytes)
	// The existing lock operation can finish this separately qualified pair.
	runStateLossCommand(t, paths.ManifestPath, 0, "lock")
	assertCLIPathMissing(t, marker)

	writeApplyConfirmationFile(t, filepath.Dir(config), filepath.Base(config), "model='declared'\n[plugins.\"documents@openai-primary-runtime\"]\n")
	writeApplyConfirmationFile(t, paths.ManifestRoot, filepath.Base(paths.ManifestPath), `version = 1
targets = ["codex"]
[[extension]]
id = "documents"
carrier = "codex-plugin"
scope = "global"
source = { marketplace = "documents@openai-primary-runtime" }
`)
	runStateLossCommand(t, paths.ManifestPath, 0, "lock")
	var stdout, stderr bytes.Buffer
	if code := RunWithOptions([]string{"refresh", "extension", "documents", "--manifest", paths.ManifestPath, "--yes", "--json"}, RunOptions{
		Stdout: &stdout, Stderr: &stderr,
		RefreshExecuteOptions: refreshworkflow.ExecuteOptions{CommandOptions: subprocess.CommandOptions{
			Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
				t.Fatal("refresh invoked a host after losing its State")
				return subprocess.CommandResult{}
			},
		}},
	}); code != 1 || stderr.Len() != 0 {
		t.Fatalf("refresh exit=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	var refresh clipresent.RefreshReport
	if err := json.Unmarshal(stdout.Bytes(), &refresh); err != nil || refresh.Result.Class != "refused" ||
		refresh.Result.ReasonCode != "mutation_authority" || refresh.Result.Attempted || refresh.Result.AttemptHistory.Persisted {
		t.Fatalf("incorrect refresh refusal: %s (%v)", &stdout, err)
	}
	assertCLIPathMissing(t, paths.StatefilePath)
	assertApplyConfirmationFileContent(t, paths.OwnershipRegistryPath, registry)
}

func TestMissingStateRefusesDisjointAgentConfigCapture(t *testing.T) {
	paths, _, host := prepareAgentConfigUnmanageCLI(t)
	runStateLossCommand(t, paths.ManifestPath, 0, "lock")
	runStateLossCommand(t, paths.ManifestPath, 0, "apply", "--yes")
	hostContent := readCleanupFile(t, host) + "\nreview_model = 'capture-fixture'\n"
	writeApplyConfirmationFile(t, filepath.Dir(host), filepath.Base(host), hostContent)
	capture := []string{"import", "agent-config", "other", "--target", "codex", "--scope", "global", "--config-path", "config.toml", "--key", "/review_model", "--merge", "--dry-run", "--json"}
	runStateLossCommand(t, paths.ManifestPath, 0, capture...)

	manifest, registry := readCleanupFile(t, paths.ManifestPath), readCleanupFile(t, paths.OwnershipRegistryPath)
	if err := os.Remove(paths.StatefilePath); err != nil {
		t.Fatal(err)
	}
	assertLossReleaseGuidance(t, runStateLossCommand(t, paths.ManifestPath, 1, capture...), paths.ManifestPath)
	assertApplyConfirmationFileContent(t, paths.ManifestPath, manifest)
	assertApplyConfirmationFileContent(t, paths.OwnershipRegistryPath, registry)
	assertApplyConfirmationFileContent(t, host, hostContent)
	assertCLIPathMissing(t, paths.StatefilePath)
}

func TestOutputReleaseRefusesWhenBackupIsRestoredDuringConfirmation(t *testing.T) {
	paths, _, host := prepareAgentConfigUnmanageCLI(t)
	runStateLossCommand(t, paths.ManifestPath, 0, "lock")
	runStateLossCommand(t, paths.ManifestPath, 0, "apply", "--yes")
	backup := filepath.Join(t.TempDir(), "state.json")
	state, registry, payload := readCleanupFile(t, paths.StatefilePath), readCleanupFile(t, paths.OwnershipRegistryPath), readCleanupFile(t, host)
	if err := os.Rename(paths.StatefilePath, backup); err != nil {
		t.Fatal(err)
	}
	input := &beforeReadApplyConfirmation{
		reader: strings.NewReader("yes\n"), beforeRead: func() error { return os.Rename(backup, paths.StatefilePath) },
	}
	var stdout, stderr bytes.Buffer
	args := []string{"recover", "--release-output-ownership", "--manifest", paths.ManifestPath}
	if code := RunWithOptions(args, interactiveRunOptions(input, &stdout, &stderr)); code != 1 || !input.read {
		t.Fatalf("state restoration did not refuse the approved release: exit=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	assertApplyConfirmationFileContent(t, paths.StatefilePath, state)
	assertApplyConfirmationFileContent(t, paths.OwnershipRegistryPath, registry)
	assertApplyConfirmationFileContent(t, host, payload)
	runStateLossCommand(t, paths.ManifestPath, 0, "status", "--check")
}

func TestOutputReleaseDoesNotRebindAfterApprovedClaimsChange(t *testing.T) {
	paths, _, host := prepareAgentConfigUnmanageCLI(t)
	runStateLossCommand(t, paths.ManifestPath, 0, "lock")
	runStateLossCommand(t, paths.ManifestPath, 0, "apply", "--yes")
	if err := os.Remove(paths.StatefilePath); err != nil {
		t.Fatal(err)
	}
	claims := readReleaseRegistry(t, paths).Claims()
	if len(claims) != 1 {
		t.Fatalf("fixture did not apply one claim: %v", claims)
	}
	store, err := ownershipstore.New(paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	before := readCleanupFile(t, host)
	var changedRegistry string
	input := &beforeReadApplyConfirmation{reader: strings.NewReader("yes\n"), beforeRead: func() error {
		if _, err := store.RemoveClaim(t.Context(), claims[0]); err != nil {
			return err
		}
		changedRegistry = readCleanupFile(t, paths.OwnershipRegistryPath)
		return nil
	}}
	var stdout, stderr bytes.Buffer
	args := []string{"recover", "--release-output-ownership", "--manifest", paths.ManifestPath}
	if code := RunWithOptions(args, interactiveRunOptions(input, &stdout, &stderr)); code != 1 || changedRegistry == "" {
		t.Fatalf("changed consent did not refuse release: exit=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	assertApplyConfirmationFileContent(t, paths.OwnershipRegistryPath, changedRegistry)
	assertApplyConfirmationFileContent(t, host, before)
	assertCLIPathMissing(t, paths.StatefilePath)
	preview := readOutputRelease(t, runStateLossCommand(t, paths.ManifestPath, 0, "recover", "--release-output-ownership", "--dry-run", "--json"))
	if preview.Phase != "planned" || preview.Release.ClaimCount != 0 || preview.Release.BindingCount != 1 {
		t.Fatalf("the changed binding-only release needs a new approval: %+v", preview)
	}
}

func TestApprovedApplyKeepsCleanupRecordWhenStateDisappears(t *testing.T) {
	paths, _, host := prepareAgentConfigUnmanageCLI(t)
	runStateLossCommand(t, paths.ManifestPath, 0, "lock")
	runStateLossCommand(t, paths.ManifestPath, 0, "apply", "--yes")
	marker := stageApplyPair(t, paths, true)
	markerBytes, hostBytes, registryBytes := readCleanupFile(t, marker), readCleanupFile(t, host), readCleanupFile(t, paths.OwnershipRegistryPath)
	prepared, err := applyworkflow.PlanWrite(t.Context(), applyworkflow.CommandInput{ManifestPath: paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = prepared.Close() })
	if len(prepared.RecoveryCleanup) != 1 {
		t.Fatal("fixture did not plan to retire the completed pair")
	}
	if err := os.Rename(paths.StatefilePath, filepath.Join(t.TempDir(), "state.json")); err != nil {
		t.Fatal(err)
	}
	result, err := applyworkflow.ExecuteWithOptions(t.Context(), prepared, applyworkflow.ExecuteOptions{PlanWasDisclosed: true})
	var loss *ownershipobserve.MissingStateRefusal
	if !errors.As(err, &loss) || result.ExecutionAttempted || len(result.RecoveryCleanup) != 1 || result.RecoveryCleanup[0].Outcome != workspacecleanup.NotAttempted {
		t.Fatalf("state loss did not stop approved cleanup before effects: %+v (%v)", result.RecoveryCleanup, err)
	}
	failure := applyworkflow.ClassifyFailure(err, result)
	if failure.Reason() != applyworkflow.FailureReasonInterruptedFileSetTransaction || !strings.Contains(failure.Detail(), loss.Error()) {
		t.Fatalf("apply result lost the fence or the State diagnosis: %+v", failure)
	}
	diagnostic := applyFailureDiagnostic(paths.ManifestPath, err, applyFailureExecution, result, true)
	guidance := diagnostic.Guidance()
	lockCommand := []string{"daem", "lock", "--manifest", paths.ManifestPath}
	if !slices.ContainsFunc(guidance, func(hint clipresent.Guidance) bool {
		return hint.Kind() == clipresent.GuidanceNote && hint.Message() == loss.Error()
	}) || !slices.ContainsFunc(guidance, func(hint clipresent.Guidance) bool {
		return hint.Kind() == clipresent.GuidanceCommand && slices.Equal(hint.Argv(), lockCommand)
	}) || slices.ContainsFunc(guidance, func(hint clipresent.Guidance) bool {
		return slices.Contains(hint.Argv(), "--release-output-ownership")
	}) {
		t.Fatalf("approved apply did not direct the user to the remaining fence: %s", diagnostic.HumanText())
	}
	assertCLIPathMissing(t, paths.StatefilePath)
	assertApplyConfirmationFileContent(t, marker, markerBytes)
	assertApplyConfirmationFileContent(t, host, hostBytes)
	assertApplyConfirmationFileContent(t, paths.OwnershipRegistryPath, registryBytes)
}

func TestOutputReleaseLeavesPermissionRecoveryToItsOwner(t *testing.T) {
	fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml")
	if err := os.Remove(fixture.Paths.StatefilePath); err != nil {
		t.Fatal(err)
	}
	registry := readCleanupFile(t, fixture.Paths.OwnershipRegistryPath)
	refusal := readStateLossFailure(t, runStateLossCommand(t, fixture.Paths.ManifestPath, 1, "recover", "--release-output-ownership", "--yes", "--json"))
	if !slices.ContainsFunc(refusal.Guidance, func(hint stateLossGuidance) bool {
		return hint.Kind == "command" && slices.Equal(hint.Argv, []string{"daem", "recover", "--manifest", fixture.Paths.ManifestPath, "--dry-run"})
	}) {
		t.Fatalf("release refusal did not offer permission recovery: %+v", refusal.Guidance)
	}
	assertApplyConfirmationFileContent(t, fixture.Paths.OwnershipRegistryPath, registry)
	assertCLIPathMissing(t, fixture.Paths.StatefilePath)
	if plan, code := runPermissionRecoverCLI(t, fixture.Paths.ManifestPath, "--dry-run"); code != 0 || plan.HasErrors {
		t.Fatalf("ordinary permission recovery is no longer available: %d %+v", code, plan)
	}
}

func TestOutputReleaseReportsUnconfirmedCommitEvenWhenRegistryChanged(t *testing.T) {
	paths, _, host := prepareAgentConfigUnmanageCLI(t)
	runStateLossCommand(t, paths.ManifestPath, 0, "lock")
	runStateLossCommand(t, paths.ManifestPath, 0, "apply", "--yes")
	if err := os.Remove(paths.StatefilePath); err != nil {
		t.Fatal(err)
	}
	before := readCleanupFile(t, host)
	writer := &unconfirmedOutputReleaseWriter{}
	var stdout, stderr bytes.Buffer
	if code := RunWithOptions([]string{"recover", "--manifest", paths.ManifestPath, "--release-output-ownership", "--yes", "--json"}, RunOptions{
		Stdout: &stdout, Stderr: &stderr, RecoverExecuteOptions: recoverworkflow.ExecuteOptions{Filesystem: writer},
	}); code != 1 || stderr.Len() != 0 || writer.calls != 1 {
		t.Fatalf("unconfirmed release exit=%d calls=%d stdout=%s stderr=%s", code, writer.calls, &stdout, &stderr)
	}
	var report outputReleaseReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil || !report.HasErrors || report.Phase != "unconfirmed" ||
		strings.Contains(stdout.String(), "injected private sync failure") {
		t.Fatalf("invalid unconfirmed release: %s (%v)", &stdout, err)
	}
	assertApplyConfirmationFileContent(t, host, before)
	result := readOutputRelease(t, runStateLossCommand(t, paths.ManifestPath, 0, "recover", "--release-output-ownership", "--dry-run", "--json"))
	if result.Phase != "nothing_to_release" {
		t.Fatalf("fresh recovery did not inspect the published registry: %+v", result)
	}
}

type unconfirmedOutputReleaseWriter struct {
	storagecommit.Adapter
	calls int
}

func (writer *unconfirmedOutputReleaseWriter) ReplaceRootedFileWithOutcome(ctx context.Context, capability rootedpath.CommitCapability, content []byte, mode fs.FileMode, identity mutationfs.EntryIdentity) (mutationfs.CommitOutcome, error) {
	writer.calls++
	result, err := writer.Adapter.ReplaceRootedFileWithOutcome(ctx, capability, content, mode, identity)
	if err != nil {
		return result, err
	}
	unknown, err := mutationfs.NewCommitOutcome(mutationfs.CommitOutcomeIndeterminate, nil)
	return unknown, errors.Join(err, errors.New("injected private sync failure"))
}

func runStateLossCommand(t *testing.T, manifest string, wantCode int, args ...string) []byte {
	t.Helper()
	var stdout, stderr bytes.Buffer
	args = append(args, "--manifest", manifest)
	if code := RunWithOptions(args, RunOptions{Stdout: &stdout, Stderr: &stderr}); code != wantCode {
		t.Fatalf("%v exit=%d, want %d: stdout=%s stderr=%s", args, code, wantCode, &stdout, &stderr)
	}
	return stdout.Bytes()
}

func readReleaseRegistry(t *testing.T, paths daempaths.Paths) ownership.Registry {
	t.Helper()
	registry, err := ownershipstore.ReadProtectionRegistryBounded(t.Context(), paths.OwnershipRegistryPath, physicalwork.MaximumPhysicalPathDepth, physicalwork.NewCaptureBudget(nil))
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

type outputReleaseReport struct {
	SchemaVersion int    `json:"schema_version"`
	AuthorityKind string `json:"authority_kind"`
	Phase         string `json:"phase"`
	HasErrors     bool   `json:"has_errors"`
	Release       struct {
		ClaimCount            int      `json:"claim_count"`
		BindingCount          int      `json:"binding_count"`
		RecordedManifests     []string `json:"recorded_manifests"`
		WithheldManifestCount int      `json:"withheld_manifest_count"`
		Locations             []string `json:"locations"`
	} `json:"output_ownership_release"`
}

func readOutputRelease(t *testing.T, payload []byte) outputReleaseReport {
	t.Helper()
	var report outputReleaseReport
	if err := json.Unmarshal(payload, &report); err != nil || report.SchemaVersion != contractversion.RecoveryJSON ||
		report.AuthorityKind != "output_ownership_release" || report.HasErrors {
		t.Fatalf("invalid successful release: %s (%v)", payload, err)
	}
	return report
}

type stateLossGuidance struct {
	Kind string   `json:"kind"`
	Argv []string `json:"argv"`
}

type stateLossCLIFailure struct {
	ResultType string `json:"result_type"`
	Error      struct {
		Kind string `json:"kind"`
	} `json:"error"`
	Guidance []stateLossGuidance `json:"guidance"`
}

func readStateLossFailure(t *testing.T, payload []byte) stateLossCLIFailure {
	t.Helper()
	var result stateLossCLIFailure
	if err := json.Unmarshal(payload, &result); err != nil || result.ResultType != "cli_error" || result.Error.Kind != "operation" {
		t.Fatalf("not an early CLI failure: %s (%v)", payload, err)
	}
	return result
}

func assertLossReleaseGuidance(t *testing.T, payload []byte, selectedManifest string) {
	t.Helper()
	for _, guidance := range readStateLossFailure(t, payload).Guidance {
		if guidance.Kind == "command" && slices.Equal(guidance.Argv, []string{"daem", "recover", "--manifest", selectedManifest, "--release-output-ownership", "--dry-run"}) {
			return
		}
	}
	t.Fatalf("missing selected release command: %s", payload)
}
