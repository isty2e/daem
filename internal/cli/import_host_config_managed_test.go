package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	codexhostpath "github.com/isty2e/daem/internal/output/hostpath/codex"
	pihostpath "github.com/isty2e/daem/internal/output/hostpath/pi"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
)

func TestImportHostConfigAfterManagedApplyPreservesScopeOwnership(t *testing.T) {
	for _, selected := range []string{"pi", "codex"} {
		for _, scope := range []string{"project", "global"} {
			for _, mode := range []string{"keys", "document"} {
				t.Run(fmt.Sprintf("%s/%s/%s", selected, scope, mode), func(t *testing.T) {
					root, manifest, _, _ := captureCLIFixture(t, `{}`)
					hostRoot := filepath.Join(root, "."+selected)
					if scope == "global" {
						hostRoot = filepath.Join(root, "native-home")
						if selected == "pi" {
							t.Setenv(pihostpath.AgentRootEnvironmentVariable, hostRoot)
						} else {
							t.Setenv(codexhostpath.HomeEnvironmentVariable, hostRoot)
						}
					}
					if err := os.MkdirAll(hostRoot, 0o700); err != nil {
						t.Fatal(err)
					}
					host := filepath.Join(hostRoot, "captured.jsonc")
					if err := os.WriteFile(host, []byte(`{"nested":{"leaf":1},"outside":8}`), 0o600); err != nil {
						t.Fatal(err)
					}
					args := []string{"import", "host-config", "chosen", "--manifest", manifest, "--target", selected, "--scope", scope, "--host-path", "captured.jsonc", "--ownership", mode}
					if mode == "keys" {
						args = append(args, "--key", "/nested")
					}
					fingerprint := captureFingerprint(t, args)
					if code, out, err := runCaptureCLI(t, append(args, "--yes", "--expect-plan", fingerprint)); code != 0 {
						t.Fatal(code, out, err)
					}
					for _, command := range [][]string{{"lock", "--manifest", manifest}, {"apply", "--manifest", manifest, "--manage-existing", "--yes"}} {
						if code, out, err := runCaptureCLI(t, command); code != 0 {
							t.Fatal(command, code, out, err)
						}
					}
					paths, err := daempaths.Resolve(manifest)
					if err != nil {
						t.Fatal(err)
					}
					reader, err := ownershipstore.NewRecoveryReader(paths.OwnershipRegistryPath)
					if err != nil {
						t.Fatal(err)
					}
					registry, err := reader.LoadForClaimRemovals(t.Context(), nil, recovery.MaximumPhysicalPathDepth, recovery.NewPhysicalCaptureBudget(nil))
					if err != nil {
						t.Fatal(err)
					}
					wantClaims := 0
					if scope == "global" {
						wantClaims = 1
					}
					if len(registry.Claims()) != wantClaims || len(registry.DocumentBindings()) != 1 {
						t.Fatal("fixture does not represent real scope-specific ownership", registry.Claims(), registry.DocumentBindings())
					}
					beforeManifest, err := os.ReadFile(manifest)
					if err != nil {
						t.Fatal(err)
					}
					environment, err := declarationmanifest.Decode(beforeManifest)
					if err != nil {
						t.Fatal(err)
					}
					local, _ := environment.HostConfigs()[0].Source().Local()
					changed := []byte(`{"nested":{"leaf":2},"outside":9}`)
					if err := os.WriteFile(host, changed, 0o600); err != nil {
						t.Fatal(err)
					}
					update := []string{"import", "host-config", "chosen", "--manifest", manifest, "--update-source"}
					if scope == "global" {
						original, err := os.ReadFile(paths.OwnershipRegistryPath)
						if err != nil {
							t.Fatal(err)
						}
						missing, err := ownership.NewRegistry(nil, registry.DocumentBindings()...)
						if err != nil {
							t.Fatal(err)
						}
						encoded, err := ownershipstore.Marshal(missing)
						if err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(paths.OwnershipRegistryPath, encoded, 0o600); err != nil {
							t.Fatal(err)
						}
						if code, _, _ := runCaptureCLI(t, append(append([]string{}, update...), "--dry-run")); code != 1 {
							t.Fatal("global capture accepted missing active claims", code)
						}
						if err := os.WriteFile(paths.OwnershipRegistryPath, original, 0o600); err != nil {
							t.Fatal(err)
						}
					}
					type unchangedArtifact struct {
						path    string
						content []byte
						info    os.FileInfo
					}
					var controls []unchangedArtifact
					for _, path := range []string{paths.LockfilePath, paths.StatefilePath, paths.OwnershipRegistryPath, host} {
						content, err := os.ReadFile(path)
						if err != nil {
							t.Fatal(err)
						}
						info, err := os.Stat(path)
						if err != nil {
							t.Fatal(err)
						}
						controls = append(controls, unchangedArtifact{path, content, info})
					}
					fingerprint = captureFingerprint(t, update)
					if code, out, err := runCaptureCLI(t, append(update, "--yes", "--expect-plan", fingerprint)); code != 0 {
						t.Fatal(code, out, err)
					}
					for _, control := range controls {
						content, err := os.ReadFile(control.path)
						if err != nil || !bytes.Equal(content, control.content) {
							t.Fatal("capture changed control/host bytes", control.path, err)
						}
						info, err := os.Stat(control.path)
						if err != nil || !os.SameFile(control.info, info) || !control.info.ModTime().Equal(info.ModTime()) || control.info.Mode() != info.Mode() {
							t.Fatal("capture replaced or changed control/host metadata", control.path, err)
						}
					}
					captured, err := os.ReadFile(local.Path())
					if err != nil {
						t.Fatal(err)
					}
					var values struct {
						Nested struct {
							Leaf int `json:"leaf"`
						} `json:"nested"`
						Outside *int `json:"outside"`
					}
					if err := json.Unmarshal(captured, &values); err != nil || values.Nested.Leaf != 2 || mode == "keys" && values.Outside != nil || mode == "document" && (values.Outside == nil || *values.Outside != 9) {
						t.Fatal("capture selected incorrect managed values", err)
					}
					for _, command := range [][]string{{"lock", "--manifest", manifest}, {"apply", "--manifest", manifest, "--yes"}, {"status", "--manifest", manifest, "--check", "--json"}} {
						if code, out, err := runCaptureCLI(t, command); code != 0 {
							t.Fatal("ordinary settlement after capture failed", command, code, out, err)
						}
					}
				})
			}
		}
	}
}
