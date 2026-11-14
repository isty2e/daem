//go:build darwin || linux

package metadataconversion_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
)

func TestMetadataCLIRecoversTheOriginalTransactionFromAnotherSelection(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for key, directory := range map[string]string{
		"HOME": "home", "XDG_CONFIG_HOME": "config", "XDG_STATE_HOME": "state", "XDG_DATA_HOME": "data",
	} {
		t.Setenv(key, filepath.Join(root, directory))
	}
	paths, err := daempaths.Resolve(filepath.Join(root, "original", "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	state, err := statefile.Marshal(durable.EmptySnapshot())
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
	before := map[string][]byte{
		paths.StatefilePath: state, paths.OwnershipRegistryPath: registry, paths.CarrierClaimRegistryPath: carriers,
	}
	for path, content := range before {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(paths.ManifestPath, []byte("version=1\ntargets=['pi']\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	process := exec.CommandContext(t.Context(), executable, "-test.run=^TestMetadataPublisherCrashHelper$")
	process.Env = append(os.Environ(), "DAEM_METADATA_CRASH_MANIFEST="+paths.ManifestPath, "DAEM_METADATA_CRASH_DATA="+paths.DataDir)
	output, err := process.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 86 {
		t.Fatal("publication fixture did not lose its process at the expected mixed point", err, string(output))
	}
	if !errors.Is(recoverygate.RequireGlobalMetadataClear(t.Context(), paths), recoverygate.ErrGlobalMetadataFence) {
		t.Fatal("process loss did not retain the global conversion fence")
	}
	unrelated := filepath.Join(root, "different", "daem.toml")
	for _, arguments := range [][]string{
		{"status", "--manifest", paths.ManifestPath, "--json"},
		{"apply", "--manifest", paths.ManifestPath, "--dry-run", "--json"},
		{"recover", "--manifest", unrelated, "--dry-run", "--json"},
		{"migrate", "metadata", "--manifest", unrelated, "--dry-run", "--json"},
	} {
		var stdout, stderr bytes.Buffer
		code := cli.RunWithOptions(arguments, cli.RunOptions{
			Context: t.Context(), Stdout: &stdout, Stderr: &stderr,
		})
		var refusal struct {
			Error    struct{ Kind, Message string }
			Guidance []struct {
				Kind string
				Argv []string
			}
		}
		if err := json.Unmarshal(stdout.Bytes(), &refusal); err != nil {
			t.Fatal(arguments, err, stdout.String(), stderr.String())
		}
		expected := []string{"daem", "migrate", "metadata", "--recover", "--dry-run"}
		hasRecovery := slices.ContainsFunc(refusal.Guidance, func(item struct {
			Kind string
			Argv []string
		},
		) bool {
			return item.Kind == "command" && slices.Equal(item.Argv, expected)
		})
		if code != 1 || stderr.Len() != 0 || refusal.Error.Kind != "operation" ||
			!strings.Contains(refusal.Error.Message, recoverygate.ErrGlobalMetadataFence.Error()) || !hasRecovery {
			t.Fatalf("metadata refusal lost its cause or CLI recovery route: args=%v code=%d result=%+v stderr=%q", arguments, code, refusal, stderr.String())
		}
	}
	for _, mode := range []string{"--dry-run", "--yes"} {
		var stdout, stderr bytes.Buffer
		code := cli.RunWithOptions([]string{"migrate", "metadata", "--manifest", unrelated, "--recover", mode, "--json"}, cli.RunOptions{
			Context: t.Context(), Stdout: &stdout, Stderr: &stderr,
		})
		var result struct {
			Action       string   `json:"action"`
			ManifestPath string   `json:"manifest_path"`
			Statefiles   []string `json:"statefiles"`
			Targets      []string `json:"targets"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatal(err, stdout.String(), stderr.String())
		}
		if code != 0 || stderr.Len() != 0 || result.Action != "rollback" || result.ManifestPath != paths.ManifestPath || len(result.Statefiles) != 1 || len(result.Targets) != 3 {
			t.Fatalf("code=%d result=%+v stderr=%q", code, result, stderr.String())
		}
		if mode == "--dry-run" && !errors.Is(recoverygate.RequireGlobalMetadataClear(t.Context(), paths), recoverygate.ErrGlobalMetadataFence) {
			t.Fatal("recovery preview retired the conversion fence")
		}
	}
	for path, expected := range before {
		content, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(content, expected) {
			t.Fatalf("recovery did not restore %q: %v", path, err)
		}
	}
	if err := recoverygate.RequireGlobalMetadataClear(t.Context(), paths); err != nil {
		t.Fatal("completed CLI recovery left a global fence", err)
	}
	var stdout, stderr bytes.Buffer
	code := cli.RunWithOptions([]string{"migrate", "metadata", "--manifest", unrelated, "--recover", "--yes", "--json"}, cli.RunOptions{
		Context: t.Context(), Stdout: &stdout, Stderr: &stderr,
	})
	var repeated struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &repeated); err != nil || code != 0 || stderr.Len() != 0 || repeated.Action != "none" {
		t.Fatalf("completed recovery was not an idempotent no-op: code=%d output=%q stderr=%q error=%v", code, stdout.String(), stderr.String(), err)
	}
}
