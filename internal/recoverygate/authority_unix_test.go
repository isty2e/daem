//go:build darwin || linux

package recoverygate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	daempaths "github.com/isty2e/daem/internal/paths"
)

func TestAbsentStateDirBarrierUsesTransferredCensusAuthority(t *testing.T) {
	censusErr := errors.New("injected retained census failure")
	stateDir := &absentBarrierAuthority{requireClearErr: censusErr}
	err := validateBarrier(t.Context(), daempaths.Paths{
		StateDir:    filepath.Join(t.TempDir(), ".daem"),
		RecoveryDir: filepath.Join(t.TempDir(), "recovery"),
	}, stateDir)
	if !errors.Is(err, censusErr) {
		t.Fatalf("validateBarrier error = %v, want retained census failure", err)
	}
	if stateDir.requireClearCalls != 1 {
		t.Fatalf("retained census calls = %d, want 1", stateDir.requireClearCalls)
	}
}

func TestAbsentStateDirForwardWorkMatchesBarrierAndEnsurePasses(t *testing.T) {
	tests := []struct {
		name        string
		plan        ForwardEffectPlan
		validations int
		create      bool
	}{
		{name: "barrier", plan: ForwardEffectPlan{BarrierValidationCalls: 1}, validations: 3},
		{name: "first ensure", plan: ForwardEffectPlan{EnsureCalls: 1}, validations: 6, create: true},
		{name: "later ensure", plan: ForwardEffectPlan{EnsureCalls: 2}, validations: 13, create: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			validations, create, err := forwardStateDirValidationPlan(false, test.plan)
			if err != nil {
				t.Fatal(err)
			}
			if validations != test.validations || create != test.create {
				t.Fatalf("forward plan = (%d, %t), want (%d, %t)", validations, create, test.validations, test.create)
			}
		})
	}
}

func TestForwardEffectReservationRejectsCensusWorkBeforeProviderEffect(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, ".daem")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	paths := daempaths.Paths{
		StateDir:    stateDir,
		RecoveryDir: filepath.Join(stateDir, "recovery"),
	}
	budget := &forwardEffectPathBudget{
		limit:      524_288,
		entryLimit: 0,
		byteLimit:  16 << 30,
	}
	stateAuthority, err := transaction.CaptureStateDirAuthorityBounded(
		t.Context(),
		stateDir,
		256,
		budget,
	)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := NewEffectAuthority(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	authority.stateDir = stateAuthority
	providerEffects := 0
	_, reserveErr := authority.ReserveForwardEffects(ForwardEffectPlan{
		BarrierValidationCalls: 1,
	})
	if reserveErr == nil {
		providerEffects++
	}
	if !errors.Is(reserveErr, transaction.ErrFileSetAccessUnprovable) {
		t.Fatalf("ReserveForwardEffects error = %v, want census budget refusal", reserveErr)
	}
	if providerEffects != 0 {
		t.Fatalf("provider effects = %d, want none before census reservation", providerEffects)
	}
}

func TestForwardEffectReservationRejectsHighCardinalityBeforeProviderEffect(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, ".daem")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	paths := daempaths.Paths{
		StateDir:    stateDir,
		RecoveryDir: filepath.Join(stateDir, "recovery"),
	}
	budget := &forwardEffectPathBudget{
		limit:      524_288,
		entryLimit: 400_000,
		byteLimit:  16 << 30,
	}
	stateAuthority, err := transaction.CaptureStateDirAuthorityBounded(
		t.Context(),
		stateDir,
		256,
		budget,
	)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := NewEffectAuthority(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	authority.stateDir = stateAuthority
	providerEffects := 0
	run := func() error {
		_, reserveErr := authority.ReserveForwardEffects(ForwardEffectPlan{
			EnsureCalls:             2,
			BarrierValidationCalls:  3,
			StateDirValidationCalls: 20_000,
			DescendantPath:          filepath.Join(stateDir, "state.json"),
			DescendantValidations:   20_000,
			DescendantFileCommits:   5_000,
		})
		if reserveErr != nil {
			return reserveErr
		}
		providerEffects++
		return nil
	}
	err = run()
	if !errors.Is(err, transaction.ErrFileSetAccessUnprovable) {
		t.Fatalf("ReserveForwardEffects error = %v, want operation budget refusal", err)
	}
	if providerEffects != 0 {
		t.Fatalf("provider effects = %d, want none before complete reservation", providerEffects)
	}
	if _, statErr := os.Lstat(filepath.Join(stateDir, "state.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("statefile exists after failed pre-effect reservation: %v", statErr)
	}
}

func TestEffectAuthorityRejectsStateDirDirectoryReplacement(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, ".daem")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	paths := daempaths.Paths{
		StateDir:    stateDir,
		RecoveryDir: filepath.Join(stateDir, "recovery"),
	}
	authority, err := NewEffectAuthority(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	retained := stateDir + "-retained"
	if err := os.Rename(stateDir, retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := authority.Validate(t.Context()); !errors.Is(err, transaction.ErrFileSetAccessUnprovable) {
		t.Fatalf("Validate error = %v, want StateDir identity refusal", err)
	}
}

type absentBarrierAuthority struct {
	requireClearErr   error
	requireClearCalls int
}

func (*absentBarrierAuthority) PresentAtCapture() bool { return false }
func (*absentBarrierAuthority) Validate(context.Context) error {
	return nil
}

func (authority *absentBarrierAuthority) RequireClear(context.Context) error {
	authority.requireClearCalls++
	return authority.requireClearErr
}

func (*absentBarrierAuthority) EnsureOwnedIncarnation(context.Context) (bool, error) {
	return false, nil
}

type forwardEffectPathBudget struct {
	used       int
	limit      int
	entries    int
	entryLimit int
	bytes      int64
	byteLimit  int64
}

func (budget *forwardEffectPathBudget) AdmitPathComponents(count int) error {
	return budget.AdmitPhysicalWork(count, 0, 0)
}

func (budget *forwardEffectPathBudget) AdmitPhysicalWork(
	pathComponents int,
	entries int,
	bytes int64,
) error {
	if pathComponents < 0 || pathComponents > budget.limit-budget.used {
		return errors.New("injected forward effect path budget exhausted")
	}
	if entries < 0 || entries > budget.entryLimit-budget.entries {
		return errors.New("injected forward effect entry budget exhausted")
	}
	if bytes < 0 || bytes > budget.byteLimit-budget.bytes {
		return errors.New("injected forward effect byte budget exhausted")
	}
	budget.used += pathComponents
	budget.entries += entries
	budget.bytes += bytes
	return nil
}

var _ rootedpath.PhysicalTraversalBudget = (*forwardEffectPathBudget)(nil)

func TestEffectAuthorityCreatesStateDirBetweenPeerAuthorityValidations(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, ".daem")
	authority, err := NewEffectAuthority(t.Context(), daempaths.Paths{
		StateDir:    stateDir,
		RecoveryDir: filepath.Join(stateDir, "recovery"),
	})
	if err != nil {
		t.Fatal(err)
	}

	validations := 0
	created, err := authority.EnsureStateDirForEffect(
		t.Context(),
		func(context.Context) error {
			validations++
			_, statErr := os.Stat(stateDir)
			if validations == 1 && !os.IsNotExist(statErr) {
				t.Fatalf("StateDir existed during pre-effect validation: %v", statErr)
			}
			if validations == 2 && statErr != nil {
				t.Fatalf("StateDir missing during post-effect validation: %v", statErr)
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("EnsureStateDirForEffect: %v", err)
	}
	if !created || validations != 2 {
		t.Fatalf("created, validations = %t, %d; want true, 2", created, validations)
	}
}

func TestEffectAuthorityDoesNotCreateStateDirAfterPeerValidationFailure(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, ".daem")
	authority, err := NewEffectAuthority(t.Context(), daempaths.Paths{
		StateDir:    stateDir,
		RecoveryDir: filepath.Join(stateDir, "recovery"),
	})
	if err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("peer authority changed")

	created, err := authority.EnsureStateDirForEffect(
		t.Context(),
		func(context.Context) error { return wantErr },
	)
	if !errors.Is(err, wantErr) || created {
		t.Fatalf("EnsureStateDirForEffect = %t, %v; want false, peer error", created, err)
	}
	if _, statErr := os.Stat(stateDir); !os.IsNotExist(statErr) {
		t.Fatalf("StateDir stat error = %v, want absent", statErr)
	}
}

func TestEffectAuthorityCreatesAndBindsFirstStateDirIncarnation(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, ".daem")
	paths := daempaths.Paths{
		StateDir:    stateDir,
		RecoveryDir: filepath.Join(stateDir, "recovery"),
	}
	authority, err := NewEffectAuthority(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.Validate(t.Context()); err != nil {
		t.Fatalf("Validate absent StateDir: %v", err)
	}
	created, err := authority.EnsureStateDirForEffect(
		t.Context(),
		func(context.Context) error { return nil },
	)
	if err != nil {
		t.Fatalf("EnsureStateDirForEffect: %v", err)
	}
	if !created {
		t.Fatal("EnsureStateDirForEffect did not report the created directory")
	}
	if err := authority.Validate(t.Context()); err != nil {
		t.Fatalf("Validate accepted StateDir: %v", err)
	}

	retained := stateDir + "-retained"
	if err := os.Rename(stateDir, retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := authority.Validate(t.Context()); !errors.Is(err, transaction.ErrFileSetAccessUnprovable) {
		t.Fatalf("Validate replacement error = %v, want StateDir identity refusal", err)
	}
}
