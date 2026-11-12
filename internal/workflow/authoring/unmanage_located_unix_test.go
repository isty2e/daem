//go:build darwin || linux

package authoring

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	"github.com/isty2e/daem/internal/target"
)

func TestUnmanageRetainsLocatedForeignCarrierEvidence(t *testing.T) {
	root := t.TempDir()
	configureUnmanageTestHomes(t, root)
	paths := unmanageTestPaths(t, root)
	writeUnmanageFile(t, paths.ManifestPath, []byte("version = 1\ntargets = [\"claude-code\"]\n"))
	selected := newUnmanageTestFixture(t, "context7", "context7@official", target.ScopeGlobal)
	selectedClaim := unmanageTestClaim(t, selected, unmanageTestOwner(t, paths))
	foreignPaths := unmanageTestPaths(t, filepath.Join(root, "foreign"))
	if err := os.MkdirAll(foreignPaths.ManifestRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := newUnmanageTestFixture(t, "other", "other@official", target.ScopeGlobal)
	foreignClaim := unmanageTestClaim(t, foreign, unmanageTestOwner(t, foreignPaths))
	alias := filepath.Join(root, "foreign-lookup")
	if err := os.Symlink(foreignPaths.ManifestRoot, alias); err != nil {
		t.Fatal(err)
	}
	foreignReference, err := pathauthority.NewReference(foreignClaim.Owner().StatefileAuthority(), filepath.Join(alias, ".daem", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	observed, err := mutation.ObservePersistedDirectoryEntryAuthority(paths.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{selectedClaim, foreignClaim})
	if err != nil {
		t.Fatal(err)
	}
	image, err := carrierclaim.NewRegistryImage(registry, []pathauthority.Reference{observed.Reference(), foreignReference})
	if err != nil {
		t.Fatal(err)
	}
	content, err := carrierclaim.MarshalImage(image)
	if err != nil {
		t.Fatal(err)
	}
	writeUnmanageFile(t, paths.CarrierClaimRegistryPath, content)
	request := UnmanageExtensionRequest{ManifestPath: paths.ManifestPath, ID: "context7", Scope: target.ScopeGlobal, Mode: UnmanageModeDryRun}
	if _, err := UnmanageExtension(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, readUnmanageFile(t, paths.CarrierClaimRegistryPath)) {
		t.Fatal("preview changed the located carrier image")
	}
	request.Mode = UnmanageModeWrite
	result, err := UnmanageExtension(t.Context(), request)
	if err != nil || result.RegistryStatus != UnmanageStateStatusWritten {
		t.Fatal("located unmanage failed", err)
	}
	after, err := carrierclaim.UnmarshalImage(readUnmanageFile(t, paths.CarrierClaimRegistryPath))
	if err != nil {
		t.Fatal("unmanage downgraded carrier reference evidence", err)
	}
	if claims := after.Registry().Claims(); len(claims) != 1 || !claims[0].ExactEqual(foreignClaim) {
		t.Fatal("unmanage changed foreign carrier facts")
	}
	if references := after.References(); len(references) != 1 || !references[0].Equal(foreignReference) {
		t.Fatal("unmanage lost foreign locator evidence or retained the released owner")
	}
}
