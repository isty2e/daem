package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	daempaths "github.com/isty2e/daem/internal/paths"
)

func TestManifestHintsAfterCreationExecuteLocalPreview(t *testing.T) {
	for _, command := range []string{"init", "import"} {
		t.Run(command, func(t *testing.T) {
			root, cwd := manifestHintWorkspace(t)
			writeHintManifest(t, filepath.Join(root, "config", "daem", "daem.toml"))
			args := []string{command}
			if command == "import" {
				if err := os.WriteFile("AGENTS.md", []byte("Local import fixture.\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--target", "codex", "--scope", "project")
			}
			var stdout, stderr bytes.Buffer
			if code := runWithoutTerminal(append(slices.Clone(args), "--dry-run"), &stdout, &stderr); code != 0 || stderr.Len() != 0 {
				t.Fatalf("preview code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
			}
			if _, err := os.Stat(filepath.Join(cwd, "daem.toml")); !os.IsNotExist(err) {
				t.Fatalf("creation preview wrote a manifest: %v", err)
			}
			stdout.Reset()
			stderr.Reset()
			if code := runWithoutTerminal(args, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
				t.Fatalf("create code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
			}
			argv := manifestHintArgv(t, stdout.String(), "next: run ")
			if !reflect.DeepEqual(argv, []string{"lock", "--dry-run"}) {
				t.Fatalf("post-create next argv=%q, want local lock preview", argv)
			}
			before := snapshotApplyInputFiles(t, root)
			stdout.Reset()
			stderr.Reset()
			if code := runWithoutTerminal(append(argv, "--json"), &stdout, &stderr); code != 0 || stderr.Len() != 0 {
				t.Fatalf("next preview code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
			}
			var result struct {
				ManifestPath string `json:"manifest_path"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.ManifestPath != filepath.Join(cwd, "daem.toml") {
				t.Fatalf("post-create preview selected=%q, want cwd manifest", result.ManifestPath)
			}
			if !reflect.DeepEqual(before, snapshotApplyInputFiles(t, root)) {
				t.Fatal("post-create preview changed fixture files")
			}
		})
	}
}

func TestManifestHintsAfterAuthoringExecuteApplyPreview(t *testing.T) {
	root, cwd := manifestHintWorkspace(t)
	writeHintManifest(t, filepath.Join(cwd, "daem.toml"))
	if err := os.WriteFile("source.md", []byte("Project instructions.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "instruction", "guide", "source.md", "--target", "codex"},
		{"remove", "instruction", "guide"},
	} {
		var stdout, stderr bytes.Buffer
		if code := runWithoutTerminal(args, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
			t.Fatalf("authoring %q code=%d stdout=%q stderr=%q", args, code, &stdout, &stderr)
		}
		argv := manifestHintArgv(t, stdout.String(), "next: run ")
		if !reflect.DeepEqual(argv, []string{"apply", "--dry-run"}) {
			t.Fatalf("authoring next argv=%q, want local apply preview", argv)
		}
		before := snapshotApplyInputFiles(t, root)
		stdout.Reset()
		stderr.Reset()
		if code := runWithoutTerminal(append(argv, "--json"), &stdout, &stderr); code != 0 || stderr.Len() != 0 {
			t.Fatalf("apply preview code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
		}
		var result struct {
			Lockfile struct {
				Path string `json:"path"`
			} `json:"lockfile"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Lockfile.Path != filepath.Join(cwd, "daem.lock.toml") {
			t.Fatalf("apply preview lock=%q, want cwd workspace", result.Lockfile.Path)
		}
		if !reflect.DeepEqual(before, snapshotApplyInputFiles(t, root)) {
			t.Fatal("apply preview changed fixture files")
		}
	}
}

func TestManifestHintsPreserveTargetAndScopeArguments(t *testing.T) {
	_, cwd := manifestHintWorkspace(t)
	path := filepath.Join(cwd, "daem.toml")
	writeHintManifest(t, path)
	var output strings.Builder
	printMissingResourceSelectionHint(&output, path, missingSelectionForTest())
	want := []string{"remove", "instruction", "project name", "--target", "pi", "--target", "codex agent", "--scope", "global"}
	if got := manifestHintArgv(t, output.String(), "hint: available selection: "); !reflect.DeepEqual(got, want) {
		t.Fatalf("remove next argv=%q, want %q", got, want)
	}
	output.Reset()
	printReconciliationInspectionHint(&output, path, path, targetFlagValues{"codex", "pi"}, true)
	want = []string{"status", "--target", "codex", "--target", "pi", "--verbose"}
	if got := manifestHintArgv(t, output.String(), "next: inspect with "); !reflect.DeepEqual(got, want) {
		t.Fatalf("inspection argv=%q, want %q", got, want)
	}
	output.Reset()
	printReconciliationInspectionHint(&output, path, path, targetFlagValues{"codex", "pi"}, false)
	if strings.Contains(output.String(), path) || strings.Contains(output.String(), "next: inspect with ") {
		t.Fatalf("nonverbose inspection disclosed a concrete path: %q", &output)
	}
}

func TestManifestHintsKeepUserDefaultCreationExplicit(t *testing.T) {
	root, _ := manifestHintWorkspace(t)
	path := filepath.Join(root, "config", "daem", "daem.toml")
	var output strings.Builder
	printMissingManifestInitHint(&output, "", os.ErrNotExist)
	want := []string{"init", "--manifest", path, "--dry-run"}
	if got := manifestHintArgv(t, output.String(), "next: run "); !reflect.DeepEqual(got, want) {
		t.Fatalf("user-default creation argv=%q, want %q", got, want)
	}
}

func TestManifestHintsCanUseUserManifestAsCWD(t *testing.T) {
	root, _ := manifestHintWorkspace(t)
	path := filepath.Join(root, "config", "daem", "daem.toml")
	writeHintManifest(t, path)
	t.Chdir(filepath.Dir(path))
	implicit, err := daempaths.Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := daempaths.Resolve(implicit.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if implicit.ManifestOrigin != daempaths.ManifestOriginCWD || !implicit.ProjectPlacementAllowed() ||
		!explicit.ProjectPlacementAllowed() || implicit.StateDir != explicit.StateDir || implicit.CacheDir != explicit.CacheDir {
		t.Fatal("cwd and explicit user-manifest selection do not share storage and project admission")
	}
	for _, request := range []string{"", implicit.ManifestPath} {
		var output strings.Builder
		printLockCommandHint(&output, implicit.ManifestPath, request)
		if got := manifestHintArgv(t, output.String(), "next: run "); !reflect.DeepEqual(got, []string{"lock"}) {
			t.Fatalf("cwd user-manifest next argv=%q for request %q", got, request)
		}
	}
}
