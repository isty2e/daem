//go:build darwin || linux

package execute

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestCaptureBoundGlobalManagedPathPreconditionRejectsFinalSymlink(t *testing.T) {
	root := t.TempDir()
	referent := filepath.Join(root, "referent")
	if err := os.Mkdir(referent, 0o700); err != nil {
		t.Fatalf("create referent: %v", err)
	}
	path := filepath.Join(root, "managed")
	if err := os.Symlink(referent, path); err != nil {
		t.Fatalf("create final symlink: %v", err)
	}

	_, err := captureBoundGlobalManagedPathPrecondition(
		t,
		t.Context(),
		path,
		true,
		testArtifactHash("untrusted-referent"),
	)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "symlink") {
		t.Fatalf("capture final symlink error = %v, want symlink rejection", err)
	}
}

func TestCaptureBoundGlobalManagedPathPreconditionRejectsTypeSubstitution(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed")
	if err := os.WriteFile(path, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write substituted file: %v", err)
	}

	_, err := captureBoundGlobalManagedPathPrecondition(
		t,
		t.Context(),
		path,
		true,
		testArtifactHash("untrusted-file"),
	)
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("capture substituted file error = %v, want directory-kind rejection", err)
	}
}

func TestCaptureBoundGlobalManagedPathPreconditionRejectsDestinationAppearingAfterObservation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("create racing destination: %v", err)
	}

	_, err := captureBoundGlobalManagedPathPrecondition(
		t,
		t.Context(),
		path,
		false,
		"",
	)
	if err == nil || !strings.Contains(err.Error(), "appeared after planning") {
		t.Fatalf("capture appeared destination error = %v, want race rejection", err)
	}
}

func TestCaptureBoundGlobalManagedPathPreconditionHonorsCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("create managed directory: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := captureBoundGlobalManagedPathPrecondition(
		t,
		ctx,
		path,
		true,
		testArtifactHash("untrusted-directory"),
	)
	if err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("capture canceled context error = %v, want cancellation", err)
	}
}

func TestManagedPathAuthorityRetainsBoundGlobalPathAfterAncestorSymlinkRetarget(t *testing.T) {
	root := t.TempDir()
	admittedHome := filepath.Join(root, "admitted")
	retargetedHome := filepath.Join(root, "retargeted")
	for _, directory := range []string{admittedHome, retargetedHome} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatalf("create home fixture: %v", err)
		}
	}
	homeAlias := filepath.Join(root, "home")
	if err := os.Symlink(admittedHome, homeAlias); err != nil {
		t.Fatalf("create home alias: %v", err)
	}
	t.Setenv("HOME", homeAlias)
	t.Setenv("USERPROFILE", homeAlias)

	destination := outputtest.Parse(t, "~/.agents/skills/oracle")
	effect := ManagedPathEffect{create: &managedPathCreateEffect{facts: managedPathEffectFacts{
		scope: target.ScopeGlobal, destination: destination,
	}}}
	paths := Paths{ManifestRoot: root, DataDir: filepath.Join(root, "data")}
	authority, err := newMutationAuthorityWithProjectionEffects(
		paths,
		[]ManagedPathEffect{effect},
		nil,
		nil,
		destinationResolver(paths),
		testFilesystem(),
		nil,
	)
	if err != nil {
		t.Fatalf("newMutationAuthorityWithProjectionEffects: %v", err)
	}
	t.Cleanup(func() { _ = authority.close() })
	bound, err := authority.resolveBoundDestination(target.ScopeGlobal, destination)
	if err != nil {
		t.Fatalf("resolve bound managed path: %v", err)
	}
	precondition, err := captureManagedPathPrecondition(
		t.Context(), authority, bound, false, "", realization.PathProjectionDirectory, nil,
	)
	if err != nil {
		t.Fatalf("capture bound managed path precondition: %v", err)
	}
	t.Cleanup(func() { _ = precondition.close() })

	if err := os.Remove(homeAlias); err != nil {
		t.Fatalf("remove home alias: %v", err)
	}
	if err := os.Symlink(retargetedHome, homeAlias); err != nil {
		t.Fatalf("retarget home alias: %v", err)
	}
	want, err := mutation.CanonicalDirectoryEntryPath(filepath.Join(admittedHome, ".agents", "skills", "oracle"))
	if err != nil {
		t.Fatalf("canonicalize admitted managed path: %v", err)
	}
	if bound.hostPath != want {
		t.Fatalf("bound managed path = %q, want %q", bound.hostPath, want)
	}
	journalPath, err := authority.rootedJournalResolver(destinationResolver(paths))(destination)
	if err != nil {
		t.Fatalf("resolve bound journal path: %v", err)
	}
	if journalPath != want {
		t.Fatalf("bound journal path = %q, want %q", journalPath, want)
	}

	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatalf("create payload source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("retained-root\n"), 0o600); err != nil {
		t.Fatalf("write payload source: %v", err)
	}
	identity, view := directoryPayloadForTest(t, source)
	if err := commitManagedDirectoryDestination(
		t.Context(), authority, identity, view, bound, &precondition,
	); err != nil {
		t.Fatalf("commit through retained managed path authority: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(want, "SKILL.md"))
	if err != nil || string(content) != "retained-root\n" {
		t.Fatalf("admitted destination content = %q, error = %v", content, err)
	}
	retargetedPath := filepath.Join(retargetedHome, ".agents", "skills", "oracle")
	if _, err := os.Lstat(retargetedPath); !os.IsNotExist(err) {
		t.Fatalf("retargeted destination exists or cannot be inspected: %v", err)
	}
}

func TestManagedPathAuthorityRetainsDataRootRoleAfterDataDirSymlinkRetarget(t *testing.T) {
	root := t.TempDir()
	admittedData := filepath.Join(root, "admitted-data")
	retargetedData := filepath.Join(root, "retargeted-data")
	for _, directory := range []string{admittedData, retargetedData} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatalf("create data root fixture: %v", err)
		}
	}
	dataAlias := filepath.Join(root, "data")
	if err := os.Symlink(admittedData, dataAlias); err != nil {
		t.Fatalf("create data root alias: %v", err)
	}

	destination := outputtest.Parse(t, "@data/hook-assets/guard/sha256-deadbeef/asset")
	effect := ManagedPathEffect{create: &managedPathCreateEffect{facts: managedPathEffectFacts{
		scope: target.ScopeGlobal, destination: destination,
	}}}
	paths := Paths{ManifestRoot: root, DataDir: dataAlias}
	authority, err := newMutationAuthorityWithProjectionEffects(
		paths,
		[]ManagedPathEffect{effect},
		nil,
		nil,
		destinationResolver(paths),
		testFilesystem(),
		nil,
	)
	if err != nil {
		t.Fatalf("newMutationAuthorityWithProjectionEffects: %v", err)
	}
	t.Cleanup(func() { _ = authority.close() })
	bound, err := authority.resolveBoundDestination(target.ScopeGlobal, destination)
	if err != nil {
		t.Fatalf("resolve bound managed path: %v", err)
	}
	precondition, err := captureManagedPathPrecondition(
		t.Context(), authority, bound, false, "", realization.PathProjectionDirectory, nil,
	)
	if err != nil {
		t.Fatalf("capture bound managed path precondition: %v", err)
	}
	t.Cleanup(func() { _ = precondition.close() })

	if err := os.Remove(dataAlias); err != nil {
		t.Fatalf("remove data root alias: %v", err)
	}
	if err := os.Symlink(retargetedData, dataAlias); err != nil {
		t.Fatalf("retarget data root alias: %v", err)
	}
	want, err := mutation.CanonicalDirectoryEntryPath(filepath.Join(admittedData, "hook-assets", "guard", "sha256-deadbeef", "asset"))
	if err != nil {
		t.Fatalf("canonicalize admitted data destination: %v", err)
	}
	if bound.hostPath != want {
		t.Fatalf("bound managed path = %q, want %q", bound.hostPath, want)
	}
	journalPath, err := authority.rootedJournalResolver(destinationResolver(paths))(destination)
	if err != nil {
		t.Fatalf("resolve bound journal path: %v", err)
	}
	if journalPath != want {
		t.Fatalf("bound journal path = %q, want %q", journalPath, want)
	}

	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatalf("create payload source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "marker"), []byte("retained-data-root\n"), 0o600); err != nil {
		t.Fatalf("write payload source: %v", err)
	}
	identity, view := directoryPayloadForTest(t, source)
	if err := commitManagedDirectoryDestination(t.Context(), authority, identity, view, bound, &precondition); err != nil {
		t.Fatalf("commit through retained data root authority: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(want, "marker"))
	if err != nil || string(content) != "retained-data-root\n" {
		t.Fatalf("admitted data destination content = %q, error = %v", content, err)
	}
	retargetedPath := filepath.Join(retargetedData, "hook-assets", "guard", "sha256-deadbeef", "asset")
	if _, err := os.Lstat(retargetedPath); !os.IsNotExist(err) {
		t.Fatalf("retargeted data destination exists or cannot be inspected: %v", err)
	}
}

func TestManagedPathAuthoritySharesCapturedRootAcrossGlobalSiblings(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	sharedRoot := filepath.Join(home, ".agents", "skills")
	if err := os.MkdirAll(sharedRoot, 0o700); err != nil {
		t.Fatalf("create shared global skill root: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	const effectCount = 300
	effects := make([]ManagedPathEffect, 0, effectCount)
	for index := range effectCount {
		effect := ManagedPathEffect{create: &managedPathCreateEffect{facts: managedPathEffectFacts{
			scope:       target.ScopeGlobal,
			destination: outputtest.Parse(t, fmt.Sprintf("~/.agents/skills/skill-%03d", index)),
		}}}
		effects = append(effects, effect, effect)
	}
	paths := Paths{ManifestRoot: home, DataDir: filepath.Join(home, ".daem")}
	authority, err := newMutationAuthorityWithProjectionEffects(
		paths,
		effects,
		nil,
		nil,
		destinationResolver(paths),
		testFilesystem(),
		nil,
	)
	if err != nil {
		t.Fatalf("bind sibling managed paths: %v", err)
	}
	t.Cleanup(func() { _ = authority.close() })
	if len(authority.globalDestinationBindings) != effectCount {
		t.Fatalf("global bindings = %d, want %d", len(authority.globalDestinationBindings), effectCount)
	}
	if len(authority.retainedGlobalRoots) != 1 {
		t.Fatalf("retained global roots = %d, want 1 shared root", len(authority.retainedGlobalRoots))
	}
	retained := authority.retainedGlobalRoots[0]
	for logical, binding := range authority.globalDestinationBindings {
		if binding.root != retained {
			t.Fatalf("binding %q retained a duplicate root witness", logical)
		}
		capability, err := binding.root.Acquire(binding.destination)
		if err != nil {
			t.Fatalf("binding %q lost shared root authority: %v", logical, err)
		}
		if err := capability.Close(); err != nil {
			t.Fatalf("close binding %q capability: %v", logical, err)
		}
	}
}

func captureBoundGlobalManagedPathPrecondition(
	t *testing.T,
	ctx context.Context,
	path string,
	expectedExists bool,
	expectedHash artifact.ContentHash,
) (managedPathPrecondition, error) {
	t.Helper()
	root := filepath.Dir(path)
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	destination := outputtest.Parse(t, "~/"+filepath.Base(path))
	effect := ManagedPathEffect{create: &managedPathCreateEffect{facts: managedPathEffectFacts{
		scope: target.ScopeGlobal, destination: destination,
	}}}
	paths := Paths{ManifestRoot: root, DataDir: root}
	authority, err := newMutationAuthorityWithProjectionEffects(
		paths,
		[]ManagedPathEffect{effect},
		nil,
		nil,
		destinationResolver(paths),
		testFilesystem(),
		nil,
	)
	if err != nil {
		t.Fatalf("bind global managed path: %v", err)
	}
	t.Cleanup(func() { _ = authority.close() })
	bound, err := authority.resolveBoundDestination(target.ScopeGlobal, destination)
	if err != nil {
		t.Fatalf("resolve bound global managed path: %v", err)
	}
	return captureManagedPathPrecondition(
		ctx, authority, bound, expectedExists, expectedHash, realization.PathProjectionDirectory, nil,
	)
}
