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
	"github.com/isty2e/daem/internal/effect/mutation"
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
	attempt := (subprocess.CommandExecutor{}).ExecuteProtectedInWorkingDirectory(t.Context(), subprocess.CommandAttemptRequest{Command: "/bin/chmod", Args: []string{"644", fixture.Documents[0]}}, bindDirectory, fixture.Protection.Invocation(), func(ctx context.Context, binding executionidentity.Binding) error {
		transition, err := ownership.NewBindProtection(fixture.Protection, binding)
		if err != nil {
			return err
		}
		_, err = fixture.Store.TransitionProtection(ctx, fixture.Owner, transition)
		return err
	})
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
