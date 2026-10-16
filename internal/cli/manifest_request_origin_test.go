package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	daempaths "github.com/isty2e/daem/internal/paths"
)

func TestManifestHintsPreserveRequestedUserOriginAfterEditingScope(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		name := "implicit user fallback"
		if explicit {
			name = "explicit user path"
		}
		t.Run(name, func(t *testing.T) {
			root, _ := manifestHintWorkspace(t)
			manifestPath := filepath.Join(root, "config", "daem", "daem.toml")
			writeHintManifest(t, manifestPath)
			sourcePath := filepath.Join(root, "source.md")
			if err := os.WriteFile(sourcePath, []byte("User instructions.\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			request := ""
			var selector []string
			if explicit {
				request = manifestPath
				selector = []string{"--manifest", request}
			}
			var stdout, stderr bytes.Buffer
			add := append([]string{"add", "instruction", "guide", sourcePath, "--target", "codex", "--scope", "global"}, selector...)
			if code := runWithoutTerminal(add, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
				t.Fatalf("user authoring code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
			}
			hint := manifestHintArgv(t, stdout.String(), "next: run ")
			if len(hint) == 0 || hint[0] != "apply" || !slices.Contains(hint, "--dry-run") {
				t.Fatalf("next command=%q, want apply preview", hint)
			}
			if slices.Contains(hint, "--manifest") != explicit {
				t.Errorf("next argv=%q, explicit user selector wanted=%v", hint, explicit)
			}
			hintRequest := ""
			if index := slices.Index(hint, "--manifest"); index >= 0 && index+1 < len(hint) {
				hintRequest = hint[index+1]
			}
			originalPaths, err := daempaths.Resolve(request)
			if err != nil {
				t.Fatal(err)
			}
			hintPaths, err := daempaths.Resolve(hintRequest)
			if err != nil {
				t.Fatal(err)
			}
			if hintPaths.ManifestPath != originalPaths.ManifestPath || hintPaths.ProjectPlacementAllowed() != originalPaths.ProjectPlacementAllowed() {
				t.Errorf("hint changed user selection/admission: request=%+v hint=%+v", originalPaths, hintPaths)
			}

			project := "version=1\ntargets=[\"codex\"]\n[instructions.guide]\nsource=" + strconv.Quote(sourcePath) + "\nscope=\"project\"\ntargets=[\"codex\"]\n"
			if err := os.WriteFile(manifestPath, []byte(project), 0o600); err != nil {
				t.Fatal(err)
			}
			_, inputErr := declarationmanifest.LoadSelected(t.Context(), originalPaths)
			if (inputErr == nil) != explicit {
				t.Fatalf("fixture did not distinguish implicit/explicit project admission: %v", inputErr)
			}
			before := snapshotApplyInputFiles(t, root)
			var originalOut, originalErr, hintOut, hintErr bytes.Buffer
			original := append([]string{"apply", "--dry-run"}, selector...)
			originalCode := runWithoutTerminal(original, &originalOut, &originalErr)
			hintCode := runWithoutTerminal(hint, &hintOut, &hintErr)
			if !explicit && (originalCode != 1 || originalOut.Len() != 0) {
				t.Fatalf("implicit project admission did not refuse before planning: code=%d stdout=%q stderr=%q", originalCode, &originalOut, &originalErr)
			}
			if hintCode != originalCode || !bytes.Equal(hintOut.Bytes(), originalOut.Bytes()) || !bytes.Equal(hintErr.Bytes(), originalErr.Bytes()) {
				t.Errorf("hint changed edited-manifest behavior: original=(%d,%q,%q) hint=(%d,%q,%q)", originalCode, &originalOut, &originalErr, hintCode, &hintOut, &hintErr)
			}
			if !reflect.DeepEqual(before, snapshotApplyInputFiles(t, root)) {
				t.Fatal("original or copied dry-run changed fixture files")
			}
		})
	}
}

func TestManifestHintsKeepImplicitUserOriginAcrossReadCommands(t *testing.T) {
	for _, command := range []struct {
		name string
		args []string
		code int
	}{
		{name: "status", args: []string{"status"}},
		{name: "lock preview", args: []string{"lock", "--dry-run"}},
		{name: "apply missing lock", args: []string{"apply", "--dry-run"}, code: 1},
		{name: "outdated", args: []string{"outdated"}},
	} {
		t.Run(command.name, func(t *testing.T) {
			root, _ := manifestHintWorkspace(t)
			manifestPath := filepath.Join(root, "config", "daem", "daem.toml")
			writeHintManifest(t, manifestPath)
			source := filepath.Join(root, "source.md")
			if err := os.WriteFile(source, []byte("Global instructions.\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			content := "version=1\ntargets=[\"codex\"]\n[instructions.guide]\nsource=" + strconv.Quote(source) + "\nscope=\"global\"\n"
			if err := os.WriteFile(manifestPath, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			before := snapshotApplyInputFiles(t, root)
			var stdout, stderr bytes.Buffer
			if code := runWithoutTerminal(command.args, &stdout, &stderr); code != command.code {
				t.Fatalf("source command code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
			}
			hint := manifestHintArgv(t, stdout.String()+stderr.String(), "next: run ")
			if len(hint) == 0 || hint[0] != "lock" || slices.Contains(hint, "--manifest") {
				t.Fatalf("implicit user command changed to an explicit next selector: %q", hint)
			}
			if !slices.Contains(hint, "--dry-run") {
				hint = append(hint, "--dry-run")
			}
			stdout.Reset()
			stderr.Reset()
			if code := runWithoutTerminal(append(hint, "--json"), &stdout, &stderr); code != 0 || stderr.Len() != 0 {
				t.Fatalf("next preview code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
			}
			var result struct {
				ManifestPath string `json:"manifest_path"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			selected, err := daempaths.Resolve("")
			if err != nil || selected.ProjectPlacementAllowed() || result.ManifestPath != manifestPath {
				t.Fatalf("next command lost user-default workspace/admission: result=%+v selected=%+v err=%v", result, selected, err)
			}
			if !reflect.DeepEqual(before, snapshotApplyInputFiles(t, root)) {
				t.Fatal("read-only commands changed fixture files")
			}
		})
	}
}

func TestManifestHintsKeepUnknownRequestInsteadOfResolvedTarget(t *testing.T) {
	_, cwd := manifestHintWorkspace(t)
	t.Setenv("XDG_DATA_HOME", "invalid-relative-data")
	for _, request := range []string{"", "./selected/daem.toml"} {
		var output strings.Builder
		printLockCommandHint(&output, filepath.Join(cwd, "selected", "daem.toml"), request)
		want := []string{"lock"}
		if request != "" {
			want = []string{"lock", "--manifest", request}
		}
		if got := manifestHintArgv(t, output.String(), "next: run "); !reflect.DeepEqual(got, want) {
			t.Fatalf("unknown request fallback=%q, want %q", got, want)
		}
	}
}
