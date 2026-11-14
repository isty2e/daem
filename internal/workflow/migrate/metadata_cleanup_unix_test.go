//go:build darwin || linux

package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMetadataRecoveryCleanupIsDisclosedAndStateIndependent(t *testing.T) {
	paths, before := metadataMigrationFixture(t)
	stage := filepath.Join(paths.GlobalMetadataDir(), ".daem-tmp-"+strings.Repeat("a", 32))
	if err := os.MkdirAll(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "intent.json"), []byte("{torn"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Collection must not need a decodable selected State or registry.
	for _, path := range []string{paths.StatefilePath, paths.OwnershipRegistryPath} {
		before[path] = []byte("{unreadable")
		if err := os.WriteFile(path, before[path], 0o600); err != nil {
			t.Fatal(err)
		}
	}
	prepared, err := PlanMetadata(t.Context(), MetadataInput{ManifestPath: paths.ManifestPath, Recover: true})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	preview := prepared.Disclosure()
	if preview.Action != "cleanup" || len(preview.CleanupPaths) != 1 || len(preview.Targets) != 0 {
		t.Fatal("cleanup hidden as no-op or target conversion", preview)
	}
	if _, err := os.Stat(stage); err != nil {
		t.Fatal("planning collected an artifact", err)
	}
	if _, err := prepared.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(stage); !os.IsNotExist(err) {
		t.Fatal("cleanup left its disclosed artifact", err)
	}
	requireMetadataImages(t, before)
	repeated, err := PlanMetadata(t.Context(), MetadataInput{ManifestPath: paths.ManifestPath, Recover: true})
	if err != nil {
		t.Fatal(err)
	}
	defer repeated.Close()
	if repeated.Disclosure().Action != "none" {
		t.Fatal("completed cleanup did not become a no-op", repeated.Disclosure())
	}
}

func TestMetadataCleanupDoesNotBypassInvalidActiveEvidence(t *testing.T) {
	paths, before := metadataMigrationFixture(t)
	active := filepath.Join(paths.GlobalMetadataDir(), "metadata-transaction")
	if err := os.MkdirAll(active, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanMetadata(t.Context(), MetadataInput{ManifestPath: paths.ManifestPath, Recover: true}); err == nil {
		t.Fatal("invalid active evidence became cleanup")
	}
	requireMetadataImages(t, before)
	if _, err := os.Stat(active); err != nil {
		t.Fatal("invalid evidence was discarded", err)
	}
}
