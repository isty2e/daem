package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/metadatatx"
)

func TestAddRecoveryGuidanceDistinguishesDeclarationLinks(t *testing.T) {
	for _, producer := range []string{"unmanage", "migrate"} {
		for _, selection := range []string{"regular", "manifest-link", "lockfile-link", "parent-link"} {
			t.Run(producer+"/"+selection, func(t *testing.T) {
				root, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				testkit.SetDefaultRootEnv(t, filepath.Join(root, "home"))
				project := filepath.Join(root, "project")
				manifest := filepath.Join(project, "daem.toml")
				testkit.WriteFile(t, project, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
				testkit.WriteFile(t, project, "notes.md", "instruction\n")
				var stdout, stderr bytes.Buffer
				if code := testkit.RunCLI([]string{"lock", "--manifest", manifest}, &stdout, &stderr); code != 0 {
					t.Fatalf("seed lock: code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
				}
				resolved, err := paths.Resolve(manifest)
				if err != nil {
					t.Fatal(err)
				}
				state, err := statefile.Marshal(durable.EmptySnapshot())
				if err != nil {
					t.Fatal(err)
				}
				image := func(path string) metadatatx.Write {
					content := testkit.ReadFile(t, path)
					return metadatatx.Write{Path: path, Before: &metadatatx.Image{Content: content, Mode: 0o600}, After: content}
				}
				writes := []metadatatx.Write{
					{Path: resolved.StatefilePath, After: state},
					{Path: resolved.CarrierClaimRegistryPath, After: []byte("{}\n"), CommitPoint: producer == "unmanage"},
				}
				if producer == "unmanage" {
					writes = append(writes, image(manifest), image(resolved.LockfilePath))
				} else {
					writes = append(
						writes,
						metadatatx.Write{Path: resolved.OwnershipRegistryPath, After: []byte("{}\n")},
						metadatatx.Write{Path: filepath.Join(root, "legacy", "state.json"), After: state, CommitPoint: true},
					)
				}
				metadatatx.WritePrefix(t, resolved.StateDir, writes, 1)
				marker, err := fileset.FileSetAuthorityPath(resolved.StateDir)
				if err != nil {
					t.Fatal(err)
				}
				markerFile := filepath.Join(marker, "transaction.json")
				markerBefore := testkit.ReadFile(t, markerFile)
				manifestBefore := testkit.ReadFile(t, manifest)
				lockBefore := testkit.ReadFile(t, resolved.LockfilePath)

				switch selection {
				case "manifest-link", "lockfile-link":
					selected := manifest
					if selection == "lockfile-link" {
						selected = resolved.LockfilePath
					}
					actual := selected + ".actual"
					if err := os.Rename(selected, actual); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(filepath.Base(actual), selected); err != nil {
						t.Fatal(err)
					}
				case "parent-link":
					alias := filepath.Join(root, "selected")
					if err := os.Symlink("project", alias); err != nil {
						t.Fatal(err)
					}
					manifest = filepath.Join(alias, "daem.toml")
				}

				stdout.Reset()
				stderr.Reset()
				code := testkit.RunCLI([]string{"add", "instruction", "note", "notes.md", "--manifest", manifest, "--target", "codex"}, &stdout, &stderr)
				output := stdout.String() + stderr.String()
				if code != 1 || !strings.Contains(output, "outside current recovery authority") {
					t.Fatalf("add must refuse foreign recovery targets: code=%d output=%q", code, output)
				}
				if selection == "regular" {
					if strings.Contains(output, "links") || strings.Contains(output, "retry the exact interrupted write") {
						t.Fatalf("ordinary %s marker received link guidance: %s", producer, output)
					}
				} else if !strings.Contains(output, "if the manifest or lockfile links changed") || !strings.Contains(output, "preserve the metadata marker") {
					t.Fatalf("linked selection needs conditional guidance, not an asserted cause: %s", output)
				}
				testkit.AssertFileContent(t, markerFile, string(markerBefore))
				testkit.AssertFileContent(t, manifest, string(manifestBefore))
				testkit.AssertFileContent(t, resolved.LockfilePath, string(lockBefore))
			})
		}
	}
}
