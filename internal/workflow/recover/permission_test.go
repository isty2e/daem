//go:build darwin || linux

package recover

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	journalrecovery "github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/executionidentity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/subprocess/executioncontext"
	"github.com/isty2e/daem/internal/workflow/recover/permissions"
	"github.com/isty2e/daem/test/ownershiptest"
	"golang.org/x/sys/unix"
)

func TestPermissionRecoverWithoutSourceOrJournal(t *testing.T) {
	fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml", ".pi/settings.json")
	beforeState, _ := os.ReadFile(fixture.Paths.StatefilePath)
	beforeHost, _ := os.ReadFile(fixture.Documents[0])
	prepared, err := Plan(t.Context(), PlanInput{ManifestPath: fixture.Paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	plan, selected := prepared.PermissionDisclosure()
	if !selected || prepared.Disclosure() != nil || plan.HasErrors() || len(plan.Records()) != 1 {
		t.Fatalf("permission selection: %+v, %v", plan, selected)
	}
	info, _ := os.Stat(fixture.Documents[0])
	if info.Mode().Perm() != 0o644 {
		t.Fatal("planning changed permissions")
	}
	result, err := Execute(t.Context(), prepared, ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	safety, ok := result.PermissionResult()
	if !ok || !safety.Complete() || safety.Records()[0].Settlement() != permissions.SettlementCleared {
		t.Fatalf("result=%+v", safety)
	}
	for _, path := range fixture.Documents {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("not contained: %v", err)
		}
	}
	afterHost, _ := os.ReadFile(fixture.Documents[0])
	afterState, _ := os.ReadFile(fixture.Paths.StatefilePath)
	if !bytes.Equal(beforeHost, afterHost) || !bytes.Equal(beforeState, afterState) {
		t.Fatal("permission recovery changed payloads")
	}
	rows, err := fixture.Store.LoadPendingProtections(t.Context())
	if err != nil || len(rows) != 0 {
		t.Fatalf("protection not cleared: %v", err)
	}
	if _, err := Execute(t.Context(), prepared, ExecuteOptions{}); !errors.Is(err, ErrPreparedRecoveryConsumed) {
		t.Fatalf("reused prepared operation: %v", err)
	}
}

func TestPermissionRecoverContainsSafePeersAndRetriesWithoutRestoringRights(t *testing.T) {
	fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml", ".pi/settings.json")
	if err := os.Chmod(fixture.Documents[0], 0o744); err != nil {
		t.Fatal(err)
	}
	prepared, err := Plan(t.Context(), PlanInput{ManifestPath: fixture.Paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Execute(t.Context(), prepared, ExecuteOptions{})
	if err == nil {
		t.Fatal("blocked target became whole recovery success")
	}
	safety, _ := result.PermissionResult()
	if safety.Complete() {
		t.Fatal("partial result reported completion")
	}
	secure, _ := os.Stat(fixture.Documents[1])
	blocked, _ := os.Stat(fixture.Documents[0])
	if secure.Mode().Perm() != 0o600 || blocked.Mode().Perm() != 0o744 {
		t.Fatal("independent restriction or owner/execute refusal failed")
	}
	rows, err := fixture.Store.LoadPendingProtections(t.Context())
	if err != nil || len(rows) != 1 {
		t.Fatalf("partial recovery lost frozen obligations: %v", err)
	}
	if err := os.Chmod(fixture.Documents[0], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fixture.Documents[1], 0o600); err != nil {
		t.Fatal(err)
	}
	retry, err := Plan(t.Context(), PlanInput{ManifestPath: fixture.Paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(t.Context(), retry, ExecuteOptions{}); err != nil {
		t.Fatal(err)
	}
	secure, _ = os.Stat(fixture.Documents[1])
	if secure.Mode().Perm() != 0o600 {
		t.Fatal("retry restored removed rights")
	}
}

func TestPermissionRecoverQualifiesAbsenceWithoutRecreatingDocument(t *testing.T) {
	fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml")
	if err := os.Remove(fixture.Documents[0]); err != nil {
		t.Fatal(err)
	}
	prepared, err := Plan(t.Context(), PlanInput{ManifestPath: fixture.Paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Execute(t.Context(), prepared, ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	safety, _ := result.PermissionResult()
	if !safety.Complete() || safety.Records()[0].Targets()[0].State() != permissions.TargetAbsent {
		t.Fatal("absence was not discharged independently")
	}
	if _, err := os.Stat(fixture.Documents[0]); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("recovery recreated deleted settings")
	}
}

func TestPermissionRecoverBlocksLiveOrUnqualifiedExecutionWithoutSignaling(t *testing.T) {
	for _, live := range []bool{true, false} {
		t.Run(map[bool]string{true: "live", false: "legacy-unqualified"}[live], func(t *testing.T) {
			fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml")
			command := exec.Command("/bin/sleep", "30")
			command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
			current, err := executioncontext.ObserveExecutionContext(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if !live {
				current, err = executionidentity.NewContext(current.Platform(), current.Realm(), current.Epoch())
				if err != nil {
					t.Fatal(err)
				}
			}
			binding, err := executionidentity.NewBinding(fixture.Protection.Invocation(), current, int64(command.Process.Pid))
			if err != nil {
				t.Fatal(err)
			}
			bind, err := ownership.NewBindProtection(fixture.Protection, binding)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.Store.TransitionProtection(t.Context(), fixture.Owner, bind); err != nil {
				t.Fatal(err)
			}
			prepared, err := Plan(t.Context(), PlanInput{ManifestPath: fixture.Paths.ManifestPath})
			if err != nil {
				t.Fatal(err)
			}
			disclosure, selected := prepared.PermissionDisclosure()
			if !selected || !disclosure.HasErrors() {
				t.Fatal("unqualified execution was not disclosed as blocked")
			}
			if _, err := Execute(t.Context(), prepared, ExecuteOptions{}); err == nil {
				t.Fatal("unqualified execution admitted chmod")
			}
			if err := command.Process.Signal(syscall.Signal(0)); err != nil {
				t.Fatalf("Recover signaled the stored process: %v", err)
			}
			info, _ := os.Stat(fixture.Documents[0])
			if info.Mode().Perm() != 0o644 {
				t.Fatal("blocked process did not fence permissions")
			}
		})
	}
}

func TestPermissionRecoverScopesNamespaceAndCohortFailuresToTheirTargets(t *testing.T) {
	for _, change := range []string{"namespace", "cohort"} {
		t.Run(change, func(t *testing.T) {
			fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml", ".pi/settings.json")
			prepared, err := Plan(t.Context(), PlanInput{ManifestPath: fixture.Paths.ManifestPath})
			if err != nil {
				t.Fatal(err)
			}
			if change == "namespace" {
				parent := filepath.Dir(fixture.Documents[0])
				if err := os.Rename(parent, parent+"-old"); err != nil {
					t.Fatal(err)
				}
				replacement := t.TempDir()
				if err := os.WriteFile(filepath.Join(replacement, "config.toml"), []byte("replacement"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(filepath.Join(replacement, "config.toml"), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(replacement, parent); err != nil {
					t.Fatal(err)
				}
			} else {
				current, err := fixture.Store.LoadProtectionRegistry(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				var bindings []ownership.DocumentBinding
				for _, binding := range current.DocumentBindings() {
					if binding.PathAuthority().Key() != fixture.Protection.Documents()[0].PathAuthority().Key() {
						bindings = append(bindings, binding)
					}
				}
				current, err = ownership.NewRegistryWithProtections(current.Claims(), bindings, current.PendingProtections())
				if err != nil {
					t.Fatal(err)
				}
				content, err := ownershipstore.Marshal(current)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(fixture.Store.Path(), content, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Execute(t.Context(), prepared, ExecuteOptions{}); err == nil {
				t.Fatal("changed authority completed recovery")
			}
			blocked, _ := os.Stat(fixture.Documents[0])
			secure, _ := os.Stat(fixture.Documents[1])
			if blocked.Mode().Perm() != 0o644 || secure.Mode().Perm() != 0o600 {
				t.Fatal("changed target authority hid an independently secure peer")
			}
		})
	}
}

func TestPermissionRecoverFailedClearRetainsFenceAndRetryUsesSecureModes(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("requires a non-privileged filesystem writer")
	}
	fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml")
	registryDir := filepath.Dir(fixture.Store.Path())
	if err := os.Chmod(registryDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(registryDir, 0o700) })
	prepared, err := Plan(t.Context(), PlanInput{ManifestPath: fixture.Paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Execute(t.Context(), prepared, ExecuteOptions{})
	if err == nil {
		t.Fatal("failed publication reported settlement")
	}
	safety, _ := result.PermissionResult()
	if safety.Records()[0].Settlement() != permissions.SettlementPending {
		t.Fatalf("lost current retained fence: %s", safety.Phase())
	}
	info, _ := os.Stat(fixture.Documents[0])
	if info.Mode().Perm() != 0o600 {
		t.Fatal("clear failure undid containment")
	}
	if err := os.Chmod(registryDir, 0o700); err != nil {
		t.Fatal(err)
	}
	retry, err := Plan(t.Context(), PlanInput{ManifestPath: fixture.Paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(t.Context(), retry, ExecuteOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestPermissionRecoverRetainsForeignProtectionOnSuccessfulCAS(t *testing.T) {
	fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml")
	state, err := mutation.ObservePersistedDirectoryEntryAuthority(filepath.Join(fixture.Paths.ManifestRoot, "foreign-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(state.Exact(), filepath.Join(fixture.Paths.ManifestRoot, "foreign.toml"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(fixture.Paths.ManifestRoot, "foreign-settings.json")
	physical, err := mutation.ObservePersistedDirectoryEntryAuthority(path)
	if err != nil {
		t.Fatal(err)
	}
	destination, _ := output.Parse("foreign-settings.json")
	binding, err := ownership.NewDocumentBinding(owner, destination, physical.Exact())
	if err != nil {
		t.Fatal(err)
	}
	baseline := fixture.Protection.Documents()[0]
	document, err := ownership.NewProtectedDocument([]ownership.DocumentBinding{binding}, baseline.UserID(), baseline.GroupID(), baseline.ModeCap())
	if err != nil {
		t.Fatal(err)
	}
	invocation, _ := executionidentity.NewInvocation(strings.Repeat("f", 32))
	foreign, err := ownership.NewArmedProtection(owner, invocation, fixture.Protection.Correlation(), []ownership.ProtectedDocument{document})
	if err != nil {
		t.Fatal(err)
	}
	current, err := fixture.Store.LoadProtectionRegistry(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	current, err = ownership.NewRegistryWithProtections(current.Claims(), append(current.DocumentBindings(), binding), current.PendingProtections())
	if err != nil {
		t.Fatal(err)
	}
	content, err := ownershipstore.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.Store.Path(), content, 0o600); err != nil {
		t.Fatal(err)
	}
	arm, _ := ownership.NewArmProtection(foreign)
	if _, err := fixture.Store.TransitionProtection(t.Context(), owner, arm); err != nil {
		t.Fatal(err)
	}
	prepared, err := Plan(t.Context(), PlanInput{ManifestPath: fixture.Paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(t.Context(), prepared, ExecuteOptions{}); err != nil {
		t.Fatal(err)
	}
	current, err = fixture.Store.LoadProtectionRegistry(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	retained, present := current.Protection(foreign.Invocation())
	if !present || !retained.Equal(foreign) || len(current.PendingProtections()) != 1 {
		t.Fatal("owner settlement replaced foreign safety facts")
	}
}

func TestPermissionExecutionReportsUnknownRatherThanAStalePlannedFence(t *testing.T) {
	fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml")
	first, err := Plan(t.Context(), PlanInput{ManifestPath: fixture.Paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Plan(t.Context(), PlanInput{ManifestPath: fixture.Paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(t.Context(), first, ExecuteOptions{}); err != nil {
		t.Fatal(err)
	}
	result, err := Execute(t.Context(), second, ExecuteOptions{})
	if err == nil {
		t.Fatal("stale preparation claimed fresh completion")
	}
	safety, selected := result.PermissionResult()
	if !selected || safety.Phase() != permissions.ResultUnknown || safety.Records()[0].Settlement() != permissions.SettlementUnknown {
		t.Fatal("planning-time authority became a retained-fence assertion")
	}
	fixture = ownershiptest.PermissionRecovery(t, ".codex/config.toml")
	canceled, err := Plan(t.Context(), PlanInput{ManifestPath: fixture.Paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err = Execute(ctx, canceled, ExecuteOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	safety, selected = result.PermissionResult()
	if !selected || safety.Phase() != permissions.ResultUnknown {
		t.Fatal("early failure claimed a freshly observed fence")
	}
}

func TestPermissionRecoveryQualifiesEveryCohortOwnersResolutionContext(t *testing.T) {
	fixture := ownershiptest.PermissionRecovery(t, "sub/.codex/config.toml")
	foreignPaths, err := daempaths.Resolve(filepath.Join(fixture.Paths.ManifestRoot, "sub", "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(foreignPaths.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreignPaths.StatefilePath, []byte("malformed foreign state"), 0o600); err != nil {
		t.Fatal(err)
	}
	observed, err := mutation.ObservePersistedDirectoryEntryAuthority(foreignPaths.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	foreignOwner, err := stateauthority.New(observed.Exact(), foreignPaths.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	destination, _ := output.Parse(".codex/config.toml")
	baseline := fixture.Protection.Documents()[0]
	binding, err := ownership.NewDocumentBinding(foreignOwner, destination, baseline.PathAuthority())
	if err != nil {
		t.Fatal(err)
	}
	bindings := append(baseline.Bindings(), binding)
	document, err := ownership.NewProtectedDocument(bindings, baseline.UserID(), baseline.GroupID(), baseline.ModeCap())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := ownership.NewRegistry(nil, bindings...)
	if err != nil {
		t.Fatal(err)
	}
	content, err := ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.Store.Path(), content, 0o600); err != nil {
		t.Fatal(err)
	}
	row, err := ownership.NewArmedProtection(fixture.Owner, fixture.Protection.Invocation(), fixture.Protection.Correlation(), []ownership.ProtectedDocument{document})
	if err != nil {
		t.Fatal(err)
	}
	arm, _ := ownership.NewArmProtection(row)
	if _, err := fixture.Store.TransitionProtection(t.Context(), fixture.Owner, arm); err != nil {
		t.Fatal(err)
	}
	prepared, err := Plan(t.Context(), PlanInput{ManifestPath: fixture.Paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := prepared.PermissionDisclosure()
	if plan.HasErrors() {
		t.Fatalf("legitimate cross-owner cohort blocked: %v", plan.Records()[0].Targets()[0].Cause())
	}
	if _, err := Execute(t.Context(), prepared, ExecuteOptions{}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(fixture.Documents[0])
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("cross-owner permissions were not contained", err)
	}
}

type permissionCommandDirectory struct{ directory *os.File }

func (binding *permissionCommandDirectory) Validate() error {
	return subprocess.ValidateWorkingDirectory(binding.directory)
}

func (binding *permissionCommandDirectory) OpenDirectory() (*os.File, error) {
	fd, err := unix.Dup(int(binding.directory.Fd()))
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), binding.directory.Name()), nil
}
func (binding *permissionCommandDirectory) Close() error { return binding.directory.Close() }

func TestPermissionRecoverAfterRealGatedInvocationAndDurableBinding(t *testing.T) {
	fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml")
	if err := os.Chmod(fixture.Documents[0], 0o600); err != nil {
		t.Fatal(err)
	}
	bindDirectory := func() (subprocess.WorkingDirectoryBinding, error) {
		directory, err := os.Open(fixture.Paths.ManifestRoot)
		if err != nil {
			return nil, err
		}
		return &permissionCommandDirectory{directory: directory}, nil
	}
	unfinished := errors.New("safety settlement was interrupted")
	attempt, finalizationErr := (subprocess.CommandExecutor{}).ExecuteProtectedInWorkingDirectory(t.Context(), subprocess.CommandAttemptRequest{Command: "/bin/chmod", Args: []string{"644", fixture.Documents[0]}}, bindDirectory, fixture.Protection.Invocation(), func(ctx context.Context, binding executionidentity.Binding) error {
		transition, err := ownership.NewBindProtection(fixture.Protection, binding)
		if err != nil {
			return err
		}
		_, err = fixture.Store.TransitionProtection(ctx, fixture.Owner, transition)
		return err
	}, func(context.Context, subprocess.ProtectedCommandCompletion) error { return unfinished })
	if !errors.Is(finalizationErr, unfinished) {
		t.Fatal("unfinished safety was hidden", finalizationErr)
	}
	if !attempt.ReleaseAttempted() || !attempt.Attempt().Succeeded() {
		t.Fatal("gated target did not execute after binding publication", attempt.Attempt().ErrorDetail())
	}
	if _, err := attempt.Quiescence(); err != nil {
		t.Fatal(err)
	}
	prepared, err := Plan(t.Context(), PlanInput{ManifestPath: fixture.Paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	plan, selected := prepared.PermissionDisclosure()
	if !selected || plan.HasErrors() || plan.Records()[0].Phase() != ownership.ProtectionBound {
		t.Fatal("persisted bound invocation was not independently recoverable")
	}
	result, err := Execute(t.Context(), prepared, ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	safety, selected := result.PermissionResult()
	if !selected || !safety.Complete() || !safety.Records()[0].Targets()[0].Expanded() {
		t.Fatal("fresh recovery did not retain actual expansion/settlement facts")
	}
	info, err := os.Stat(fixture.Documents[0])
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("real invocation permissions remained expanded", err)
	}
}

func TestProtectedFinalizerContainsAndSettlesBeforeReturningMalformedCapture(t *testing.T) {
	testProtectedFinalizerWithPrepaidWork(t, true)
}

func TestProtectedFinalizerLeavesProtectionWhenPrepaidSafetyIsUnavailable(t *testing.T) {
	testProtectedFinalizerWithPrepaidWork(t, false)
}

func testProtectedFinalizerWithPrepaidWork(t *testing.T, prepaySafety bool) {
	t.Helper()
	fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml")
	if err := os.Chmod(fixture.Documents[0], 0o600); err != nil {
		t.Fatal(err)
	}

	// This fixture reserves a finite native-adapter envelope. The complete
	// producer still owns compiling demand from its frozen cohort and schedule.
	var passes []int
	for range 1024 {
		passes = append(passes, journalrecovery.MaximumPhysicalPathDepth)
	}
	artifact, err := journalrecovery.NewArtifactWork(4096, 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	forwardWork, err := journalrecovery.NewInvocationWork(passes, artifact)
	if err != nil {
		t.Fatal(err)
	}
	safetyWork := journalrecovery.InvocationWork{}
	if prepaySafety {
		safetyWork = forwardWork
	}
	parent := journalrecovery.NewPhysicalCaptureBudget(journalrecovery.NewPhysicalPathBudget())
	reservation, err := parent.ReserveInvocation(forwardWork, safetyWork)
	if err != nil {
		t.Fatal(err)
	}
	forward, safety, err := reservation.Take()
	if err != nil {
		t.Fatal(err)
	}
	root, destination, err := rootedpath.CaptureDestinationBounded(fixture.Paths.OwnershipRegistryPath, journalrecovery.MaximumPhysicalPathDepth, forward)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	forwardStore, err := ownershipstore.NewRooted(root, destination, journalrecovery.MaximumPhysicalPathDepth, forward)
	if err != nil {
		t.Fatal(err)
	}

	mutations, err := mutation.NewStore(fixture.Paths.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	var domains []mutation.Domain
	controls := []string{fixture.Paths.StatefilePath, fixture.Paths.OwnershipRegistryPath, fixture.Paths.RecoveryDir}
	documents := []mutation.PhysicalAuthorityRequest{{Path: fixture.Documents[0], Target: "codex", Scope: "project"}}
	for _, path := range controls {
		for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
			domain, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{Path: path, Access: mutation.AccessExclusive, Effect: effect})
			if err != nil {
				t.Fatal(err)
			}
			domains = append(domains, domain)
		}
	}
	for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
		domain, err := mutation.NewPhysicalPathDomain(mutation.PhysicalPathRequest{Path: fixture.Documents[0], Access: mutation.AccessExclusive, Effect: effect, Target: "codex", Scope: "project"})
		if err != nil {
			t.Fatal(err)
		}
		domains = append(domains, domain)
	}
	leases, err := mutations.Acquire(t.Context(), domains...)
	if err != nil {
		t.Fatal(err)
	}
	defer leases.Release()

	bindDirectory := func() (subprocess.WorkingDirectoryBinding, error) {
		directory, err := os.Open(fixture.Paths.ManifestRoot)
		if err != nil {
			return nil, err
		}
		return &permissionCommandDirectory{directory: directory}, nil
	}

	var bound ownership.PendingProtection
	widened := errors.New("contained expansion still requires a non-success outcome")
	finalized := 0
	result, err := (subprocess.CommandExecutor{}).ExecuteProtectedInWorkingDirectory(t.Context(), subprocess.CommandAttemptRequest{Command: "/bin/sh", Args: []string{"-c", `/bin/chmod 644 "$1"; printf '[malformed'`, "protected", fixture.Documents[0]}}, bindDirectory, fixture.Protection.Invocation(), func(ctx context.Context, binding executionidentity.Binding) error {
		if matches, err := leases.MatchCurrentExclusiveRequestsBounded(ctx, controls, documents, journalrecovery.MaximumPhysicalPathDepth, forward); err != nil || !matches {
			return errors.Join(mutation.StaleSnapshotError{}, err)
		}
		transition, err := ownership.NewBindProtection(fixture.Protection, binding)
		if err != nil {
			return err
		}
		registry, err := forwardStore.TransitionProtection(ctx, fixture.Owner, transition)
		if err == nil {
			bound, _ = registry.Protection(fixture.Protection.Invocation())
		}
		return err
	}, func(ctx context.Context, completion subprocess.ProtectedCommandCompletion) error {
		finalized++
		binding, present := completion.Binding()
		proof, err := completion.Quiescence()
		if !present || err != nil || proof.Require(binding) != nil {
			return errors.New("native quiescence is unavailable")
		}

		if err := forward.AdmitPhysicalWork(len(passes)*journalrecovery.MaximumPhysicalPathDepth, 0, 0); err == nil {
			return errors.New("forward overdraw did not exercise allocation failure")
		}
		if matches, err := leases.MatchCurrentExclusiveRequestsBounded(ctx, controls, documents, journalrecovery.MaximumPhysicalPathDepth, safety); err != nil || !matches {
			return errors.Join(mutation.StaleSnapshotError{}, err)
		}

		safetyStore, err := ownershipstore.NewRooted(root, destination, journalrecovery.MaximumPhysicalPathDepth, safety)
		if err != nil {
			return err
		}
		document := bound.Documents()[0]
		registry, err := safetyStore.LoadProtectionRegistry(ctx)
		if err != nil {
			return err
		}
		if err := registry.RequireProtectedDocument(bound, document); err != nil {
			return err
		}

		namespace, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(fixture.Documents[0], journalrecovery.MaximumPhysicalPathDepth, safety)
		if err != nil {
			return err
		}
		if !namespace.Exact().Equal(document.PathAuthority()) {
			return mutation.StaleSnapshotError{}
		}

		documentRoot, entry, err := rootedpath.CaptureDestinationBounded(fixture.Documents[0], journalrecovery.MaximumPhysicalPathDepth, safety)
		if err != nil {
			return err
		}
		defer documentRoot.Close()
		capability, err := documentRoot.AcquireBounded(entry, journalrecovery.MaximumPhysicalPathDepth, safety)
		if err != nil {
			return err
		}
		request, err := storagecommit.NewRootedPermissionRestriction(capability, document.Restriction)
		if err != nil {
			return errors.Join(err, capability.Close())
		}
		contained, err := storagecommit.RestrictRootedFilePermissions(ctx, request)
		if err != nil {
			return err
		}
		if !contained.Expanded() {
			return errors.New("native command did not exercise the containment fixture")
		}

		fresh, err := executioncontext.ObserveExecutionQuiescence(ctx, binding)
		if err != nil {
			return err
		}
		if err := fresh.Require(binding); err != nil {
			return err
		}
		if matches, err := leases.MatchCurrentExclusiveRequestsBounded(ctx, controls, documents, journalrecovery.MaximumPhysicalPathDepth, safety); err != nil || !matches {
			return errors.Join(mutation.StaleSnapshotError{}, err)
		}

		registry, err = safetyStore.LoadProtectionRegistry(ctx)
		if err != nil {
			return err
		}
		if err := registry.RequireProtectedDocument(bound, document); err != nil {
			return err
		}
		clear, err := ownership.NewClearProtection(bound)
		if err != nil {
			return err
		}
		if _, err = safetyStore.TransitionProtection(ctx, fixture.Owner, clear); err != nil {
			return err
		}
		return widened
	})
	if finalized != 1 || !result.Attempt().Succeeded() || result.Attempt().Stdout() != "[malformed" {
		t.Fatalf("lost separate safety and process outcomes: finalized=%d err=%v reason=%s", finalized, err, result.Attempt().Reason())
	}
	if prepaySafety && !errors.Is(err, widened) {
		t.Fatal("prepaid containment did not finish", err)
	}
	if !prepaySafety {
		if err == nil || errors.Is(err, widened) {
			t.Fatal("unavailable safety allowance pretended to settle", err)
		}
		info, statErr := os.Stat(fixture.Documents[0])
		if statErr != nil || info.Mode().Perm() != 0o644 {
			t.Fatal("unadmitted containment changed permissions", statErr)
		}
		rows, loadErr := fixture.Store.LoadPendingProtections(t.Context())
		if loadErr != nil || len(rows) != 1 || !rows[0].Equal(bound) {
			t.Fatal("failed safety work lost the bound fence", loadErr)
		}
		if err := leases.Release(); err != nil {
			t.Fatal(err)
		}
		prepared, err := Plan(t.Context(), PlanInput{ManifestPath: fixture.Paths.ManifestPath})
		if err != nil {
			t.Fatal(err)
		}
		defer prepared.Close()
		recovered, err := Execute(t.Context(), prepared, ExecuteOptions{})
		if err != nil {
			t.Fatal("fresh recovery could not finish retained protection", err)
		}
		outcome, selected := recovered.PermissionResult()
		if !selected || !outcome.Complete() {
			t.Fatal("fresh recovery left the fence unresolved")
		}
	}

	info, err := os.Stat(fixture.Documents[0])
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("capture returned before containment", err)
	}
	rows, err := fixture.Store.LoadPendingProtections(t.Context())
	if err != nil || len(rows) != 0 {
		t.Fatal("finalizer did not confirm settlement", err)
	}
	if payload, err := os.ReadFile(fixture.Documents[0]); err != nil || string(payload) != "malformed host payload\n" {
		t.Fatal("metadata containment changed payload", err)
	}
}
