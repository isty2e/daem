package build

import (
	"context"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/hook"
	"github.com/isty2e/daem/internal/desired/hookasset"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

func TestBuildLocksHookAssetWithExecutableIdentity(t *testing.T) {
	root := t.TempDir()
	sourcePath := writeFile(t, root, "hooks/guard.sh", "#!/bin/sh\necho guard\n")
	sourceSpec := sourcetest.Local(t, "hooks/guard.sh", source.LocalSourceModeVendor)

	lockfile, err := buildWithTestOptions(context.Background(), lockEnvironment(t, desired.Spec{
		HookAssets: []hookasset.HookAsset{
			desiredtest.HookAsset(t, hookasset.Spec{
				Name: "guard", Source: sourceSpec, ArtifactKind: hookasset.ArtifactKindFile,
				Scope: target.ScopeProject, Executable: true,
			}),
		},
		Hooks: []hook.Hook{
			desiredtest.Hook(t, hook.Spec{
				Name: "protect", Event: "Stop", Type: hook.TypeCommand,
				Command: "sh {hook_file:guard}", Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject,
			}),
		},
	}), stubResolver{artifacts: map[string]resolutionFixture{
		"local:hooks/guard.sh?mode=vendor": {
			SourceID:    "local:hooks/guard.sh?mode=vendor",
			ContentPath: sourcePath,
			Kind:        artifact.ArtifactKindFile,
			ContentHash: "sha256:resolver-observation",
		},
	}}, Options{})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	locked := mustLockedSubject(t, lockfile, entity.KindHookAsset, "guard")
	identity := mustExactSupply(t, locked)
	expectedHash := artifact.HashFileContent([]byte("#!/bin/sh\necho guard\n"))
	if identity.SourceID() != "local:hooks/guard.sh?mode=vendor" ||
		identity.ResolvedRef() != "" ||
		identity.ContentHash() != expectedHash ||
		identity.Kind() != artifact.ArtifactKindFile {
		t.Fatalf("locked hook asset identity = %#v", identity)
	}
	fileUse, ok := locked.ExactFileUse()
	if !ok || fileUse.Scope() != target.ScopeProject || !fileUse.Executable() {
		t.Fatalf("locked hook asset file use = %#v, %t", fileUse, ok)
	}
	derivation, ok := locked.Derivation()
	if !ok {
		t.Fatal("locked hook asset has no derivation")
	}
	transform, ok := derivation.DeterministicTransform()
	if !ok || !transform.InputIdentity.Equal(identity) ||
		transform.ExpectedOutputIdentity.ContentHash() != artifact.HashFileContentWithExecutable([]byte("#!/bin/sh\necho guard\n"), true) ||
		transform.AlgorithmID != artifact.FileMaterializationAlgorithmID {
		t.Fatalf("locked hook asset derivation = %#v, %t", transform, ok)
	}

	pathProjections := lockedPathProjectionSubjectsOfKind(lockfile, entity.KindHookAsset)
	if len(pathProjections) != 1 {
		t.Fatalf("HookAsset path projections = %#v, want one", pathProjections)
	}
	expected, err := refine.ExpectedManagedPaths(
		lockEnvironment(t, desired.Spec{
			HookAssets: []hookasset.HookAsset{desiredtest.HookAsset(t, hookasset.Spec{
				Name: "guard", Source: sourceSpec, ArtifactKind: hookasset.ArtifactKindFile,
				Scope: target.ScopeProject, Executable: true,
			})},
			Hooks: []hook.Hook{desiredtest.Hook(t, hook.Spec{
				Name: "protect", Event: "Stop", Type: hook.TypeCommand,
				Command: "sh {hook_file:guard}", Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject,
			})},
		}),
		lockfile.Locked,
	)
	if err != nil {
		t.Fatalf("ExpectedManagedPaths returned error: %v", err)
	}
	var expectedHookAssets []lock.LockedSubjectContract
	for _, contract := range expected {
		if contract.EntityID().Kind() == entity.KindHookAsset {
			expectedHookAssets = append(expectedHookAssets, contract)
		}
	}
	if len(expectedHookAssets) != 1 || !expectedHookAssets[0].Equal(pathProjections[0]) {
		t.Fatalf("expected HookAsset path = %#v, locked = %#v", expectedHookAssets, pathProjections)
	}
	pathRealization, _ := pathProjections[0].Realization()
	pathProjection, _ := pathRealization.ManagedPathProjection()
	if strings.HasPrefix(pathProjection.Destination(), root) ||
		!strings.HasPrefix(pathProjection.Destination(), ".daem/hook-assets/guard/sha256-") {
		t.Fatalf("HookAsset destination = %q, want portable content-addressed project path", pathProjection.Destination())
	}
	hookContracts := lockedSubjectsOfKind(lockfile, entity.KindHook)
	if len(hookContracts) != 1 {
		t.Fatalf("Hook contribution subjects = %#v, want one", hookContracts)
	}
	hookRealization, _ := hookContracts[0].Realization()
	hookContribution, _ := hookRealization.ManagedAggregateContribution()
	if !strings.Contains(hookContribution.CanonicalContribution(), "{hook_file:guard}") ||
		strings.Contains(hookContribution.CanonicalContribution(), root) {
		t.Fatalf("locked Hook contribution is not portable: %q", hookContribution.CanonicalContribution())
	}
}

func TestHookAssetExecutableIntentDoesNotChangeExactSupply(t *testing.T) {
	root := t.TempDir()
	content := []byte("#!/bin/sh\necho guard\n")
	sourcePath := writeFile(t, root, "hooks/guard.sh", string(content))
	sourceSpec := sourcetest.Local(t, "hooks/guard.sh", source.LocalSourceModeVendor)
	resolver := stubResolver{artifacts: map[string]resolutionFixture{
		"local:hooks/guard.sh?mode=vendor": {
			SourceID: "local:hooks/guard.sh?mode=vendor", ContentPath: sourcePath,
			Kind: artifact.ArtifactKindFile, ContentHash: "sha256:resolver-observation",
		},
	}}

	locked := make([]lock.LockedSubjectContract, 0, 2)
	for _, executable := range []bool{false, true} {
		file, err := buildWithTestOptions(context.Background(), lockEnvironment(t, desired.Spec{
			HookAssets: []hookasset.HookAsset{desiredtest.HookAsset(t, hookasset.Spec{
				Name: "guard", Source: sourceSpec, ArtifactKind: hookasset.ArtifactKindFile,
				Scope: target.ScopeProject, Executable: executable,
			})},
		}), resolver, Options{})
		if err != nil {
			t.Fatalf("buildWithTestOptions(executable=%t) returned error: %v", executable, err)
		}
		locked = append(locked, mustLockedSubject(t, file, entity.KindHookAsset, "guard"))
	}

	leftSupply := mustExactSupply(t, locked[0])
	rightSupply := mustExactSupply(t, locked[1])
	if !leftSupply.Equal(rightSupply) || leftSupply.ContentHash() != artifact.HashFileContent(content) {
		t.Fatalf("ExactSupply changed with executable intent: false=%#v true=%#v", leftSupply, rightSupply)
	}
	leftUse, _ := locked[0].ExactFileUse()
	rightUse, _ := locked[1].ExactFileUse()
	if leftUse.Equal(rightUse) {
		t.Fatal("ExactFileUse did not preserve executable intent drift")
	}
}

func TestBuildRejectsNonFileHookAssetSource(t *testing.T) {
	root := t.TempDir()
	sourcePath := writeSkill(t, root, "hooks")
	sourceSpec := sourcetest.Local(t, "hooks", source.LocalSourceModeVendor)

	_, err := buildWithTestOptions(context.Background(), lockEnvironment(t, desired.Spec{
		HookAssets: []hookasset.HookAsset{
			desiredtest.HookAsset(t, hookasset.Spec{
				Name: "guard", Source: sourceSpec, ArtifactKind: hookasset.ArtifactKindFile, Scope: target.ScopeProject,
			}),
		},
	}), stubResolver{artifacts: map[string]resolutionFixture{
		"local:hooks?mode=vendor": {
			SourceID:    "local:hooks?mode=vendor",
			ContentPath: sourcePath,
			Kind:        artifact.ArtifactKindDirectory,
			ContentHash: "sha256:directory",
		},
	}}, Options{})
	if err == nil || !strings.Contains(err.Error(), `validate hook_asset "guard" source: expected file artifact`) {
		t.Fatalf("Build error = %v, want non-file source diagnostic", err)
	}
}
