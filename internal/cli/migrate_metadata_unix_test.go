//go:build darwin || linux

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/workflow/migrate"
)

type metadataMigrationOutput struct {
	SchemaVersion int      `json:"schema_version"`
	Mode          string   `json:"mode"`
	Action        string   `json:"action"`
	ManifestPath  string   `json:"manifest_path"`
	Statefiles    []string `json:"statefiles"`
	Targets       []string `json:"targets"`
	Error         string   `json:"error"`
	Assertions    []struct {
		InputPath    string `json:"input_path"`
		IdentityKey  string `json:"identity_key"`
		Witness      string `json:"witness"`
		ObservedPath string `json:"observed_path"`
	} `json:"reference_assertions"`
}

func TestMigrateMetadataJSONPreviewExecutionAndNoop(t *testing.T) {
	paths, before := metadataCLIValueFixture(t)
	for _, mode := range []string{"--dry-run", "--yes", "--yes"} {
		var stdout, stderr bytes.Buffer
		code := RunWithOptions([]string{"migrate", "metadata", "--manifest", paths.ManifestPath, mode, "--json"}, RunOptions{
			Context: t.Context(), Stdout: &stdout, Stderr: &stderr,
		})
		var result metadataMigrationOutput
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatalf("code=%d output=%q stderr=%q: %v", code, stdout.String(), stderr.String(), err)
		}
		if code != 0 || stderr.Len() != 0 || result.Error != "" || result.SchemaVersion != contractversion.MetadataMigrationJSON || result.ManifestPath != paths.ManifestPath || len(result.Statefiles) != 1 || len(result.Targets) != 3 {
			t.Fatalf("code=%d result=%+v stderr=%q", code, result, stderr.String())
		}
		if mode == "--dry-run" {
			if result.Mode != "dry-run" || result.Action != "convert" {
				t.Fatalf("preview=%+v", result)
			}
			requireMetadataCLIImages(t, before)
			continue
		}
		if result.Mode != "write" || result.Action != "converted" && result.Action != "already_converted" {
			t.Fatalf("execution=%+v", result)
		}
		content, err := os.ReadFile(paths.StatefilePath)
		if err != nil {
			t.Fatal(err)
		}
		image, err := statefile.UnmarshalImage(content)
		if err != nil || image.Snapshot().NativeDocumentCommit() != 21 {
			t.Fatal("CLI conversion changed State facts", err)
		}
	}
}

func TestMigrateMetadataDisclosesOperatorLookupAssertions(t *testing.T) {
	paths, before := metadataCLIValueFixture(t)
	alias := filepath.Join(filepath.Dir(paths.ManifestRoot), "selected-state")
	if err := os.Symlink(paths.StateDir, alias); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(alias, "state.json")
	var stdout, stderr bytes.Buffer
	code := runMigrate([]string{"metadata", "--manifest", paths.ManifestPath, "--reference", input, "--dry-run", "--json"}, &stdout, &stderr, commandOptions{context: t.Context()})
	var result metadataMigrationOutput
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err, stdout.String(), stderr.String())
	}
	if code != 0 || len(result.Assertions) != 1 {
		t.Fatalf("code=%d assertions=%+v stderr=%q", code, result.Assertions, stderr.String())
	}
	assertion := result.Assertions[0]
	if assertion.InputPath != input || assertion.ObservedPath != paths.StatefilePath || assertion.IdentityKey == "" || assertion.Witness == "" {
		t.Fatalf("operator input and observed lookup were conflated: %+v", assertion)
	}
	requireMetadataCLIImages(t, before)
}

func TestMigrateMetadataRejectsInvalidAuthorizationBeforePlanning(t *testing.T) {
	for _, args := range [][]string{
		{"metadata"},
		{"metadata", "--json"},
		{"metadata", "--dry-run", "--yes"},
		{"metadata", "--recover", "--reference", "/unused", "--dry-run"},
	} {
		var stdout, stderr bytes.Buffer
		planned := false
		code := runMigrate(args, &stdout, &stderr, commandOptions{
			context: t.Context(),
			metadataMigrationPlan: func(context.Context, migrate.MetadataInput) (*migrate.PreparedMetadata, error) {
				planned = true
				return nil, errors.New("unexpected metadata planning")
			},
		})
		if code != 2 || planned {
			t.Fatalf("args=%v code=%d planned=%t stdout=%q stderr=%q", args, code, planned, stdout.String(), stderr.String())
		}
	}
}

func TestMigrateMetadataJSONExecutionFailureKeepsTheDisclosedScope(t *testing.T) {
	paths, before := metadataCLIValueFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var stdout, stderr bytes.Buffer
	code := runMigrate([]string{"metadata", "--manifest", paths.ManifestPath, "--yes", "--json"}, &stdout, &stderr, commandOptions{
		context: ctx,
		metadataMigrationPlan: func(ctx context.Context, input migrate.MetadataInput) (*migrate.PreparedMetadata, error) {
			prepared, err := migrate.PlanMetadata(ctx, input)
			cancel()
			return prepared, err
		},
	})
	var result metadataMigrationOutput
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err, stdout.String())
	}
	if code != 1 || stderr.Len() != 0 || result.Error == "" || result.Action != "convert" || len(result.Statefiles) != 1 || len(result.Targets) != 3 {
		t.Fatalf("code=%d result=%+v stderr=%q", code, result, stderr.String())
	}
	requireMetadataCLIImages(t, before)
}

func metadataCLIValueFixture(t *testing.T) (daempaths.Paths, map[string][]byte) {
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
	manifest := filepath.Join(root, "project", "daem.toml")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte("version=1\ntargets=['pi']\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := daempaths.Resolve(manifest)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{NativeDocumentCommit: 21})
	if err != nil {
		t.Fatal(err)
	}
	state, err := statefile.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := ownershipstore.Marshal(ownership.EmptyRegistry())
	if err != nil {
		t.Fatal(err)
	}
	carriers, err := carrierclaim.Marshal(durablecarrier.EmptyGlobalCarrierClaims())
	if err != nil {
		t.Fatal(err)
	}
	before := map[string][]byte{paths.StatefilePath: state, paths.OwnershipRegistryPath: registry, paths.CarrierClaimRegistryPath: carriers}
	for path, content := range before {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return paths, before
}

func requireMetadataCLIImages(t *testing.T, images map[string][]byte) {
	t.Helper()
	for path, expected := range images {
		content, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(content, expected) {
			t.Fatalf("metadata %q changed: %v", path, err)
		}
	}
}
