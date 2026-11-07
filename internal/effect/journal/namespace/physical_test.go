package namespace

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeNamedProfileRulesDoNotChangeWithOwnCreationOrRemoval(t *testing.T) {
	base := t.TempDir()
	codex := filepath.Join(base, "codex")
	manifest := nativeNamespaceRoot(t, base)
	global := nativeNamespaceRoot(t, codex)
	roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: manifest, output.RootCodexHome: global})
	protection := protectionRoots(t, filepath.Join(base, "pi-global"), codex)
	address := nativeNamespaceAddress(t, target.TargetCodex, target.ScopeGlobal, "@codex-home/work.config.toml")
	observe := func() DocumentNamespace {
		t.Helper()
		value, err := Observe(t.Context(), []aggregate.DocumentAddress{address}, roots, protection, journalTestFilesystem(), imageBudget(t))
		if err != nil {
			t.Fatal(err)
		}
		if err := value.RequireCompletePhysicalRules(); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := observe()
	if !slices.ContainsFunc(before.PhysicalRoles(), func(role configuration.DocumentSpec) bool {
		return role.RootRelativePath() == "work.config.toml" && role.Class() == configuration.SharedConfiguration
	}) {
		t.Fatal("a prospective named profile lost its actual shared role")
	}
	path := filepath.Join(codex, "work.config.toml")
	if err := os.WriteFile(path, []byte("theme = 'dark'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	created := observe()
	if err := created.RequirePhysicalRules(before); err != nil {
		t.Fatal("own profile creation invalidated original planning facts", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := observe().RequirePhysicalRules(created); err != nil {
		t.Fatal("own profile removal invalidated original planning facts", err)
	}
}

func TestNativeMixedCaseProfileRetirementUsesOneCanonicalRole(t *testing.T) {
	base := t.TempDir()
	codex := filepath.Join(base, "codex")
	manifest := nativeNamespaceRoot(t, base)
	global := nativeNamespaceRoot(t, codex)
	stored := filepath.Join(codex, "Work.config.toml")
	if err := os.WriteFile(stored, []byte("theme = 'dark'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	upper, err := os.Stat(stored)
	if err != nil {
		t.Fatal(err)
	}
	lower, err := os.Stat(filepath.Join(codex, "work.config.toml"))
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("mixed-case alias requires a case-insensitive test volume")
	}
	if err != nil || !os.SameFile(upper, lower) {
		t.Fatal("mixed-case fixture does not identify one entry", err)
	}
	roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: manifest, output.RootCodexHome: global})
	protection := protectionRoots(t, filepath.Join(base, "pi-global"), codex)
	address := nativeNamespaceAddress(t, target.TargetCodex, target.ScopeGlobal, "@codex-home/work.config.toml")
	observe := func() DocumentNamespace {
		t.Helper()
		value, err := Observe(t.Context(), []aggregate.DocumentAddress{address}, roots, protection, journalTestFilesystem(), imageBudget(t))
		if err != nil {
			t.Fatal(err)
		}
		if len(value.PhysicalRoles()) != 1 {
			t.Fatal("one profile entry acquired spelling-dependent duplicate roles", value.PhysicalRoles())
		}
		return value
	}
	before := observe()
	if err := os.Remove(stored); err != nil {
		t.Fatal(err)
	}
	if err := observe().RequirePhysicalRules(before); err != nil {
		t.Fatal("retiring a mixed-case stored profile changed its rule identity", err)
	}
}

func TestNativeRoleOnlyUnknownBlocksNewClaimsButRetainsRecordedObservation(t *testing.T) {
	base := t.TempDir()
	for _, relative := range []string{".pi", "codex"} {
		if err := os.Mkdir(filepath.Join(base, relative), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	selected := filepath.Join(base, ".pi", "extra.json")
	if err := os.WriteFile(selected, []byte(`{"fixture":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	loop := filepath.Join(base, "codex", "config.toml")
	if err := os.Symlink(loop, loop); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	manifest := nativeNamespaceRoot(t, base)
	roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: manifest})
	protection := protectionRoots(t, filepath.Join(base, "pi-global"), filepath.Join(base, "codex"))
	address := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/extra.json")
	value, err := Observe(t.Context(), []aggregate.DocumentAddress{address}, roots, protection, journalTestFilesystem(), imageBudget(t))
	if err != nil {
		t.Fatal("role-only uncertainty prevented recorded namespace observation", err)
	}
	var failure *ProtectionError
	if err := value.RequireCompletePhysicalRules(); !errors.As(err, &failure) || failure.Kind() != ProtectionUnverified || !failure.documents || failure.Host() != target.TargetCodex {
		t.Fatal("new claim admission discarded the role-only uncertainty", err)
	}
	capability, err := value.AcquireReferent(t.Context(), imageBudget(t))
	if err != nil {
		t.Fatal("stable partial rules prevented recorded referent acquisition", err)
	}
	if err := capability.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(loop); err != nil {
		t.Fatal(err)
	}
	if _, _, err := value.WriterBinding(t.Context(), imageBudget(t)); err == nil {
		t.Fatal("known/unknown role transition was ignored by writer binding")
	}
}

func TestNativePhysicalRolesFollowAnUnselectedHostDocumentLink(t *testing.T) {
	base := t.TempDir()
	manifestPath, pi := filepath.Join(base, "dotfiles"), filepath.Join(base, "pi")
	for _, directory := range []string{filepath.Join(manifestPath, ".codex"), pi} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	selected := filepath.Join(manifestPath, ".codex", "shared.json")
	other := filepath.Join(manifestPath, "other.json")
	for _, path := range []string{selected, other} {
		if err := os.WriteFile(path, []byte(`{"theme":"dark"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	endpoint := filepath.Join(pi, "settings.json")
	if err := os.Symlink(selected, endpoint); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	manifest := nativeNamespaceRoot(t, manifestPath)
	roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: manifest})
	address := nativeNamespaceAddress(t, target.TargetCodex, target.ScopeProject, ".codex/shared.json")
	value, err := Observe(t.Context(), []aggregate.DocumentAddress{address}, roots, protectionRoots(t, pi, filepath.Join(base, "codex")), journalTestFilesystem(), imageBudget(t))
	if err != nil {
		t.Fatal(err)
	}
	reference, _ := configuration.RootReferenceForScope(target.TargetPi, target.ScopeGlobal)
	if !slices.Equal(value.GlobalLocations(), []output.RootReference{reference}) {
		t.Fatal("outbound global settings link lost global membership", value.GlobalLocations())
	}
	roles := value.PhysicalRoles()
	if !slices.ContainsFunc(roles, func(role configuration.DocumentSpec) bool {
		return role.Target() == target.TargetPi && role.Scope() == target.ScopeGlobal && role.RootRelativePath() == "settings.json" && role.Class() == configuration.SharedConfiguration
	}) {
		t.Fatal("shared role was inferred only from the selected alias", roles)
	}
	roles[0] = configuration.DocumentSpec{}
	if value.PhysicalRoles()[0].RootReference().Validate() != nil {
		t.Fatal("physical role facts expose mutable namespace storage")
	}
	if err := os.Remove(endpoint); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, endpoint); err != nil {
		t.Fatal(err)
	}
	current, err := Observe(t.Context(), []aggregate.DocumentAddress{address}, roots, protectionRoots(t, pi, filepath.Join(base, "codex")), journalTestFilesystem(), imageBudget(t))
	if err != nil || !current.Observation().Equal(value.Observation()) || len(current.GlobalLocations()) != 0 {
		t.Fatal("fixture changed the selected referent instead of its external role", err)
	}
	if capability, err := value.AcquireReferent(t.Context(), imageBudget(t)); err == nil {
		_ = capability.Close()
		t.Fatal("changed global role reached referent acquisition")
	}
	if _, _, err := value.WriterBinding(t.Context(), imageBudget(t)); err == nil {
		t.Fatal("changed global role reached writer binding")
	}
}

func TestNativePhysicalAccountEnvelopeDoesNotClaimTheWholeHome(t *testing.T) {
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, ".pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(base, ".pi", "extra.json")
	if err := os.WriteFile(selected, []byte(`{"theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := nativeNamespaceRoot(t, base)
	roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: manifest})
	protection := protectionRoots(t, filepath.Join(base, "pi-global"), filepath.Join(base, "codex-global"))
	contracts, err := configuration.NativeRootContracts()
	if err != nil {
		t.Fatal(err)
	}
	var account output.RootReference
	for _, contract := range contracts {
		if contract.Host() == target.TargetClaudeCode && contract.Envelope() == configuration.NativeEndpoints {
			account = contract.Reference()
		}
	}
	selector, err := NewReservationSelector(account, base)
	if err != nil {
		t.Fatal(err)
	}
	protection.selectors[account] = selector
	address := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/extra.json")
	observe := func() DocumentNamespace {
		t.Helper()
		value, err := Observe(t.Context(), []aggregate.DocumentAddress{address}, roots, protection, journalTestFilesystem(), imageBudget(t))
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	if len(observe().GlobalLocations()) != 0 {
		t.Fatal("account HOME anchor became a global directory envelope")
	}
	if err := os.Symlink(selected, filepath.Join(base, ".claude.json")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	if !slices.Equal(observe().GlobalLocations(), []output.RootReference{account}) {
		t.Fatal("account endpoint's outbound referent lost global membership")
	}
}
