package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/workflow/migrate"
)

func TestMigrateStateExecutionFailureResults(t *testing.T) {
	for _, failure := range []string{"cancelled", "stale_source"} {
		for _, mode := range []string{"json", "human"} {
			t.Run(failure+"/"+mode, func(t *testing.T) {
				fixture := newMigrationOutputFixture(t)
				ctx, cancel := context.WithCancel(t.Context())
				t.Cleanup(cancel)
				expectedSource := fixture.content
				var plan migrate.StateDisclosure
				plans := 0
				options := commandOptions{
					context: ctx,
					stateMigrationPlan: func(ctx context.Context, input migrate.StateInput) (*migrate.PreparedState, error) {
						prepared, err := migrate.PlanState(ctx, input)
						if err != nil {
							return nil, err
						}
						plans++
						plan = prepared.Disclosure()
						if failure == "cancelled" {
							cancel()
						} else {
							expectedSource = append(bytes.Clone(fixture.content), '\n')
							if err := os.WriteFile(fixture.legacy, expectedSource, 0o600); err != nil {
								prepared.Close()
								t.Fatal(err)
							}
						}
						return prepared, nil
					},
				}
				args := []string{"state", "--manifest", fixture.manifest, "--yes"}
				if mode == "json" {
					args = append(args, "--json")
				}
				var stdout, stderr bytes.Buffer

				code := runMigrate(args, &stdout, &stderr, options)
				if code != 1 || plans != 1 || plan.Action != "migrate" {
					t.Fatalf("code=%d plans=%d action=%q stderr=%q", code, plans, plan.Action, stderr.String())
				}
				if mode == "json" {
					result := decodeMigrationOutput(t, &stdout)
					if result.Error == "" || result.Action != plan.Action || result.Mode != "write" ||
						result.SchemaVersion != contractversion.StateMigrationJSON ||
						result.ManifestPath != fixture.manifest || result.SourceStatefile != fixture.legacy ||
						result.DestinationStatefile != plan.DestinationStatefile {
						t.Errorf("failed execution result = %+v, plan = %+v", result, plan)
					}
					if stderr.Len() != 0 {
						t.Errorf("post-envelope execution failure wrote stderr: %q", stderr.String())
					}
				} else if stdout.Len() == 0 || stderr.Len() == 0 {
					t.Errorf("human failure lost disclosure or diagnostic: stdout=%q stderr=%q", stdout.String(), stderr.String())
				}

				got, err := os.ReadFile(fixture.legacy)
				if err != nil || !bytes.Equal(got, expectedSource) {
					t.Fatalf("failed execution replaced source: content=%q err=%v", got, err)
				}
				for _, path := range []string{plan.DestinationStatefile, plan.OutputRegistryPath, plan.CarrierRegistryPath} {
					assertCLIPathMissing(t, path)
				}

				var retryOut, retryErr bytes.Buffer
				if code := RunWithOptions([]string{"migrate", "state", "--manifest", fixture.manifest, "--yes", "--json"}, RunOptions{
					Context: t.Context(), Stdout: &retryOut, Stderr: &retryErr,
				}); code != 0 || retryErr.Len() != 0 {
					t.Fatalf("fresh retry failed: code=%d stderr=%q", code, retryErr.String())
				}
				if result := decodeMigrationOutput(t, &retryOut); result.Error != "" || result.Action != "migrated" {
					t.Fatalf("fresh retry result = %+v", result)
				}
				if _, err := statefile.Load(t.Context(), plan.DestinationStatefile); err != nil {
					t.Fatalf("retry did not publish valid state: %v", err)
				}
			})
		}
	}
}

func TestMigrateStateJSONPlanningFailureKeepsDiagnostic(t *testing.T) {
	fixture := newMigrationOutputFixture(t)
	if err := os.Remove(fixture.legacy); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	code := RunWithOptions([]string{"migrate", "state", "--manifest", fixture.manifest, "--yes", "--json"}, RunOptions{
		Context: t.Context(), Stdout: &stdout, Stderr: &stderr,
	})
	if code != 1 {
		t.Fatalf("planning failure: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	refusalDiagnosticText(t, "migrate state", &stdout, &stderr, true)
	assertCLIPathMissing(t, fixture.destination)
}

func TestMigrateStateJSONWriteFailureKeepsDiagnosticAndCommittedState(t *testing.T) {
	fixture := newMigrationOutputFixture(t)
	var stderr bytes.Buffer

	code := RunWithOptions([]string{"migrate", "state", "--manifest", fixture.manifest, "--yes", "--json"}, RunOptions{
		Context: t.Context(), Stdout: migrationFailedDisclosure{}, Stderr: &stderr,
	})
	if code != 1 || stderr.Len() == 0 {
		t.Fatalf("result write failure: code=%d stderr=%q", code, stderr.String())
	}
	if _, err := statefile.Load(t.Context(), fixture.destination); err != nil {
		t.Fatalf("output failure lost committed state: %v", err)
	}
	if _, found, err := statefile.LoadRelocation(t.Context(), fixture.legacy); err != nil || !found {
		t.Fatalf("output failure lost relocation receipt: found=%v err=%v", found, err)
	}
}

type migrationOutputFixture struct {
	manifest    string
	legacy      string
	destination string
	content     []byte
}

func newMigrationOutputFixture(t *testing.T) migrationOutputFixture {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	for _, kind := range []string{"CONFIG", "STATE", "CACHE", "DATA"} {
		t.Setenv("XDG_"+kind+"_HOME", filepath.Join(root, strings.ToLower(kind)))
	}
	manifest := filepath.Join(root, "config", "daem", "daem.toml")
	legacy := filepath.Join(filepath.Dir(manifest), ".daem", "state.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte("version = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	content, err := statefile.Marshal(durable.EmptySnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return migrationOutputFixture{
		manifest: manifest, legacy: legacy,
		destination: filepath.Join(root, "state", "daem", "state.json"), content: content,
	}
}

type migrationOutputJSON struct {
	SchemaVersion        int    `json:"schema_version"`
	Mode                 string `json:"mode"`
	Action               string `json:"action"`
	ManifestPath         string `json:"manifest_path"`
	SourceStatefile      string `json:"source_statefile"`
	DestinationStatefile string `json:"destination_statefile"`
	OutputRegistryPath   string `json:"output_registry_path"`
	CarrierRegistryPath  string `json:"carrier_registry_path"`
	OutputClaims         int    `json:"output_claims"`
	CarrierClaims        int    `json:"carrier_claims"`
	ManagedPaths         int    `json:"managed_paths"`
	ManagedAggregates    int    `json:"managed_aggregates"`
	Error                string `json:"error"`
}

func decodeMigrationOutput(t *testing.T, output io.Reader) migrationOutputJSON {
	t.Helper()
	decoder := json.NewDecoder(output)
	decoder.DisallowUnknownFields()
	var result migrationOutputJSON
	if err := decoder.Decode(&result); err != nil {
		t.Fatalf("migration output is not its typed JSON result: %v", err)
	}
	if err := decoder.Decode(&migrationOutputJSON{}); !errors.Is(err, io.EOF) {
		t.Fatalf("migration output contains trailing data: %v", err)
	}
	return result
}
