//go:build !darwin && !linux

package codexplugin

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	observecontribution "github.com/isty2e/daem/internal/assurance/observe/contribution"
	"github.com/isty2e/daem/internal/filesnapshot"
)

func TestUnsupportedTreeAdaptersFailClosedWithoutPathnameReopen(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dirPath := filepath.Join(root, "plugin")
	if err := os.Mkdir(dirPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirPath, "inside.json"), []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dirPath, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	parent, err := os.Open(dirPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = parent.Close() })

	outside := filepath.Join(root, "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "inside.json"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(outside, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dirPath, dirPath+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(outside, dirPath); err != nil {
		t.Fatal(err)
	}

	file, err := openChildDirectoryNoFollow(parent, "child")
	if file != nil || !errors.Is(err, errDescriptorRelativeTreeUnsupported) {
		t.Fatalf("openChildDirectoryNoFollow after path replacement = %v, %v, want unsupported", file, err)
	}

	kind, err := classifyChild(parent, "inside.json")
	if kind != childMissing || !errors.Is(err, errDescriptorRelativeTreeUnsupported) {
		t.Fatalf("classifyChild after path replacement = %v, %v, want unsupported", kind, err)
	}

	counted, err := filesnapshot.ReadRegularFileAtCounted(t.Context(), parent, "inside.json", 64)
	if !errors.Is(err, filesnapshot.ErrUnsupported) {
		t.Fatalf("ReadRegularFileAtCounted after path replacement = %+v, %v, want ErrUnsupported", counted, err)
	}
	if string(counted.Content) == "outside" || string(counted.Content) == "inside" {
		t.Fatalf("tree observation leaked file bytes = %q", counted.Content)
	}
}

func TestUnsupportedTreeAdaptersRejectInvalidInputWithoutUnsupported(t *testing.T) {
	t.Parallel()

	dir, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dir.Close() })

	file, err := openChildDirectoryNoFollow(nil, "child")
	if file != nil || err == nil || errors.Is(err, errDescriptorRelativeTreeUnsupported) {
		t.Fatalf("nil parent open = %v, %v, want invalid input", file, err)
	}
	file, err = openChildDirectoryNoFollow(dir, "nested/name")
	if file != nil || err == nil || errors.Is(err, errDescriptorRelativeTreeUnsupported) {
		t.Fatalf("nested name open = %v, %v, want invalid input", file, err)
	}

	kind, err := classifyChild(nil, "child")
	if err == nil || errors.Is(err, errDescriptorRelativeTreeUnsupported) {
		t.Fatalf("nil parent classify = %v, %v, want invalid input", kind, err)
	}
	kind, err = classifyChild(dir, "..")
	if err == nil || errors.Is(err, errDescriptorRelativeTreeUnsupported) {
		t.Fatalf("dotdot classify = %v, %v, want invalid input", kind, err)
	}
}

func TestUnsupportedTreeObservationDoesNotTreatCapabilityGapAsMissing(t *testing.T) {
	t.Parallel()

	homeDirectory := t.TempDir()
	pluginRoot := filepath.Join(homeDirectory, ".codex", "plugins", "cache", "market", "alpha", "local")
	if err := os.MkdirAll(filepath.Join(pluginRoot, ".codex-plugin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), []byte(`{"mcpServers":{"local":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cacheRoot, reason, err := openPluginCacheLayout(
		filepath.Join(homeDirectory, ".codex", "plugins", "cache"),
		&observationBudget{},
	)
	if err != nil || reason != observecontribution.SourceContributionReasonNone || cacheRoot == nil {
		t.Fatalf("openPluginCacheLayout = %v, %s, %v", cacheRoot, reason, err)
	}
	t.Cleanup(cacheRoot.close)

	plugin, version, ok, ambiguous, reason, err := activePluginCacheVersion(
		t.Context(),
		cacheRoot,
		"market",
		"alpha",
	)
	if plugin != nil {
		plugin.close()
	}
	if err != nil || ok || ambiguous || version != "" {
		t.Fatalf("activePluginCacheVersion = %v, %q, ok=%t ambiguous=%t err=%v, want unavailable capability gap", plugin, version, ok, ambiguous, err)
	}
	if reason != observecontribution.SourceContributionReasonArtifactUnavailable {
		t.Fatalf("activePluginCacheVersion reason = %q, want SOURCE_ARTIFACT_UNAVAILABLE not missing/none", reason)
	}
	if directoryMissing(errDescriptorRelativeTreeUnsupported) {
		t.Fatal("unsupported tree capability must not classify as a missing directory")
	}
}
