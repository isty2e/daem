//go:build darwin || linux

package migrate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
)

func TestMetadataMigrationPreviewExecutionAndRepeat(t *testing.T) {
	paths, before := metadataMigrationFixture(t)
	prepared, err := PlanMetadata(t.Context(), MetadataInput{ManifestPath: paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	preview := prepared.Disclosure()
	if preview.Action != "convert" || len(preview.Statefiles) != 2 || len(preview.Targets) != 4 {
		t.Fatalf("whole-registry preview = %+v", preview)
	}
	requireMetadataImages(t, before)
	preview.Targets[0] = filepath.Join(t.TempDir(), "not-a-target")
	result, err := prepared.Execute(t.Context())
	if err != nil || result.Action != "converted" {
		t.Fatal("explicit conversion did not complete", err)
	}
	for _, path := range result.Statefiles {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		image, err := statefile.UnmarshalImage(content)
		if err != nil || image.Snapshot().NativeDocumentCommit() != 21 {
			t.Fatal("conversion changed State facts or omitted a participant", err)
		}
	}
	content, err := os.ReadFile(paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	image, err := ownershipstore.UnmarshalImage(content)
	if err != nil || len(image.Registry().DocumentBindings()) != 2 {
		t.Fatal("conversion lost foreign registry facts", err)
	}
	content, err = os.ReadFile(paths.CarrierClaimRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := carrierclaim.UnmarshalImage(content); err != nil {
		t.Fatal("carrier registry was not converted with the cohort", err)
	}
	if _, err := prepared.Execute(t.Context()); err == nil {
		t.Fatal("disclosed conversion was executed more than once")
	}
	repeated, err := PlanMetadata(t.Context(), MetadataInput{ManifestPath: paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	defer repeated.Close()
	if repeated.Disclosure().Action != "already_converted" {
		t.Fatal("located cohort did not become a no-op")
	}
	if _, err := repeated.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestMetadataMigrationDoesNotPublishAfterCancellationOrSourceChange(t *testing.T) {
	for _, failure := range []string{"cancel", "stale"} {
		t.Run(failure, func(t *testing.T) {
			paths, before := metadataMigrationFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			prepared, err := PlanMetadata(ctx, MetadataInput{ManifestPath: paths.ManifestPath})
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			if failure == "cancel" {
				cancel()
			} else {
				changed := append(bytes.Clone(before[paths.StatefilePath]), '\n')
				writeFixture(t, paths.StatefilePath, changed)
				before[paths.StatefilePath] = changed
			}
			_, err = prepared.Execute(ctx)
			var stale mutation.StaleSnapshotError
			if failure == "cancel" && !errors.Is(err, context.Canceled) || failure == "stale" && !errors.As(err, &stale) {
				t.Fatal("execution lost its cancellation or stale classification", err)
			}
			requireMetadataImages(t, before)
			if _, err := os.Lstat(paths.GlobalMetadataDir()); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("refused execution created publication evidence", err)
			}
		})
	}
}

func metadataMigrationFixture(t *testing.T) (daempaths.Paths, map[string][]byte) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for key, directory := range map[string]string{
		"HOME": "home", "XDG_CONFIG_HOME": "config", "XDG_STATE_HOME": "state", "XDG_DATA_HOME": "data",
	} {
		t.Setenv(key, filepath.Join(root, directory))
	}
	before := make(map[string][]byte)
	var selected daempaths.Paths
	var bindings []ownership.DocumentBinding
	for index := range 2 {
		manifest := filepath.Join(root, fmt.Sprintf("project-%d", index), "daem.toml")
		writeFixture(t, manifest, []byte("version=1\ntargets=['pi']\n"))
		paths, err := daempaths.Resolve(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			selected = paths
		}
		snapshot, err := durable.NewSnapshot(durable.SnapshotInput{NativeDocumentCommit: 21})
		if err != nil {
			t.Fatal(err)
		}
		content, err := statefile.Marshal(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, paths.StatefilePath, content)
		before[paths.StatefilePath] = content
		document := filepath.Join(paths.ManifestRoot, "document.json")
		writeFixture(t, document, []byte("{}"))
		stateAuthority, err := mutation.ObservePersistedDirectoryEntryAuthority(paths.StatefilePath)
		if err != nil {
			t.Fatal(err)
		}
		owner, err := stateauthority.New(stateAuthority.Exact(), paths.ManifestPath)
		if err != nil {
			t.Fatal(err)
		}
		documentAuthority, err := mutation.ObservePersistedDirectoryEntryAuthority(document)
		if err != nil {
			t.Fatal(err)
		}
		destination, err := output.Parse("document.json")
		if err != nil {
			t.Fatal(err)
		}
		binding, err := ownership.NewDocumentBinding(owner, destination, documentAuthority.Exact())
		if err != nil {
			t.Fatal(err)
		}
		bindings = append(bindings, binding)
	}
	registry, err := ownership.NewRegistry(nil, bindings...)
	if err != nil {
		t.Fatal(err)
	}
	content, err := ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, selected.OwnershipRegistryPath, content)
	before[selected.OwnershipRegistryPath] = content
	content, err = carrierclaim.Marshal(durablecarrier.EmptyGlobalCarrierClaims())
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, selected.CarrierClaimRegistryPath, content)
	before[selected.CarrierClaimRegistryPath] = content
	return selected, before
}

func requireMetadataImages(t *testing.T, images map[string][]byte) {
	t.Helper()
	for path, expected := range images {
		content, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(content, expected) {
			t.Fatalf("metadata %q changed: %v", path, err)
		}
	}
}
