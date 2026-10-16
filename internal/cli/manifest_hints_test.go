package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	daempaths "github.com/isty2e/daem/internal/paths"
)

func TestManifestHintsPreserveSelectedWorkspace(t *testing.T) {
	for _, selection := range []string{"cwd", "explicit cwd", "relative cwd", "other manifest", "user fallback", "explicit user"} {
		t.Run(selection, func(t *testing.T) {
			root, cwd := manifestHintWorkspace(t)
			selected := filepath.Join(cwd, "daem.toml")
			var selector []string
			switch selection {
			case "explicit cwd":
				selector = []string{"--manifest", selected}
			case "relative cwd":
				selector = []string{"--manifest", "./daem.toml"}
			case "other manifest":
				writeHintManifest(t, selected)
				selected = filepath.Join(root, "other 'workspace", "daem.toml")
				selector = []string{"--manifest", selected}
			case "user fallback", "explicit user":
				selected = filepath.Join(root, "config", "daem", "daem.toml")
				if selection == "explicit user" {
					selector = []string{"--manifest", selected}
				}
			}
			writeHintManifest(t, selected)
			before := snapshotApplyInputFiles(t, root)
			var stdout, stderr bytes.Buffer
			if code := runWithoutTerminal(append([]string{"status"}, selector...), &stdout, &stderr); code != 0 || stderr.Len() != 0 {
				t.Fatalf("status code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
			}
			if after := snapshotApplyInputFiles(t, root); !reflect.DeepEqual(before, after) {
				t.Fatal("rendering hints changed fixture files")
			}
			argv := manifestHintArgv(t, stdout.String(), "next: run ")
			if len(argv) == 0 || argv[0] != "lock" {
				t.Fatalf("next command=%q, want lock", argv)
			}
			wantExplicit := selection == "other manifest" || selection == "explicit user"
			if slices.Contains(argv, "--manifest") != wantExplicit {
				t.Fatalf("next argv=%q, explicit selector wanted=%v", argv, wantExplicit)
			}

			stdout.Reset()
			stderr.Reset()
			if code := runWithoutTerminal(append(argv, "--dry-run", "--json"), &stdout, &stderr); code != 0 || stderr.Len() != 0 {
				t.Fatalf("hint code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
			}
			var result struct {
				ManifestPath string `json:"manifest_path"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.ManifestPath != selected {
				t.Fatalf("hint selected=%q, want %q", result.ManifestPath, selected)
			}
			if after := snapshotApplyInputFiles(t, root); !reflect.DeepEqual(before, after) {
				t.Fatal("read-only next command changed fixture files")
			}
		})
	}
}

func TestManifestHintsDistinguishReadAndCreationDefaults(t *testing.T) {
	root, cwd := manifestHintWorkspace(t)
	userManifest := filepath.Join(root, "config", "daem", "daem.toml")
	writeHintManifest(t, userManifest)
	localManifest := filepath.Join(cwd, "daem.toml")
	before := snapshotApplyInputFiles(t, root)

	var readHint strings.Builder
	printLockCommandHint(&readHint, localManifest, localManifest)
	readArgv := manifestHintArgv(t, readHint.String(), "next: run ")
	if !slices.Contains(readArgv, "--manifest") {
		t.Fatalf("absent cwd read target lost explicit selection: %q", readArgv)
	}
	var stdout, stderr bytes.Buffer
	if code := runWithoutTerminal(append(readArgv, "--dry-run", "--json"), &stdout, &stderr); code != 1 {
		t.Fatalf("missing read target code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
	}
	refusalDiagnosticText(t, "lock", &stdout, &stderr, true)

	var creationHint strings.Builder
	printMissingManifestInitHint(&creationHint, localManifest, os.ErrNotExist)
	creationArgv := manifestHintArgv(t, creationHint.String(), "next: run ")
	if slices.Contains(creationArgv, "--manifest") || !slices.Contains(creationArgv, "--dry-run") {
		t.Fatalf("creation argv=%q, want implicit cwd preview", creationArgv)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runWithoutTerminal(append(creationArgv, "--json"), &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("creation hint code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
	}
	var result struct {
		ManifestPath string `json:"manifest_path"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ManifestPath != localManifest {
		t.Fatalf("creation selected=%q, want %q", result.ManifestPath, localManifest)
	}
	if after := snapshotApplyInputFiles(t, root); !reflect.DeepEqual(before, after) {
		t.Fatal("read/creation previews changed fixture files")
	}
}

func TestManifestHintObservationFailureKeepsExplicitTarget(t *testing.T) {
	_, cwd := manifestHintWorkspace(t)
	manifestPath := filepath.Join(cwd, "daem.toml")
	writeHintManifest(t, manifestPath)
	t.Setenv("XDG_DATA_HOME", "invalid-relative-data")
	if _, err := daempaths.Resolve(""); err == nil {
		t.Fatal("fixture did not prevent implicit selection observation")
	}
	var output strings.Builder
	printTargetSelectionHint(&output, manifestPath, manifestPath)
	argv := manifestHintArgv(t, output.String(), "next: run ")
	want := []string{"lock", "--manifest", manifestPath, "--dry-run"}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("next argv=%q, want explicit fallback %q", argv, want)
	}
}

func manifestHintWorkspace(t *testing.T) (string, string) {
	t.Helper()
	root := isolatedApplyAuthorityRoot(t)
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	cwd := filepath.Join(root, "project with spaces")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return root, cwd
}

func writeHintManifest(t *testing.T, manifestPath string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte("version=1\ntargets=[\"codex\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func manifestHintArgv(t *testing.T, output, prefix string) []string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX concrete command contract")
	}
	for line := range strings.SplitSeq(output, "\n") {
		command, found := strings.CutPrefix(line, prefix)
		if !found {
			continue
		}
		probe := `daem() { printf '%s\000' "$@"; }; ` + command
		result, err := exec.Command("/bin/sh", "-c", probe).Output()
		if err != nil {
			t.Fatalf("parse printed next command %q: %v", command, err)
		}
		return strings.Split(strings.TrimSuffix(string(result), "\x00"), "\x00")
	}
	t.Fatalf("no %q command in %q", prefix, output)
	return nil
}
