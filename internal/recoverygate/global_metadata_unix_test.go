//go:build darwin || linux

package recoverygate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/operationplan"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/testkit/metadatatx"
)

func TestGlobalMetadataFenceBlocksForeignAndReservedExecution(t *testing.T) {
	root := globalMetadataTestRoot(t)
	paths := daempaths.Paths{DataDir: filepath.Join(root, "data"), StateDir: filepath.Join(root, "peer", ".daem")}
	paths.RecoveryDir = filepath.Join(paths.StateDir, "recovery")
	authority, err := NewEffectAuthority(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := authority.reservePlan(forwardEffectPlan{BarrierValidationCalls: 1, EnsureCalls: 1})
	if err != nil {
		t.Fatal(err)
	}
	var builder operationplan.EffectStructureBuilder
	structure, err := builder.Compile(builder.Step("barrier", operationplan.EffectStepValidateBarrier))
	if err != nil {
		t.Fatal(err)
	}
	execution, err := authority.ReserveForwardEffectExecution(structure, "")
	if err != nil {
		t.Fatal(err)
	}
	recoverTransaction := leaveGlobalMetadataTransaction(t, paths)
	for _, test := range []struct {
		name string
		run  func() error
	}{
		{name: "observe", run: func() error { return Observe(t.Context(), paths) }},
		{name: "bounded observe", run: func() error { return ObserveBounded(t.Context(), paths, physicalwork.NewCaptureBudget(nil)) }},
		{name: "file set planning", run: func() error { return RequireFileSetClear(t.Context(), paths) }},
		{name: "effect planning", run: func() error { _, err := NewEffectAuthority(t.Context(), paths); return err }},
		{name: "leased validation", run: func() error { return authority.Validate(t.Context()) }},
		{name: "local file set recovery", run: func() error { return authority.ValidateFileSetRecovery(t.Context()) }},
		{name: "reserved validation", run: func() error { return reserved.Validate(t.Context()) }},
		{name: "structural validation", run: func() error { return execution.ValidateBarrier(t.Context(), "barrier") }},
		{name: "reserved creation", run: func() error {
			created, err := reserved.EnsureStateDirForEffect(t.Context(), func(context.Context) error { return nil })
			if created {
				return errors.New("peer StateDir was created through a global fence")
			}
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := test.run()
			if !errors.Is(err, ErrGlobalMetadataFence) {
				t.Fatal("peer did not preserve the global transaction fence", err)
			}
			if StateOf(err).Observed() || StateOf(err).HasContinuingFileSetFence() {
				t.Fatal("global evidence was projected as recoverable local evidence")
			}
		})
	}
	independent := paths
	independent.DataDir = filepath.Join(root, "other-data")
	if err := Observe(t.Context(), independent); err != nil {
		t.Fatal("another DataDir was blocked", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Observe(ctx, paths); !errors.Is(err, context.Canceled) {
		t.Fatal("global transaction hid cancellation", err)
	}
	recoverTransaction()
	if err := Observe(t.Context(), paths); err != nil {
		t.Fatal("global recovery did not unblock its peer", err)
	}
}

func TestGlobalMetadataRefusalPreservesScopeThroughLegacyUserSelection(t *testing.T) {
	root := globalMetadataTestRoot(t)
	paths := daempaths.Paths{
		DataDir:            filepath.Join(root, "data"),
		ManifestPath:       filepath.Join(root, "config", "daem.toml"),
		StateDir:           filepath.Join(root, "state"),
		StatefilePath:      filepath.Join(root, "state", "state.json"),
		RecoveryDir:        filepath.Join(root, "state", "recovery"),
		LegacyUserStateDir: filepath.Join(root, "legacy"),
	}
	legacy, err := paths.LegacyUserState()
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewEffectAuthority(t.Context(), legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(legacy.StateDir, ".daem-tmp-local"), 0o700); err != nil {
		t.Fatal(err)
	}
	recoverTransaction := leaveGlobalMetadataTransaction(t, paths)
	for _, test := range []struct {
		name string
		run  func() error
	}{
		{name: "user selection", run: func() error { return RequireUserStateMigrated(t.Context(), paths) }},
		{name: "observation", run: func() error { return Observe(t.Context(), paths) }},
		{name: "file set planning", run: func() error { return RequireFileSetClear(t.Context(), paths) }},
		{name: "effect planning", run: func() error { _, err := NewEffectAuthority(t.Context(), paths); return err }},
		{name: "relocation source revalidation", run: func() error {
			if err := source.Validate(t.Context()); err != nil {
				return LegacyUserStateRecoveryError{StateDir: legacy.StateDir, Err: err}
			}
			return nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := test.run()
			if !IsGlobalMetadataRefusal(err) || StateOf(err).Observed() {
				t.Fatal("global refusal became a local recovery result", err)
			}
			var legacyError LegacyUserStateRecoveryError
			if errors.As(err, &legacyError) && legacyError.Error() != legacyError.Err.Error() {
				t.Fatal("global refusal was reinterpreted as legacy-user recovery", err)
			}
		})
	}
	recoverTransaction()
	err = RequireUserStateMigrated(t.Context(), paths)
	var local LegacyUserStateRecoveryError
	if !errors.As(err, &local) || local.StateDir != legacy.StateDir || !errors.Is(err, fileset.ErrAbandonedFileSetResidue) {
		t.Fatal("local legacy recovery was lost after the global transaction cleared", err)
	}
}

func TestLegacyUserRecoveryDisplayPreservesCancellation(t *testing.T) {
	for _, test := range []struct {
		name  string
		cause error
	}{{name: "canceled", cause: context.Canceled}, {name: "deadline", cause: context.DeadlineExceeded}} {
		t.Run(test.name, func(t *testing.T) {
			err := LegacyUserStateRecoveryError{StateDir: t.TempDir(), Err: test.cause}
			if !IsCancellation(err) || err.Error() != test.cause.Error() {
				t.Fatal("cancellation was reinterpreted as a legacy recovery request", err)
			}
		})
	}
}

func TestGlobalMetadataReservationAndValidationRetainTheSelectedRoot(t *testing.T) {
	root := globalMetadataTestRoot(t)
	physical := filepath.Join(root, "physical", "nested", "data")
	other := filepath.Join(root, "other")
	for _, directory := range []string{physical, other} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(physical, alias); err != nil {
		t.Fatal(err)
	}
	paths := daempaths.Paths{DataDir: alias, StateDir: filepath.Join(root, ".daem"), RecoveryDir: filepath.Join(root, ".daem", "recovery")}
	authority, err := NewEffectAuthority(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	physicalPaths := paths
	physicalPaths.DataDir = physical
	physicalAuthority, err := NewEffectAuthority(t.Context(), physicalPaths)
	if err != nil {
		t.Fatal(err)
	}
	plan := forwardEffectPlan{BarrierValidationCalls: 1}
	aliasWork, err := authority.lowerForwardPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	physicalWork, err := physicalAuthority.lowerForwardPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	if aliasWork.planned.stateWork != physicalWork.planned.stateWork || aliasWork.planned.totalWork != physicalWork.planned.totalWork {
		t.Fatalf("one physical namespace received spelling-dependent reservations: alias=%+v physical=%+v", aliasWork.work, physicalWork.work)
	}
	forward, err := authority.ReserveForwardEffects(operationplan.NewDemand(1, 1, 0, "", 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	var builder operationplan.EffectStructureBuilder
	structure, err := builder.Compile(builder.Step("barrier", operationplan.EffectStepValidateBarrier))
	if err != nil {
		t.Fatal(err)
	}
	execution, err := authority.ReserveForwardEffectExecution(structure, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := execution.Close(); err != nil {
			t.Error(err)
		}
	})
	before, err := authority.IdentityFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, alias); err != nil {
		t.Fatal(err)
	}
	fresh, err := NewEffectAuthority(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	after, err := fresh.IdentityFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if authority.Equal(fresh) || before == after {
		t.Fatal("a changed metadata selection retained the old authority identity")
	}
	metadatatx.WriteInterrupted(t, physicalPaths.GlobalMetadataDir())
	for name, validate := range map[string]func(context.Context) error{
		"captured": authority.Validate,
		"reserved": forward.Validate,
		"cursor": func(ctx context.Context) error {
			return execution.ValidateBarrier(ctx, "barrier")
		},
		"ensure": func(ctx context.Context) error {
			created, err := forward.EnsureStateDirForEffect(ctx, func(context.Context) error { return nil })
			if created {
				t.Fatal("a blocked authority created the selected StateDir")
			}
			return err
		},
	} {
		if err := validate(t.Context()); !errors.Is(err, ErrGlobalMetadataFence) || !errors.Is(err, fileset.ErrInterruptedFileSetTransaction) {
			t.Fatalf("%s validation followed a retargeted alias rather than its lease selection: %v", name, err)
		}
	}
}

func TestGlobalMetadataAuthoritySharesPublisherLease(t *testing.T) {
	for _, test := range []struct {
		name  string
		alias bool
	}{{name: "physical root"}, {name: "ancestor symlink", alias: true}} {
		t.Run(test.name, func(t *testing.T) {
			root := globalMetadataTestRoot(t)
			dataRoot := root
			if test.alias {
				dataRoot = filepath.Join(root, "alias")
				if err := os.Symlink(root, dataRoot); err != nil {
					t.Fatal(err)
				}
			}
			paths := daempaths.Paths{DataDir: filepath.Join(dataRoot, "data"), StateDir: filepath.Join(root, ".daem"), RecoveryDir: filepath.Join(root, ".daem", "recovery")}
			authority, err := NewEffectAuthority(t.Context(), paths)
			if err != nil {
				t.Fatal(err)
			}
			revisions, err := mutation.CaptureRevisionSet(t.Context(), authority.RevisionRequests()...)
			if err != nil {
				t.Fatal(err)
			}
			store, err := mutation.NewStore(paths.DataDir)
			if err != nil {
				t.Fatal(err)
			}
			firstUse, err := store.Acquire(t.Context(), authority.Domains()...)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = firstUse.Release() })
			if matches, err := firstUse.DomainsMatchCurrent(t.Context()); err != nil || !matches {
				t.Fatal("lease-store creation invalidated the captured authority", err)
			}
			if matches, err := revisions.MatchesCurrent(t.Context()); err != nil || !matches {
				t.Fatal("first-use revisions are not current", err)
			}
			if err := firstUse.Release(); err != nil {
				t.Fatal(err)
			}
			publisherPaths, err := paths.WithDataDir(store.DataDir())
			if err != nil {
				t.Fatal(err)
			}
			marker, err := GlobalMetadataAuthorityPath(publisherPaths)
			if err != nil {
				t.Fatal(err)
			}
			writer, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{Path: marker, Access: mutation.AccessExclusive, Effect: mutation.PathEffectDirectoryEntry})
			if err != nil {
				t.Fatal(err)
			}
			lease, err := store.Acquire(t.Context(), writer)
			if err != nil {
				t.Fatal(err)
			}
			defer lease.Release()
			ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
			defer cancel()
			peer, err := store.Acquire(ctx, authority.Domains()...)
			if err == nil {
				_ = peer.Release()
				t.Fatal("peer bypassed the global publisher lease")
			}
			var canceled mutation.CancellationError
			if !errors.As(err, &canceled) {
				t.Fatal("peer lease failed for an unrelated reason", err)
			}
			metadatatx.WriteInterrupted(t, publisherPaths.GlobalMetadataDir())
			if err := Observe(t.Context(), paths); !errors.Is(err, ErrGlobalMetadataFence) ||
				!errors.Is(err, fileset.ErrInterruptedFileSetTransaction) || errors.Is(err, fileset.ErrFileSetEvidenceInvalid) {
				t.Fatal("census spelling changed the valid interrupted transaction classification", err)
			}
		})
	}
}

func TestGlobalMetadataFenceDoesNotConflateSelectedStateDir(t *testing.T) {
	root := globalMetadataTestRoot(t)
	paths := daempaths.Paths{DataDir: root, StateDir: root, RecoveryDir: filepath.Join(root, "recovery")}
	local, err := fileset.FileSetAuthorityPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(local, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := RequireGlobalMetadataClear(t.Context(), paths); err != nil {
		t.Fatal("selected-only evidence became global", err)
	}
	if err := Observe(t.Context(), paths); err == nil || errors.Is(err, ErrGlobalMetadataFence) {
		t.Fatal("selected fence was lost or misclassified", err)
	}
}

func TestGlobalMetadataObservationIsReadOnlyAndPreservesInvalidEvidence(t *testing.T) {
	root := globalMetadataTestRoot(t)
	paths := daempaths.Paths{DataDir: filepath.Join(root, "data"), StateDir: filepath.Join(root, ".daem"), RecoveryDir: filepath.Join(root, ".daem", "recovery")}
	if err := Observe(t.Context(), paths); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(paths.DataDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("clear observation created shared metadata", err)
	}
	marker, err := GlobalMetadataAuthorityPath(paths)
	if err != nil {
		t.Fatal(err)
	}
	residue := filepath.Join(paths.GlobalMetadataDir(), ".daem-tmp-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := os.MkdirAll(residue, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Observe(t.Context(), paths); !errors.Is(err, ErrGlobalMetadataFence) || !errors.Is(err, fileset.ErrAbandonedFileSetResidue) {
		t.Fatal("global residue was not retained as a blocking cause", err)
	}
	if err := os.Remove(residue); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(marker, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(marker, "transaction.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Observe(t.Context(), paths); !errors.Is(err, ErrGlobalMetadataFence) || !errors.Is(err, fileset.ErrFileSetEvidenceInvalid) {
		t.Fatal("invalid global evidence was not retained as a blocking cause", err)
	}
}

func leaveGlobalMetadataTransaction(t *testing.T, paths daempaths.Paths) func() {
	t.Helper()
	directory := globalMetadataTestRoot(t)
	targets := []string{filepath.Join(directory, "a-state.json"), filepath.Join(directory, "b-state.json")}
	marker, err := GlobalMetadataAuthorityPath(paths)
	if err != nil {
		t.Fatal(err)
	}
	store, err := mutation.NewStore(paths.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	var domains []mutation.Domain
	for _, path := range append(append([]string(nil), targets...), marker) {
		for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
			domain, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{Path: path, Access: mutation.AccessExclusive, Effect: effect})
			if err != nil {
				t.Fatal(err)
			}
			domains = append(domains, domain)
		}
	}
	lease, err := store.Acquire(t.Context(), domains...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lease.Release() })
	metadatatx.WritePrefix(t, paths.GlobalMetadataDir(), []metadatatx.Write{
		{Path: targets[0], After: []byte("after")},
		{Path: targets[1], After: []byte("after")},
	}, 1)
	fenceErr := fileset.ObserveClearFenceAt(t.Context(), paths.GlobalMetadataDir(), defaultStateDirMaximumPhysicalDepth, &stateDirOperationWorkBudget{})
	if fileset.FileSetFenceKindOf(fenceErr) != fileset.FileSetFencePublishedTransaction {
		t.Fatal("fixture did not retain real published transaction evidence", fenceErr)
	}
	return func() {
		t.Helper()
		if err := fileset.RecoverFileSet(t.Context(), paths.GlobalMetadataDir(), targets); err != nil {
			t.Fatal(err)
		}
		for _, path := range targets {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("global recovery did not restore the before-state", err)
			}
		}
	}
}

func globalMetadataTestRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}
