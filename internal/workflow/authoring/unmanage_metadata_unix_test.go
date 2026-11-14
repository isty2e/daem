//go:build darwin || linux

package authoring

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	"github.com/isty2e/daem/internal/target"
)

func TestUnmanageConvertsOnlyBeforeMetadataWrites(t *testing.T) {
	for _, test := range []struct {
		name       string
		scope      target.Scope
		claims     bool
		foreignBad bool
	}{
		{name: "project State", scope: target.ScopeProject, claims: true},
		{name: "global registry", scope: target.ScopeGlobal, claims: true},
		{name: "manifest only", scope: target.ScopeProject},
		{name: "foreign conversion failure", scope: target.ScopeProject, claims: true, foreignBad: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			configureUnmanageTestHomes(t, root)
			paths := unmanageTestPaths(t, root)
			writeUnmanageFile(t, paths.ManifestPath, []byte(unmanageManifest("context7@official", test.scope)))
			input := durable.SnapshotInput{NativeDocumentCommit: 7}
			if test.claims {
				fixture := newUnmanageTestFixture(t, "context7", "context7@official", test.scope)
				claim := unmanageTestClaim(t, fixture, unmanageTestOwner(t, paths))
				if test.scope == target.ScopeProject {
					input.ManagedCarrierClaims = []durablecarrier.ManagedCarrierClaim{claim}
				} else {
					registry, err := durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{claim})
					if err != nil {
						t.Fatal(err)
					}
					writeUnmanageRegistry(t, paths.CarrierClaimRegistryPath, registry)
				}
			}
			snapshot, err := durable.NewSnapshot(input)
			if err != nil {
				t.Fatal(err)
			}
			writeUnmanageState(t, paths.StatefilePath, snapshot)
			before := readUnmanageFile(t, paths.StatefilePath)
			var foreignState string
			if test.foreignBad {
				foreign := unmanageTestPaths(t, filepath.Join(root, "other"))
				writeUnmanageFile(t, foreign.ManifestPath, []byte("version=1\ntargets=['claude-code']\n"))
				fixture := newUnmanageTestFixture(t, "foreign", "foreign@official", target.ScopeGlobal)
				claim := unmanageTestClaim(t, fixture, unmanageTestOwner(t, foreign))
				registry, err := durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{claim})
				if err != nil {
					t.Fatal(err)
				}
				writeUnmanageRegistry(t, paths.CarrierClaimRegistryPath, registry)
				foreignState = foreign.StatefilePath
				writeUnmanageFile(t, foreignState, []byte("{broken"))
			}
			host := filepath.Join(root, "home", ".claude", "settings.json")
			writeUnmanageFile(t, host, []byte(`{"unrelated":true}`))
			request := UnmanageExtensionRequest{ManifestPath: paths.ManifestPath, ID: "context7", Mode: UnmanageModeDryRun}
			if _, err := UnmanageExtension(t.Context(), request); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(readUnmanageFile(t, paths.StatefilePath), before) {
				t.Fatal("preview converted State")
			}
			if _, err := os.Stat(paths.GlobalMetadataDir()); !os.IsNotExist(err) {
				t.Fatal("preview created conversion storage", err)
			}
			request.Mode = UnmanageModeWrite
			result, err := UnmanageExtension(t.Context(), request)
			if err != nil || !result.HostStateRetained || result.ManifestStatus != UnmanageManifestStatusRemoved {
				t.Fatal("management release failed", err, result)
			}
			if string(readUnmanageFile(t, host)) != `{"unrelated":true}` {
				t.Fatal("management release changed host settings")
			}
			content := readUnmanageFile(t, paths.StatefilePath)
			if !test.claims {
				if !bytes.Equal(content, before) {
					t.Fatal("manifest-only release converted State")
				}
				if _, err := os.Stat(paths.GlobalMetadataDir()); !os.IsNotExist(err) {
					t.Fatal("manifest-only release created conversion storage", err)
				}
				return
			}
			current, err := statefile.Load(t.Context(), paths.StatefilePath)
			if err != nil || current.NativeDocumentCommit() != 7 || len(current.ManagedCarrierClaims()) != 0 {
				t.Fatal("metadata release changed unrelated State facts", err)
			}
			if test.foreignBad {
				var version struct{ Version int }
				if err := json.Unmarshal(content, &version); err != nil || version.Version != 10 {
					t.Fatal("failed conversion did not preserve the legacy format", err)
				}
				if string(readUnmanageFile(t, foreignState)) != "{broken" {
					t.Fatal("release changed the foreign State")
				}
				return
			}
			if _, err := statefile.UnmarshalImage(content); err != nil {
				t.Fatal("metadata release did not preserve a converted State successor", err)
			}
			if test.scope == target.ScopeGlobal {
				registry, err := carrierclaim.UnmarshalImage(readUnmanageFile(t, paths.CarrierClaimRegistryPath))
				if err != nil || len(registry.Registry().Claims()) != 0 {
					t.Fatal("global release did not retain the converted registry", err)
				}
			}
		})
	}
}
