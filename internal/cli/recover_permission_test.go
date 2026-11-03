//go:build darwin || linux

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/test/ownershiptest"
)

type permissionCLIOutput struct {
	SchemaVersion int    `json:"schema_version"`
	AuthorityKind string `json:"authority_kind"`
	Phase         string `json:"phase"`
	HasErrors     bool   `json:"has_errors"`
	Protections   []struct {
		Settlement string `json:"settlement"`
		Reason     string `json:"reason"`
		Targets    []struct {
			State    string `json:"state"`
			Reason   string `json:"reason"`
			Expanded bool   `json:"observed_access_expansion"`
		} `json:"targets"`
	} `json:"protections"`
}

func runPermissionRecoverCLI(t *testing.T, manifest string, arguments ...string) (permissionCLIOutput, int) {
	t.Helper()
	args := append([]string{"recover", "--json", "--manifest", manifest}, arguments...)
	var stdout, stderr bytes.Buffer
	code := RunWithOptions(args, RunOptions{Stdout: &stdout, Stderr: &stderr})
	var result permissionCLIOutput
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("invalid recovery result (%d): %v, %s, %s", code, err, stdout.String(), stderr.String())
	}
	if result.SchemaVersion != contractversion.RecoveryJSON || result.AuthorityKind != "permission_safety" {
		t.Fatalf("wrong authority projection: %+v stderr=%s", result, stderr.String())
	}
	return result, code
}

func TestRecoverPermissionCLIPlansWithoutEffectsThenContainsMalformedPayload(t *testing.T) {
	fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml", ".pi/settings.json")
	before, _ := os.ReadFile(fixture.Paths.OwnershipRegistryPath)
	payload, _ := os.ReadFile(fixture.Documents[0])
	state, _ := os.ReadFile(fixture.Paths.StatefilePath)
	plan, code := runPermissionRecoverCLI(t, fixture.Paths.ManifestPath, "--dry-run")
	if code != 0 || plan.HasErrors || plan.Phase != "planned" || len(plan.Protections) != 1 {
		t.Fatalf("dry-run: %d %+v", code, plan)
	}
	current, _ := os.ReadFile(fixture.Paths.OwnershipRegistryPath)
	info, _ := os.Stat(fixture.Documents[0])
	if !bytes.Equal(before, current) || info.Mode().Perm() != 0o644 {
		t.Fatal("dry-run modified protection or permissions")
	}
	result, code := runPermissionRecoverCLI(t, fixture.Paths.ManifestPath, "--yes")
	if code != 0 || result.HasErrors || result.Phase != "completed" || result.Protections[0].Settlement != "cleared" {
		t.Fatalf("write: %d %+v", code, result)
	}
	for _, target := range result.Protections[0].Targets {
		if target.State != "secure" || !target.Expanded {
			t.Fatalf("lost per-target permission facts: %+v", target)
		}
	}
	after, _ := os.ReadFile(fixture.Documents[0])
	afterState, _ := os.ReadFile(fixture.Paths.StatefilePath)
	if !bytes.Equal(payload, after) || !bytes.Equal(state, afterState) {
		t.Fatal("Recover rewrote host or state payload")
	}
	rows, err := fixture.Store.LoadPendingProtections(t.Context())
	if err != nil || len(rows) != 0 {
		t.Fatal("CLI did not settle permission authority", err)
	}
}

func TestRecoverPermissionCLIKeepsPartialResultsDistinctAndRetries(t *testing.T) {
	fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml", ".pi/settings.json")
	if err := os.Chmod(fixture.Documents[0], 0o744); err != nil {
		t.Fatal(err)
	}
	result, code := runPermissionRecoverCLI(t, fixture.Paths.ManifestPath, "--yes")
	if code == 0 || !result.HasErrors || result.Phase != "pending" || result.Protections[0].Settlement != "pending" {
		t.Fatalf("partial result: %d %+v", code, result)
	}
	states := map[string]int{}
	for _, target := range result.Protections[0].Targets {
		states[target.State]++
	}
	if states["secure"] != 1 || states["blocked"] != 1 {
		t.Fatalf("partial target facts: %+v", states)
	}
	if err := os.Chmod(fixture.Documents[0], 0o644); err != nil {
		t.Fatal(err)
	}
	retry, code := runPermissionRecoverCLI(t, fixture.Paths.ManifestPath, "--yes")
	if code != 0 || retry.HasErrors || retry.Phase != "completed" {
		t.Fatalf("retry: %d %+v", code, retry)
	}
	for _, path := range fixture.Documents {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("retry did not retain containment: %v", err)
		}
	}
}

func TestPermissionDryRunShowsMetadataAndCohortBlockersWithoutEffects(t *testing.T) {
	for _, cause := range []string{"mode", "cohort", "namespace"} {
		t.Run(cause, func(t *testing.T) {
			fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml")
			want := "protected_mode_changed"
			if cause == "mode" {
				if err := os.Chmod(fixture.Documents[0], 0o744); err != nil {
					t.Fatal(err)
				}
			} else if cause == "namespace" {
				want = "protected_namespace_unqualified"
				parent := filepath.Dir(fixture.Documents[0])
				if err := os.Rename(parent, parent+"-old"); err != nil {
					t.Fatal(err)
				}
				other := t.TempDir()
				if err := os.WriteFile(filepath.Join(other, "config.toml"), []byte("private test payload"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(other, parent); err != nil {
					t.Fatal(err)
				}
			} else {
				want = "protection_cohort_changed"
				current, err := fixture.Store.LoadProtectionRegistry(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				current, err = ownership.NewRegistryWithProtections(current.Claims(), nil, current.PendingProtections())
				if err != nil {
					t.Fatal(err)
				}
				content, err := ownershipstore.Marshal(current)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(fixture.Store.Path(), content, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := os.Stat(fixture.Documents[0])
			registry, _ := os.ReadFile(fixture.Store.Path())
			plan, code := runPermissionRecoverCLI(t, fixture.Paths.ManifestPath, "--dry-run")
			if code == 0 || !plan.HasErrors || plan.Protections[0].Targets[0].State != "blocked" || plan.Protections[0].Targets[0].Reason != want {
				t.Fatalf("lost dry-run blocker: %d %+v", code, plan)
			}
			after, _ := os.Stat(fixture.Documents[0])
			afterRegistry, _ := os.ReadFile(fixture.Store.Path())
			if before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) || !bytes.Equal(registry, afterRegistry) {
				t.Fatal("blocked dry-run changed metadata or Registry")
			}
			var human, diagnostics bytes.Buffer
			code = RunWithOptions([]string{"recover", "--dry-run", "--manifest", fixture.Paths.ManifestPath}, RunOptions{Stdout: &human, Stderr: &diagnostics})
			if code == 0 || !strings.Contains(human.String(), want) {
				t.Fatalf("human output lost safe cause %q: %s %s", want, human.String(), diagnostics.String())
			}
			if strings.Contains(human.String(), "private test payload") || strings.Contains(human.String(), "malformed host payload") {
				t.Fatal("diagnostic exposed host values")
			}
		})
	}
}

func TestPermissionRecoveryPublicationFailureHasItsOwnSafeCause(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("requires a non-privileged publication attempt")
	}
	fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml")
	directory := filepath.Dir(fixture.Store.Path())
	if err := os.Chmod(directory, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(directory, 0o700) })
	result, code := runPermissionRecoverCLI(t, fixture.Paths.ManifestPath, "--yes")
	if code == 0 || !result.HasErrors || result.Protections[0].Reason != "protection_publication_failed" {
		t.Fatalf("publication cause was collapsed: %d %+v", code, result)
	}
}
