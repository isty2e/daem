//go:build darwin || linux

package invocation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/protection"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/workflow/protection/recovery"
	"github.com/isty2e/daem/test/ownershiptest"
)

func invocationFixture(t *testing.T, names ...string) (ownershiptest.PermissionRecoveryFixture, InvocationInput, subprocess.WorkingDirectoryBinder) {
	t.Helper()
	if len(names) == 0 {
		names = []string{".codex/config.toml", ".pi/settings.json"}
	}
	fixture := ownershiptest.PermissionRecovery(t, names...)
	clear, err := ownership.NewClearProtection(fixture.Protection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Store.TransitionProtection(t.Context(), fixture.Owner, clear); err != nil {
		t.Fatal(err)
	}
	var requests []mutation.PhysicalAuthorityRequest
	for index, path := range fixture.Documents {
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		target := "codex"
		if index == 1 {
			target = "pi"
		}
		requests = append(requests, mutation.PhysicalAuthorityRequest{Path: path, Target: target, Scope: "project"})
	}
	leases := invocationFixtureLeases(t, fixture.Paths, requests)
	cwd, err := rootedpath.CaptureRoot(fixture.Paths.ManifestRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cwd.Close() })
	bind := func() (subprocess.WorkingDirectoryBinding, error) { return cwd.AcquireWorkingDirectory() }
	return fixture, InvocationInput{Paths: fixture.Paths, Protection: fixture.Protection, Leases: leases, Requests: requests}, bind
}

func invocationFixtureLeases(t *testing.T, paths daempaths.Paths, requests []mutation.PhysicalAuthorityRequest) *mutation.LeaseSet {
	t.Helper()
	var domains []mutation.Domain
	for _, path := range []string{paths.StatefilePath, paths.OwnershipRegistryPath, paths.RecoveryDir} {
		for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
			domain, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{Path: path, Access: mutation.AccessExclusive, Effect: effect})
			if err != nil {
				t.Fatal(err)
			}
			domains = append(domains, domain)
		}
	}
	for _, request := range requests {
		for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
			domain, err := mutation.NewPhysicalPathDomain(mutation.PhysicalPathRequest{Path: request.Path, Target: request.Target, Scope: request.Scope, Access: mutation.AccessExclusive, Effect: effect})
			if err != nil {
				t.Fatal(err)
			}
			domains = append(domains, domain)
		}
	}
	store, err := mutation.NewStore(paths.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	leases, err := store.Acquire(t.Context(), domains...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = leases.Release() })
	return leases
}

func assertInvocationSettled(t *testing.T, fixture ownershiptest.PermissionRecoveryFixture, result InvocationResult) protection.Record {
	t.Helper()
	record, present := result.Protection()
	if !present || record.Settlement() != protection.SettlementCleared {
		t.Fatalf("invocation protection did not settle: present=%t record=%+v", present, record)
	}
	rows, err := fixture.Store.LoadPendingProtections(t.Context())
	if err != nil || len(rows) != 0 {
		t.Fatal("invocation left pending protection", err)
	}
	for _, path := range fixture.Documents {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatal("invocation did not retain its permission ceiling", err)
		}
		if content, err := os.ReadFile(path); err != nil || string(content) != "malformed host payload\n" {
			t.Fatal("permission settlement changed document content", err)
		}
	}
	return record
}

func prepareInvocationFixture(ctx context.Context, input InvocationInput) (InvocationInput, error) {
	budget := physicalwork.NewCaptureBudget(nil)
	prepared, err := PrepareInvocation(ctx, input, budget)
	if err != nil {
		return InvocationInput{}, err
	}
	return prepared.ReserveWork(budget, physicalwork.InvocationWork{})
}

func invokeFixture(ctx context.Context, input InvocationInput, executor subprocess.CommandExecutor, request subprocess.CommandAttemptRequest, bind subprocess.WorkingDirectoryBinder, correlation func(context.Context, *physicalwork.Budget) error) (InvocationResult, error) {
	prepared, err := prepareInvocationFixture(ctx, input)
	if err != nil {
		return InvocationResult{}, err
	}
	return ExecuteInvocation(ctx, prepared, executor, request, bind, correlation)
}

func TestInvocationArmsBindsAndContainsBeforeReturningCapture(t *testing.T) {
	fixture, input, bind := invocationFixture(t)
	sawArmed := false
	result, err := invokeFixture(t.Context(), input, subprocess.CommandExecutor{}, subprocess.CommandAttemptRequest{
		Command: "/bin/sh", Args: []string{"-c", `/bin/chmod 644 "$1" "$2"; printf '[malformed'`, "protected", fixture.Documents[0], fixture.Documents[1]},
	}, bind, func(ctx context.Context, _ *physicalwork.Budget) error {
		rows, err := fixture.Store.LoadPendingProtections(ctx)
		if err == nil && len(rows) == 1 && rows[0].Equal(fixture.Protection) {
			sawArmed = true
			return nil
		}
		return errors.Join(errors.New("correlation preceded Arm"), err)
	})
	if !sawArmed || !errors.Is(err, ErrInvocationPermissionsExpanded) {
		t.Fatal("expansion was hidden or correlation preceded protection", err)
	}
	process, attempted := result.Process()
	if !attempted || !process.ReleaseAttempted() || !process.Attempt().Succeeded() || process.Attempt().Stdout() != "[malformed" {
		t.Fatal("mechanical and safety outcomes were conflated")
	}
	record := assertInvocationSettled(t, fixture, result)
	if record.Phase() != ownership.ProtectionBound || !record.Targets()[0].Expanded() || !record.Targets()[1].Expanded() {
		t.Fatal("actual binding or per-document expansion was lost")
	}
}

func TestInvocationNormalCompletionAndSingleUseReservation(t *testing.T) {
	fixture, input, bind := invocationFixture(t)
	input, err := prepareInvocationFixture(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(fixture.Paths.ManifestRoot, "command-count")
	request := subprocess.CommandAttemptRequest{Command: "/bin/sh", Args: []string{"-c", `printf x >> "$1"`, "protected", marker}}
	result, err := ExecuteInvocation(t.Context(), input, subprocess.CommandExecutor{}, request, bind, nil)
	if err != nil {
		t.Fatal(err)
	}
	if process, present := result.Process(); !present || !process.Attempt().Succeeded() {
		t.Fatal("ordinary protected command did not complete")
	}
	assertInvocationSettled(t, fixture, result)
	if _, err := ExecuteInvocation(t.Context(), input, subprocess.CommandExecutor{}, request, bind, nil); err == nil {
		t.Fatal("reused invocation reservation ran another command")
	}
	if count, err := os.ReadFile(marker); err != nil || string(count) != "x" {
		t.Fatal("reused invocation replayed a command", err)
	}
}

func TestInvocationSettlesArmedProtectionAfterCorrelationFailure(t *testing.T) {
	fixture, input, bind := invocationFixture(t)
	failure := errors.New("correlation publication failed")
	result, err := invokeFixture(t.Context(), input, subprocess.CommandExecutor{}, subprocess.CommandAttemptRequest{Command: "/bin/false"}, bind, func(context.Context, *physicalwork.Budget) error { return failure })
	if !errors.Is(err, failure) {
		t.Fatal("correlation failure was discarded", err)
	}
	if _, attempted := result.Process(); attempted {
		t.Fatal("command execution preceded correlation publication")
	}
	if record := assertInvocationSettled(t, fixture, result); record.Phase() != ownership.ProtectionArmed {
		t.Fatal("unreleased invocation was falsely reported as bound")
	}
}

func TestInvocationCancellationOrForwardExhaustionCannotSpendSafety(t *testing.T) {
	for _, cancelBeforeLaunch := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelBeforeLaunch), func(t *testing.T) {
			fixture, input, bind := invocationFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			marker := filepath.Join(fixture.Paths.ManifestRoot, "must-not-run")
			result, err := invokeFixture(ctx, input, subprocess.CommandExecutor{}, subprocess.CommandAttemptRequest{Command: "/usr/bin/touch", Args: []string{marker}}, bind, func(_ context.Context, forward *physicalwork.Budget) error {
				if cancelBeforeLaunch {
					cancel()
					return nil
				}
				return forward.AdmitPhysicalWork(0, forward.RemainingEntries(), forward.RemainingBytes())
			})
			if err != nil {
				t.Fatal("prepaid safety was lost with forward execution", err)
			}
			if process, present := result.Process(); !present || process.ReleaseAttempted() || process.Attempt().Succeeded() {
				t.Fatal("failed forward execution released the target")
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("unreleased target command ran", err)
			}
			assertInvocationSettled(t, fixture, result)
		})
	}
}

func TestInvocationRefusesStaleModeOrIncompleteCoverageBeforeArm(t *testing.T) {
	for _, staleMode := range []bool{false, true} {
		t.Run(fmt.Sprint(staleMode), func(t *testing.T) {
			fixture, input, bind := invocationFixture(t)
			if staleMode {
				if err := os.Chmod(fixture.Documents[0], 0o640); err != nil {
					t.Fatal(err)
				}
			} else {
				input.Requests = input.Requests[:1]
			}
			result, err := invokeFixture(t.Context(), input, subprocess.CommandExecutor{}, subprocess.CommandAttemptRequest{Command: "/bin/false"}, bind, nil)
			if err == nil {
				t.Fatal("stale invocation input was admitted")
			}
			if _, attempted := result.Process(); attempted {
				t.Fatal("stale invocation input reached process execution")
			}
			if _, armed := result.Protection(); armed {
				t.Fatal("stale input reached protection publication")
			}
			if rows, err := fixture.Store.LoadPendingProtections(t.Context()); err != nil || len(rows) != 0 {
				t.Fatal("pre-Arm refusal left a new fence", err)
			}
		})
	}
}

func TestInvocationCannotReplayOrClearAnAlreadyPendingIdentity(t *testing.T) {
	fixture, input, bind := invocationFixture(t)
	arm, err := ownership.NewArmProtection(fixture.Protection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Store.TransitionProtection(t.Context(), fixture.Owner, arm); err != nil {
		t.Fatal(err)
	}
	result, err := invokeFixture(t.Context(), input, subprocess.CommandExecutor{}, subprocess.CommandAttemptRequest{Command: "/bin/true"}, bind, nil)
	if err == nil {
		t.Fatal("pending identity was admitted as a fresh invocation")
	}
	if _, attempted := result.Process(); attempted {
		t.Fatal("pending invocation was replayed")
	}
	if rows, err := fixture.Store.LoadPendingProtections(t.Context()); err != nil || len(rows) != 1 || !rows[0].Equal(fixture.Protection) {
		t.Fatal("refused replay modified an existing obligation", err)
	}
}

func TestInvocationUnpublishedArmHasNoReportedObligation(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires an unprivileged owner for publication refusal")
	}
	fixture, input, bind := invocationFixture(t)
	parent := filepath.Dir(fixture.Store.Path())
	info, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, info.Mode().Perm()) })
	result, err := invokeFixture(t.Context(), input, subprocess.CommandExecutor{}, subprocess.CommandAttemptRequest{Command: "/bin/true"}, bind, nil)
	if !errors.Is(err, os.ErrPermission) {
		t.Fatal("fixture did not refuse protection publication", err)
	}
	if _, attempted := result.Process(); attempted {
		t.Fatal("failed Arm reached process execution")
	}
	if record, present := result.Protection(); present {
		t.Fatalf("confirmed unpublished Arm reported an obligation: %+v", record)
	}
	if rows, err := fixture.Store.LoadPendingProtections(t.Context()); err != nil || len(rows) != 0 {
		t.Fatal("publication refusal unexpectedly persisted a protection", err)
	}
}

func TestInvocationUnreadableProtectionIsNotReportedAsUnpublished(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires an unprivileged owner for read refusal")
	}
	fixture, input, bind := invocationFixture(t)
	t.Cleanup(func() { _ = os.Chmod(fixture.Store.Path(), 0o600) })
	result, err := invokeFixture(t.Context(), input, subprocess.CommandExecutor{}, subprocess.CommandAttemptRequest{Command: "/bin/true"}, bind, func(context.Context, *physicalwork.Budget) error {
		return os.Chmod(fixture.Store.Path(), 0)
	})
	record, present := result.Protection()
	if !errors.Is(err, os.ErrPermission) || !present || record.Settlement() != protection.SettlementUnknown {
		t.Fatal("unreadable published protection was reported as absent", err)
	}
	if process, attempted := result.Process(); !attempted || process.ReleaseAttempted() {
		t.Fatal("unqualified binding publication released its command")
	}
	if err := os.Chmod(fixture.Store.Path(), 0o600); err != nil {
		t.Fatal(err)
	}
	if rows, err := fixture.Store.LoadPendingProtections(t.Context()); err != nil || len(rows) != 1 || !rows[0].Equal(fixture.Protection) {
		t.Fatal("read refusal lost the persisted obligation", err)
	}
}

func TestInvocationSharedCohortUsesEachOwnersRootAndRetainsPartialSafety(t *testing.T) {
	for _, invalidateForeign := range []bool{false, true} {
		t.Run(fmt.Sprint(invalidateForeign), func(t *testing.T) {
			fixture, input, bind := invocationFixture(t, "sub/.codex/config.toml", ".pi/settings.json")
			foreign, err := daempaths.Resolve(filepath.Join(fixture.Paths.ManifestRoot, "sub", "daem.toml"))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(foreign.StateDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(foreign.StatefilePath, []byte("foreign state"), 0o600); err != nil {
				t.Fatal(err)
			}
			observed, err := mutation.ObservePersistedDirectoryEntryAuthority(foreign.StatefilePath)
			if err != nil {
				t.Fatal(err)
			}
			owner, err := stateauthority.New(observed.Exact(), foreign.ManifestPath)
			if err != nil || owner.Equal(fixture.Owner) {
				t.Fatal("fixture did not establish a separate cohort owner", err)
			}
			sharedDestination, _ := output.Parse("sub/.codex/config.toml")
			foreignDestination, _ := output.Parse(".codex/config.toml")
			documents := fixture.Protection.Documents()
			var shared ownership.DocumentBinding
			for index, document := range documents {
				if document.Bindings()[0].Destination() != sharedDestination {
					continue
				}
				shared, err = ownership.NewDocumentBinding(owner, foreignDestination, document.PathAuthority())
				if err != nil {
					t.Fatal(err)
				}
				documents[index], err = ownership.NewProtectedDocument(append(document.Bindings(), shared), document.UserID(), document.GroupID(), document.ModeCap())
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := shared.Validate(); err != nil {
				t.Fatal("shared cohort was not constructed", err)
			}
			current, err := fixture.Store.LoadProtectionRegistry(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			registry, err := ownership.NewRegistry(current.Claims(), append(current.DocumentBindings(), shared)...)
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
			fixture.Protection, err = ownership.NewArmedProtection(fixture.Owner, fixture.Protection.Invocation(), fixture.Protection.Correlation(), documents)
			if err != nil {
				t.Fatal(err)
			}
			input.Protection = fixture.Protection
			moved := foreign.StateDir + ".moved"
			redirected := filepath.Join(fixture.Paths.ManifestRoot, "redirected-state")
			if err := os.MkdirAll(redirected, 0o700); err != nil {
				t.Fatal(err)
			}
			script := `/bin/chmod 644 "$1" "$2"`
			if invalidateForeign {
				script += `; /bin/mv "$3" "$4"; /bin/ln -s "$5" "$3"`
			}
			result, err := invokeFixture(t.Context(), input, subprocess.CommandExecutor{}, subprocess.CommandAttemptRequest{
				Command: "/bin/sh", Args: []string{"-c", script, "protected", fixture.Documents[0], fixture.Documents[1], foreign.StateDir, moved, redirected},
			}, bind, nil)
			if process, present := result.Process(); !present || !process.Attempt().Succeeded() {
				t.Fatal("qualified shared cohort did not reach its command", err)
			}
			if !invalidateForeign {
				if !errors.Is(err, ErrInvocationPermissionsExpanded) {
					t.Fatal("shared cohort expansion was hidden", err)
				}
				assertInvocationSettled(t, fixture, result)
				return
			}
			record, present := result.Protection()
			if err == nil || !present || record.Settlement() != protection.SettlementPending {
				t.Fatal("unqualified foreign context lost its fence", err)
			}
			for index, path := range fixture.Documents {
				want := os.FileMode(0o600)
				if index == 0 {
					want = 0o644
				}
				if info, err := os.Stat(path); err != nil || info.Mode().Perm() != want {
					t.Fatal("partial safety did not distinguish qualified and blocked targets", err)
				}
			}
			if err := os.Remove(foreign.StateDir); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(moved, foreign.StateDir); err != nil {
				t.Fatal(err)
			}
			if err := input.Leases.Release(); err != nil {
				t.Fatal(err)
			}
			prepared, selected, err := recovery.Prepare(t.Context(), fixture.Paths, physicalwork.NewCaptureBudget(nil))
			if err != nil || !selected {
				t.Fatal("fresh recovery could not discover the retained fence", err)
			}
			if recovered, err := recovery.Execute(t.Context(), prepared); err != nil || !recovered.Complete() {
				t.Fatal("fresh recovery could not finish remaining safety", err)
			}
			if rows, err := fixture.Store.LoadPendingProtections(t.Context()); err != nil || len(rows) != 0 {
				t.Fatal("fresh recovery left pending protection", err)
			}
		})
	}
}
