package cli_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

const hookAssetInventoryManifest = `version = 1
targets = ["codex", "claude-code", "pi"]

[hook_asset.shared]
source = "absent/shared.sh"
kind = "file"
scope = "project"

[hook_asset.claude_only]
source = "absent/claude.sh"
kind = "file"
scope = "project"

[hook_asset.unused]
source = "absent/unused.sh"
kind = "file"
scope = "project"

[[hook]]
name = "shared"
event = "PreToolUse"
command = "{hook_file:shared}"
targets = ["claude-code", "codex"]
scope = "project"

[[hook]]
name = "claude_only"
event = "PreToolUse"
command = "{hook_file:claude_only} {hook_file:shared}"
targets = ["claude-code"]
scope = "project"
`

type hookAssetInventory struct {
	SchemaVersion int    `json:"schema_version"`
	Command       string `json:"command"`
	ResourceCount int    `json:"resource_count"`
	Resources     []struct {
		Kind        string `json:"kind"`
		Key         string `json:"key"`
		InstallName string `json:"install_name"`
		Source      string `json:"source"`
		Targets     string `json:"targets"`
		Scope       string `json:"scope"`
	} `json:"resources"`
}

func TestRunListHookAssetsPreservesEquivalentSelectionsAndEmptyTargets(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "daem.toml")
	testkit.WriteFile(t, root, "daem.toml", hookAssetInventoryManifest)
	all := []string{"hook/claude_only", "hook/shared", "hook_asset/claude_only", "hook_asset/shared", "hook_asset/unused"}
	tests := []struct {
		name    string
		targets []string
		want    []string
	}{
		{"implicit all", nil, all},
		{"explicit all", []string{"pi", "claude-code", "codex"}, all},
		{"codex", []string{"codex"}, []string{"hook/shared", "hook_asset/shared", "hook_asset/unused"}},
		{"claude", []string{"claude-code"}, all},
		{"irrelevant target", []string{"pi"}, []string{"hook_asset/unused"}},
	}
	baseline := readHookAssetInventory(t, manifest, nil)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := readHookAssetInventory(t, manifest, test.targets)
			var identities []string
			for _, row := range payload.Resources {
				identities = append(identities, row.Kind+"/"+row.Key)
				if row.Kind != "hook_asset" {
					continue
				}
				wantTargets := map[string]string{"shared": "codex,claude-code", "claude_only": "claude-code", "unused": ""}
				if row.Targets != wantTargets[row.Key] || row.Scope != "project" || row.InstallName != "-" || !strings.HasPrefix(row.Source, "local:absent/") {
					t.Fatalf("hook asset row = %+v", row)
				}
			}
			if !slices.Equal(identities, test.want) || payload.ResourceCount != len(test.want) {
				t.Fatalf("inventory = %q count=%d, want %q", identities, payload.ResourceCount, test.want)
			}
			if payload.SchemaVersion != 4 || payload.Command != "list resources" {
				t.Fatalf("inventory header = %+v, want resource inventory v4", payload)
			}
			if test.name == "explicit all" && !reflect.DeepEqual(payload, baseline) {
				t.Fatalf("equivalent target selections differ: implicit=%+v explicit=%+v", baseline, payload)
			}
		})
	}
	testkit.AssertFileContent(t, manifest, hookAssetInventoryManifest)
	testkit.AssertPathMissing(t, filepath.Join(root, "daem.lock.toml"))
	testkit.AssertPathMissing(t, filepath.Join(root, ".daem"))
}

func TestRunListHookAssetsHumanModesAndRemoveBoundary(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "daem.toml")
	testkit.WriteFile(t, root, "daem.toml", hookAssetInventoryManifest)
	for _, verbose := range []bool{false, true} {
		args := []string{"list", "resources", "--manifest", manifest, "--target", "pi"}
		if verbose {
			args = append(args, "--verbose")
		}
		var stdout, stderr bytes.Buffer
		if exit := testkit.RunCLI(args, &stdout, &stderr); exit != 0 || stderr.Len() != 0 {
			t.Fatalf("list exit=%d stdout=%q stderr=%q", exit, &stdout, &stderr)
		}
		for _, want := range []string{"resources: 1", `resource kind=hook_asset key="unused" install="-"`, `targets="" scope="project"`} {
			if !strings.Contains(stdout.String(), want) {
				t.Fatalf("verbose=%t output=%q, want %q", verbose, &stdout, want)
			}
		}
		if strings.Contains(stdout.String(), "absent/unused.sh") != verbose {
			t.Fatalf("verbose=%t changed source disclosure: %q", verbose, &stdout)
		}
	}

	var stdout, stderr bytes.Buffer
	if exit := testkit.RunCLI([]string{"remove", "hook_asset", "unused", "--manifest", manifest, "--dry-run"}, &stdout, &stderr); exit != 2 || stdout.Len() != 0 || stderr.Len() == 0 {
		t.Fatalf("unsupported asset remove exit=%d stdout=%q stderr=%q", exit, &stdout, &stderr)
	}
	testkit.AssertFileContent(t, manifest, hookAssetInventoryManifest)
	testkit.AssertPathMissing(t, filepath.Join(root, "daem.lock.toml"))
	testkit.AssertPathMissing(t, filepath.Join(root, ".daem"))
}

func TestRunListHookAssetsDoesNotRequireOperationalAdmissionOrExpandSelection(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "daem.toml")
	testkit.WriteFile(t, root, "daem.toml", `version = 1
targets = ["codex", "pi"]
[hook_asset.guard]
source = "absent/guard.sh"
kind = "file"
[hook_asset.unused]
source = "absent/unused.sh"
kind = "file"
[[hook]]
name = "pi_reference"
event = "PreToolUse"
command = "{hook_file:guard}"
targets = ["pi"]
`)
	payload := readHookAssetInventory(t, manifest, []string{"pi"})
	if len(payload.Resources) != 3 || payload.Resources[1].Kind != "hook_asset" || payload.Resources[1].Targets != "pi" || payload.Resources[2].Targets != "" {
		t.Fatalf("declared Pi reference inventory = %+v", payload)
	}

	var stdout, stderr bytes.Buffer
	exit := testkit.RunCLI([]string{"list", "resources", "--manifest", manifest, "--target", "claude-code", "--json"}, &stdout, &stderr)
	var failure struct {
		ResultType string `json:"result_type"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &failure); err != nil {
		t.Fatalf("decode selection failure: %v; stdout=%q stderr=%q", err, &stdout, &stderr)
	}
	if exit != 1 || failure.ResultType != "cli_error" {
		t.Fatalf("unavailable target exit=%d failure=%+v", exit, failure)
	}
	testkit.AssertPathMissing(t, filepath.Join(root, "daem.lock.toml"))
	testkit.AssertPathMissing(t, filepath.Join(root, ".daem"))
}

func TestRunListHookAssetsEmptyInventoryStillUsesVersion4(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "daem.toml")
	testkit.WriteFile(t, root, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
	payload := readHookAssetInventory(t, manifest, nil)
	if payload.SchemaVersion != 4 || payload.ResourceCount != 0 || payload.Resources == nil || len(payload.Resources) != 0 {
		t.Fatalf("empty inventory = %+v, want version 4 with an empty array", payload)
	}
}

func readHookAssetInventory(t *testing.T, manifest string, targets []string) hookAssetInventory {
	t.Helper()
	args := []string{"list", "resources", "--manifest", manifest, "--json"}
	for _, selected := range targets {
		args = append(args, "--target", selected)
	}
	var stdout, stderr bytes.Buffer
	if exit := testkit.RunCLI(args, &stdout, &stderr); exit != 0 || stderr.Len() != 0 {
		t.Fatalf("list exit=%d stdout=%q stderr=%q", exit, &stdout, &stderr)
	}
	var payload hookAssetInventory
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}
