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

	daempaths "github.com/isty2e/daem/internal/paths"
)

func TestManifestHintsFromRemoveErrorsPreserveSelection(t *testing.T) {
	for _, selection := range []string{"cwd", "relative cwd", "explicit other", "user fallback", "explicit user"} {
		for _, commonJSON := range []bool{false, true} {
			t.Run(selection+"/json="+strconv.FormatBool(commonJSON), func(t *testing.T) {
				root, cwd := manifestHintWorkspace(t)
				manifestPath := filepath.Join(cwd, "daem.toml")
				scope := "project"
				var selector []string
				switch selection {
				case "relative cwd":
					selector = []string{"--manifest", "./daem.toml"}
				case "explicit other":
					writeHintManifest(t, manifestPath)
					manifestPath = filepath.Join(root, "other 'workspace", "daem.toml")
					selector = []string{"--manifest", manifestPath}
				case "user fallback", "explicit user":
					manifestPath = filepath.Join(root, "config", "daem", "daem.toml")
					scope = "global"
					if selection == "explicit user" {
						selector = []string{"--manifest", manifestPath}
					}
				}
				writeHintManifest(t, manifestPath)
				sourcePath := filepath.Join(filepath.Dir(manifestPath), "source.md")
				source := "source.md"
				if scope == "global" {
					source = sourcePath
				}
				content := "version=1\ntargets=[\"codex\"]\n[instructions.guide]\nsource=" + strconv.Quote(source) + "\nscope=\"" + scope + "\"\ntargets=[\"codex\"]\n"
				if err := os.WriteFile(manifestPath, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(sourcePath, []byte("Instructions.\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				before := snapshotApplyInputFiles(t, root)
				var stdout, stderr bytes.Buffer
				args := append([]string{"remove", "instruction", "guid", "--dry-run"}, selector...)
				if commonJSON {
					args = append(args, "--json")
				}
				if code := runWithoutTerminal(args, &stdout, &stderr); code != 1 {
					t.Fatalf("missing selection code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
				}
				refusalDiagnosticText(t, "remove instruction", &stdout, &stderr, commonJSON)
				var argv []string
				if commonJSON {
					var payload struct {
						Guidance []struct {
							Kind string
							Argv []string
						}
					}
					if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
						t.Fatal(err)
					}
					for _, guidance := range payload.Guidance {
						if guidance.Kind == "command" && len(guidance.Argv) > 1 && guidance.Argv[1] == "remove" {
							argv = slices.Clone(guidance.Argv[1:])
						}
					}
				} else {
					argv = manifestHintArgv(t, stderr.String(), "hint: available selection: ")
				}
				if len(argv) < 3 || !reflect.DeepEqual(argv[:3], []string{"remove", "instruction", "guide"}) {
					t.Fatalf("suggested operation=%q, want the existing instruction", argv)
				}
				wantExplicit := selection == "explicit other" || selection == "explicit user"
				index := slices.Index(argv, "--manifest")
				if (index >= 0) != wantExplicit {
					t.Fatalf("suggested argv=%q, explicit selector wanted=%v", argv, wantExplicit)
				}
				manifestArgument := ""
				if index >= 0 {
					if index+1 == len(argv) {
						t.Fatal("manifest flag has no argument")
					}
					manifestArgument = argv[index+1]
				}
				selected, err := daempaths.Resolve(manifestArgument)
				if err != nil || selected.ManifestPath != manifestPath || selected.ProjectPlacementAllowed() != (selection != "user fallback") {
					t.Fatalf("suggested selection=%+v err=%v, want original workspace and placement policy", selected, err)
				}

				stdout.Reset()
				stderr.Reset()
				if code := runWithoutTerminal(append(argv, "--dry-run", "--json"), &stdout, &stderr); code != 0 || stderr.Len() != 0 {
					t.Fatalf("suggestion preview code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
				}
				var result struct {
					ManifestPath string `json:"manifest_path"`
					Changes      []struct {
						Operation  string `json:"operation"`
						ResourceID string `json:"resource_id"`
					} `json:"changes"`
				}
				if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.ManifestPath != manifestPath || len(result.Changes) != 1 || result.Changes[0].Operation != "remove" || result.Changes[0].ResourceID != "instruction/guide" {
					t.Fatalf("suggestion preview=%+v, want one removal from the original manifest", result)
				}
				if !reflect.DeepEqual(before, snapshotApplyInputFiles(t, root)) {
					t.Fatal("failed remove or suggested preview changed fixture files")
				}
			})
		}
	}
}

func TestManifestHintUnknownRemoveSelectionKeepsImplicitRequest(t *testing.T) {
	_, cwd := manifestHintWorkspace(t)
	writeHintManifest(t, filepath.Join(cwd, "daem.toml"))
	t.Setenv("XDG_DATA_HOME", "invalid-relative-root")
	var output strings.Builder
	printMissingResourceSelectionHint(&output, "", missingSelectionForTest())
	argv := manifestHintArgv(t, output.String(), "hint: available selection: ")
	if slices.Contains(argv, "--manifest") {
		t.Fatalf("implicit request emitted a redundant manifest selector: %q", argv)
	}
}
