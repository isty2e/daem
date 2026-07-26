package build

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hookresource "github.com/isty2e/daem/internal/desired/hook"
	hookassetresource "github.com/isty2e/daem/internal/desired/hookasset"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	daempaths "github.com/isty2e/daem/internal/paths"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/topology"
	topologyhook "github.com/isty2e/daem/internal/topology/hook"
	resourcetopology "github.com/isty2e/daem/internal/topology/resource"
)

func TestBuildMaterializesOnlyReferencedExecutableHookAsset(t *testing.T) {
	root := t.TempDir()
	guardContent := "#!/bin/sh\necho guard\n"
	unusedContent := "#!/bin/sh\necho unused\n"
	writePayloadSource(t, root, "hooks/guard.sh", guardContent)
	writePayloadSource(t, root, "hooks/unused.sh", unusedContent)
	guardHash := artifact.HashFileContentWithExecutable([]byte(guardContent), true)
	assets := []hookassetresource.HookAsset{
		payloadAsset(t, "guard", true),
		payloadAsset(t, "unused", true),
	}
	guardLocked := payloadLockedAsset(t, assets[0], guardContent)
	unusedLocked := payloadLockedAsset(t, assets[1], unusedContent)

	resolvers := sourceResolverOnce{paths: mustPayloadPaths(t, root)}
	payloads, err := buildHookAssetPayloads(
		context.Background(),
		&resolvers,
		assets,
		snapshottest.File(t, append(guardLocked, unusedLocked...)...),
		[]topology.SubjectID{guardLocked[1].SubjectID()},
	)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("payloads = %#v, want only referenced guard payload", payloads)
	}
	if payloads[0].Subject() != guardLocked[1].SubjectID() {
		t.Fatalf("Subject = %q, want %q", payloads[0].Subject(), guardLocked[1].SubjectID())
	}
	file, ok := payloads[0].File()
	if !ok {
		t.Fatal("File returned no file variant")
	}
	if string(file.Bytes()) != "#!/bin/sh\necho guard\n" {
		t.Fatalf("Content = %q", file.Bytes())
	}
	if payloads[0].Hash() != guardHash {
		t.Fatalf("Hash = %q, want %q", payloads[0].Hash(), guardHash)
	}
	if file.Mode() != 0o700 {
		t.Fatalf("FileMode = %o, want executable mode", file.Mode())
	}
}

func TestBuildRejectsStaleHookAssetSource(t *testing.T) {
	root := t.TempDir()
	writePayloadSource(t, root, "hooks/guard.sh", "#!/bin/sh\necho changed\n")
	oldContent := "#!/bin/sh\necho old\n"

	asset := payloadAsset(t, "guard", true)
	locked := payloadLockedAsset(t, asset, oldContent)
	resolvers := sourceResolverOnce{paths: mustPayloadPaths(t, root)}
	_, err := buildHookAssetPayloads(
		context.Background(),
		&resolvers,
		[]hookassetresource.HookAsset{asset},
		snapshottest.File(t, locked...),
		[]topology.SubjectID{locked[1].SubjectID()},
	)
	if err == nil || !strings.Contains(err.Error(), `source identity does not match lockfile entry`) {
		t.Fatalf("Build error = %v, want stale exact-Supply diagnostic", err)
	}
}

func TestBuildHookAssetPayloadsRejectsDuplicateRequiredSubject(t *testing.T) {
	root := t.TempDir()
	content := "#!/bin/sh\necho guard\n"
	writePayloadSource(t, root, "hooks/guard.sh", content)
	asset := payloadAsset(t, "guard", true)
	locked := payloadLockedAsset(t, asset, content)
	subject := locked[1].SubjectID()
	resolvers := sourceResolverOnce{paths: mustPayloadPaths(t, root)}

	_, err := buildHookAssetPayloads(
		t.Context(),
		&resolvers,
		[]hookassetresource.HookAsset{asset},
		snapshottest.File(t, locked...),
		[]topology.SubjectID{subject, subject},
	)
	if err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("buildHookAssetPayloads duplicate error = %v, want duplicate-subject rejection", err)
	}
}

func TestBuildUsesNonExecutableHookAssetMode(t *testing.T) {
	root := t.TempDir()
	writePayloadSource(t, root, "hooks/guard.sh", "echo guard\n")
	content := "echo guard\n"

	asset := payloadAsset(t, "guard", false)
	locked := payloadLockedAsset(t, asset, content)
	resolvers := sourceResolverOnce{paths: mustPayloadPaths(t, root)}
	payloads, err := buildHookAssetPayloads(
		context.Background(),
		&resolvers,
		[]hookassetresource.HookAsset{asset},
		snapshottest.File(t, locked...),
		[]topology.SubjectID{locked[1].SubjectID()},
	)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("payloads = %#v, want one", payloads)
	}
	file, ok := payloads[0].File()
	if !ok {
		t.Fatal("File returned no file variant")
	}
	if file.Mode() != 0o600 {
		t.Fatalf("FileMode = %o, want non-executable mode", file.Mode())
	}
}

func mustPayloadPaths(t *testing.T, root string) daempaths.Paths {
	t.Helper()

	paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatalf("paths.Resolve returned error: %v", err)
	}
	return paths
}

func mustPayloadSelection(t *testing.T, available ...target.Target) targetselection.Selection {
	t.Helper()

	selection, err := targetselection.ForAvailableTargets(available, nil)
	if err != nil {
		t.Fatalf("ForAvailableTargets returned error: %v", err)
	}
	return selection
}

func payloadAsset(t *testing.T, name string, executable bool) hookassetresource.HookAsset {
	t.Helper()
	return desiredtest.HookAsset(t, hookassetresource.Spec{
		Name:         name,
		Source:       sourcetest.Local(t, "hooks/"+name+".sh", source.LocalSourceModeVendor),
		ArtifactKind: hookassetresource.ArtifactKindFile,
		Scope:        target.ScopeProject,
		Executable:   executable,
	})
}

func payloadHook(t *testing.T, name string, command string) hookresource.Hook {
	t.Helper()
	return desiredtest.Hook(t, hookresource.Spec{
		Name:    name,
		Event:   "Stop",
		Type:    hookresource.TypeCommand,
		Command: command,
		Targets: []target.Target{target.TargetCodex},
		Scope:   target.ScopeProject,
	})
}

func payloadLockedAsset(
	t *testing.T,
	asset hookassetresource.HookAsset,
	content string,
) []lock.LockedSubjectContract {
	t.Helper()
	name := asset.ID().Name()
	inputIdentity, err := artifact.NewExactIdentity(
		artifact.SourceID("local:hooks/"+name+".sh?mode=vendor"),
		"",
		artifact.ArtifactKindFile,
		artifact.HashFileContent([]byte(content)),
	)
	if err != nil {
		t.Fatalf("NewExactIdentity returned error: %v", err)
	}
	materialization, err := artifact.NewFileMaterialization(inputIdentity, []byte(content), false, asset.Executable())
	if err != nil {
		t.Fatalf("NewFileMaterialization returned error: %v", err)
	}
	fileUse, err := lock.NewExactFileUse(target.ScopeProject, asset.Executable())
	if err != nil {
		t.Fatalf("NewExactFileUse returned error: %v", err)
	}
	derivation, err := lock.NewFileMaterializationDerivation(materialization)
	if err != nil {
		t.Fatalf("NewFileMaterializationDerivation returned error: %v", err)
	}
	entityID := asset.ID()
	subjectID, err := resourcetopology.Subject(entityID)
	if err != nil {
		t.Fatalf("resource topology subject: %v", err)
	}
	supply, err := lock.NewExactSupplySubjectContract(lock.ExactSupplySubjectInput{
		EntityID:     entityID,
		SubjectID:    subjectID,
		ExactSupply:  materialization.InputIdentity(),
		ExactFileUse: &fileUse,
		Derivation:   derivation,
	})
	if err != nil {
		t.Fatalf("NewExactSupplySubjectContract returned error: %v", err)
	}
	lowered, err := topologyhook.Lower(
		[]hookassetresource.HookAsset{asset},
		[]hookresource.Hook{payloadHook(t, "consumer", "run {hook_file:"+name+"}")},
	)
	if err != nil {
		t.Fatalf("lower HookAsset topology: %v", err)
	}
	pathContract, err := refine.HookAssetPathProjection(
		asset,
		lowered.AssetProjections()[0],
		materialization.OutputIdentity(),
		[]target.Target{target.TargetCodex},
	)
	if err != nil {
		t.Fatalf("build HookAsset path projection: %v", err)
	}
	return []lock.LockedSubjectContract{supply, pathContract}
}

func writePayloadSource(t *testing.T, root string, relative string, content string) {
	t.Helper()

	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}
