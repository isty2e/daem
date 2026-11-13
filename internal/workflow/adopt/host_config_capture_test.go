package adopt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	codexhostpath "github.com/isty2e/daem/internal/output/hostpath/codex"
	pihostpath "github.com/isty2e/daem/internal/output/hostpath/pi"
	daempaths "github.com/isty2e/daem/internal/paths"
)

type captureWorkflowFixture struct {
	paths               daempaths.Paths
	host, source, alias string
	manifest            []byte
}

func newCaptureWorkflowFixture(t *testing.T, ownership string, host []byte) captureWorkflowFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, variable := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(variable, filepath.Join(root, variable))
	}
	paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	paths, err = paths.WithDataDir(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	fixture := captureWorkflowFixture{paths: paths, host: filepath.Join(root, ".codex", "capture.jsonc"), source: filepath.Join(root, "source.jsonc"), alias: filepath.Join(root, "alias.jsonc")}
	fixture.manifest = []byte("version = 1\ntargets = [\"codex\"]\n# keep manifest\n[[host_config]]\nid = \"chosen\"\nscope = \"project\"\npath = \"capture.jsonc\"\nsource = \"source.jsonc\"\nownership = \"" + ownership + "\"\n[[host_config]]\nid = \"peer\"\nscope = \"project\"\npath = \"unselected.json\"\nsource = \"alias.jsonc\"\n")
	if err := os.Mkdir(filepath.Dir(fixture.host), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(filepath.Dir(fixture.host), "unselected.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if host != nil {
		if err := os.WriteFile(fixture.host, host, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(paths.ManifestPath, fixture.manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.source, []byte("/* source */ {\"nested\":{\"a\":1,\"missing\":2},\"unselected\":7}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("source.jsonc", fixture.alias); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.LockfilePath, []byte("lockfile must not be decoded or changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func captureKey(t *testing.T, pointer string) output.KeyPath {
	t.Helper()
	path, err := output.ParseKeyPath(pointer)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func assertCaptureArtifact(t *testing.T, path string, expected []byte) {
	t.Helper()
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, expected) {
		t.Fatalf("artifact %s differs: %q, %v", path, actual, err)
	}
}

func TestHostConfigCapturePlansNamedSharedSourceAndExecutesSelectedUpdate(t *testing.T) {
	host := []byte(`/* host */ {"nested":{"a":4,"new":8},"unselected":9}`)
	fixture := newCaptureWorkflowFixture(t, "keys", host)
	before, _ := os.ReadFile(fixture.source)
	plan, err := BuildHostConfigUpdatePlan(t.Context(), HostConfigUpdateInput{Paths: fixture.paths, Name: "chosen", Keys: []output.KeyPath{captureKey(t, "/nested")}})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	if plan.SourcePath() != fixture.source || plan.SourceReferent() == "" || !plan.OtherReadersUnenumerated() || !plan.ReplacesDesiredValues() || len(plan.KnownReferences()) != 2 {
		t.Fatal("named-file sharing disclosure is incomplete", plan.KnownReferences())
	}
	assertCaptureArtifact(t, fixture.source, before)
	assertCaptureArtifact(t, fixture.paths.ManifestPath, fixture.manifest)
	if _, err := os.Stat(fixture.paths.StateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preview created metadata", err)
	}
	result, err := ExecuteHostConfigCapture(t.Context(), plan, plan.ApprovalPath())
	if err != nil || result.Retired {
		t.Fatal("capture execution failed", result, err)
	}
	document, err := configdocument.Parse(t.Context(), plan.SourceAfter(), configdocument.JSONC, configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32})
	if err != nil {
		t.Fatal(err)
	}
	wanted, err := configdocument.Parse(t.Context(), []byte(`{"nested":{"a":4},"unselected":7}`), configdocument.JSONC, configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32})
	if err != nil {
		t.Fatal(err)
	}
	wantedLeaves, err := wanted.Leaves(t.Context(), 4096)
	if err != nil {
		t.Fatal(err)
	}
	actualLeaves, err := document.Leaves(t.Context(), 4096)
	if err != nil || len(actualLeaves) != len(wantedLeaves) {
		t.Fatal("capture changed source membership", err)
	}
	for _, leaf := range wantedLeaves {
		value, present, err := document.Lookup(leaf.Path)
		if err != nil || !present || !value.Equal(leaf.Value) {
			t.Fatal("capture did not update actual selected values or preserve unselected intent", leaf.Path, err)
		}
	}
	for _, pointer := range []string{"/nested/missing", "/nested/new"} {
		if _, present, err := document.Lookup(captureKey(t, pointer)); err != nil || present {
			t.Fatal("capture acquired or retained an unintended leaf", pointer, err)
		}
	}
	assertCaptureArtifact(t, fixture.source, plan.SourceAfter())
	assertCaptureArtifact(t, fixture.alias, plan.SourceAfter())
	assertCaptureArtifact(t, fixture.host, host)
	assertCaptureArtifact(t, fixture.paths.ManifestPath, fixture.manifest)
	assertCaptureArtifact(t, fixture.paths.LockfilePath, []byte("lockfile must not be decoded or changed"))
	for _, path := range []string{fixture.paths.StatefilePath, fixture.paths.OwnershipRegistryPath, fixture.paths.CarrierClaimRegistryPath} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("capture wrote managed state", path, err)
		}
	}
	if _, err := ExecuteHostConfigCapture(t.Context(), plan, plan.ApprovalPath()); err == nil {
		t.Fatal("capture replayed a consumed plan")
	}
}

func TestHostConfigCaptureRequiresNamedApprovalAndRefusesStaleInputs(t *testing.T) {
	for _, change := range []string{"approval", "source", "manifest", "host", "alias"} {
		t.Run(change, func(t *testing.T) {
			fixture := newCaptureWorkflowFixture(t, "keys", []byte(`{"nested":{"a":4}}`))
			plan, err := BuildHostConfigUpdatePlan(t.Context(), HostConfigUpdateInput{Paths: fixture.paths, Name: "chosen"})
			if err != nil {
				t.Fatal(err)
			}
			defer plan.Close()
			approved := plan.ApprovalPath()
			before, _ := os.ReadFile(fixture.source)
			switch change {
			case "approval":
				approved = fixture.alias
			case "source":
				before = []byte(`{"external":1}`)
				if err := os.WriteFile(fixture.source, before, 0o600); err != nil {
					t.Fatal(err)
				}
			case "manifest":
				if err := os.WriteFile(fixture.paths.ManifestPath, append(fixture.manifest, []byte("# edit\n")...), 0o600); err != nil {
					t.Fatal(err)
				}
			case "host":
				if err := os.WriteFile(fixture.host, []byte(`{"nested":{"a":5}}`), 0o600); err != nil {
					t.Fatal(err)
				}
			case "alias":
				if err := os.Remove(fixture.alias); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(fixture.alias, before, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			_, err = ExecuteHostConfigCapture(t.Context(), plan, approved)
			if err == nil {
				t.Fatal("changed capture dependency was accepted")
			}
			if change == "approval" && !errors.Is(err, ErrHostConfigCaptureApproval) {
				t.Fatal("wrong approval classification", err)
			}
			if change == "source" || change == "manifest" {
				var stale mutation.StaleSnapshotError
				if !errors.As(err, &stale) {
					t.Fatal("wrong snapshot refusal", err)
				}
			}
			assertCaptureArtifact(t, fixture.source, before)
		})
	}
}

func TestHostConfigCaptureExportsAndRetiresWholeDocumentsWithoutRoundtrip(t *testing.T) {
	for _, retirement := range []bool{false, true} {
		host := []byte("// raw host spelling\n{ \"literal\": 1.00, \"nested\": {} }\n")
		if retirement {
			host = nil
		}
		fixture := newCaptureWorkflowFixture(t, "document", host)
		before, _ := os.ReadFile(fixture.source)
		input := HostConfigUpdateInput{Paths: fixture.paths, Name: "chosen"}
		if !retirement {
			input.SourcePath = "new-source.jsonc"
		}
		plan, err := BuildHostConfigUpdatePlan(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		defer plan.Close()
		if plan.RetiresDeclaration() != retirement {
			t.Fatal("incorrect missing-document disposition")
		}
		if _, err := ExecuteHostConfigCapture(t.Context(), plan, plan.ApprovalPath()); err != nil {
			t.Fatal(err)
		}
		assertCaptureArtifact(t, fixture.source, before)
		current, err := declarationmanifest.Load(t.Context(), fixture.paths.ManifestPath)
		if err != nil {
			t.Fatal(err)
		}
		if retirement {
			if len(current.HostConfigs()) != 1 || current.HostConfigs()[0].ID().Name() != "peer" {
				t.Fatal("retirement changed unrelated declarations")
			}
		} else {
			assertCaptureArtifact(t, filepath.Join(fixture.paths.ManifestRoot, "new-source.jsonc"), host)
		}
	}
}

func TestHostConfigCaptureRejectsHostCacheReadonlyAndExistingExportTargets(t *testing.T) {
	for _, scenario := range []string{"host", "host-unselected", "cache", "metadata", "legacy", "readonly", "export-existing"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newCaptureWorkflowFixture(t, "keys", []byte(`{"nested":{"a":42}}`))
			input := HostConfigUpdateInput{Paths: fixture.paths, Name: "chosen"}
			if scenario == "host" || scenario == "host-unselected" || scenario == "cache" || scenario == "metadata" || scenario == "legacy" {
				destination := fixture.host
				if scenario == "host-unselected" {
					destination = filepath.Join(filepath.Dir(fixture.host), "unselected.json")
					if err := os.Remove(destination); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(destination, []byte(`{"nested":{"a":1}}`), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if scenario == "cache" || scenario == "metadata" || scenario == "legacy" {
					root := fixture.paths.SourceCacheDir
					if scenario == "metadata" {
						root = fixture.paths.GlobalMetadataDir()
					}
					if scenario == "legacy" {
						root = filepath.Join(fixture.paths.ManifestRoot, "legacy-state")
						fixture.paths.LegacyUserStateDir = root
						input.Paths = fixture.paths
					}
					destination = filepath.Join(root, "cached.jsonc")
					if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(destination, []byte(`{"nested":{"a":1}}`), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Remove(fixture.source); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(destination, fixture.source); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "readonly" {
				if err := os.Chmod(fixture.source, 0o400); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "export-existing" {
				input.SourcePath = fixture.source
			}
			source, _ := os.ReadFile(fixture.source)
			if plan, err := BuildHostConfigUpdatePlan(t.Context(), input); err == nil || plan != nil {
				if plan != nil {
					_ = plan.Close()
				}
				t.Fatal("unsupported source destination was admitted")
			} else if (scenario == "host" || scenario == "host-unselected" || scenario == "cache" || scenario == "metadata" || scenario == "legacy") && !errors.Is(err, ErrHostConfigCaptureArtifactOverlap) {
				t.Fatal("artifact-overlap refusal came from an unrelated failure", err)
			}
			assertCaptureArtifact(t, fixture.source, source)
			assertCaptureArtifact(t, fixture.paths.ManifestPath, fixture.manifest)
		})
	}
}

func TestCaptureRejectsDeclarationAliasingHostOrOriginalSource(t *testing.T) {
	for _, sourceAliasesManifest := range []bool{false, true} {
		fixture := newCaptureWorkflowFixture(t, "document", nil)
		manifest := bytes.Replace(fixture.manifest, []byte(`path = "capture.jsonc"`), []byte(`path = "capture.bin"`), 1)
		if err := os.WriteFile(fixture.paths.ManifestPath, manifest, 0o600); err != nil {
			t.Fatal(err)
		}
		if sourceAliasesManifest {
			if err := os.Remove(fixture.source); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(fixture.paths.ManifestPath, fixture.source); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.Symlink("../daem.toml", filepath.Join(filepath.Dir(fixture.host), "capture.bin")); err != nil {
				t.Fatal(err)
			}
		}
		if plan, err := BuildHostConfigUpdatePlan(t.Context(), HostConfigUpdateInput{Paths: fixture.paths, Name: "chosen"}); err == nil || plan != nil {
			if plan != nil {
				_ = plan.Close()
			}
			t.Fatal("declaration write escaped source/host role separation")
		} else if !errors.Is(err, ErrHostConfigCaptureArtifactOverlap) {
			t.Fatal("declaration alias test did not reach artifact qualification", err)
		}
		assertCaptureArtifact(t, fixture.paths.ManifestPath, manifest)
	}
}

func TestWholeMissingCaptureCanRetireWithoutRecreatingMissingSource(t *testing.T) {
	fixture := newCaptureWorkflowFixture(t, "document", nil)
	if err := os.Remove(fixture.source); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildHostConfigUpdatePlan(t.Context(), HostConfigUpdateInput{Paths: fixture.paths, Name: "chosen"})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	if !plan.RetiresDeclaration() {
		t.Fatal("missing whole document became an empty capture")
	}
	result, err := ExecuteHostConfigCapture(t.Context(), plan, plan.ApprovalPath())
	if err != nil || !result.Committed || !result.Retired {
		t.Fatal("declaration retirement failed", result, err)
	}
	if _, err := os.Stat(fixture.source); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("retirement recreated the absent source", err)
	}
}

func TestHostConfigCaptureUsesSharedNativeRootAdapters(t *testing.T) {
	for _, selected := range []string{"pi", "codex", "claude-code", "opencode", "antigravity-cli"} {
		for _, scope := range []string{"project", "global"} {
			if selected == "antigravity-cli" && scope == "project" {
				continue
			}
			for _, sourceAliasesOutput := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/source-alias=%t", selected, scope, sourceAliasesOutput), func(t *testing.T) {
					host := []byte(`{"nested":{"a":42}}`)
					fixture := newCaptureWorkflowFixture(t, "keys", host)
					manifest := strings.ReplaceAll(string(fixture.manifest), `targets = ["codex"]`, `targets = ["`+selected+`"]`)
					root := filepath.Join(fixture.paths.ManifestRoot, "."+selected)
					if selected == "claude-code" {
						root = filepath.Join(fixture.paths.ManifestRoot, ".claude")
					}
					if selected == "opencode" && scope == "project" {
						manifest = strings.ReplaceAll(manifest, `path = "`, `path = ".opencode/`)
					}
					if scope == "global" {
						root = filepath.Join(fixture.paths.ManifestRoot, "native-home")
						manifest = strings.ReplaceAll(manifest, `scope = "project"`, `scope = "global"`)
						manifest = strings.ReplaceAll(manifest, `source = "source.jsonc"`, `source = "`+fixture.source+`"`)
						manifest = strings.ReplaceAll(manifest, `source = "alias.jsonc"`, `source = "`+fixture.alias+`"`)
						switch selected {
						case "pi":
							t.Setenv(pihostpath.AgentRootEnvironmentVariable, root)
						case "codex":
							t.Setenv(codexhostpath.HomeEnvironmentVariable, root)
						case "claude-code":
							t.Setenv("CLAUDE_CONFIG_DIR", root)
						case "opencode":
							t.Setenv("XDG_CONFIG_HOME", root)
							root = filepath.Join(root, "opencode")
						case "antigravity-cli":
							home, err := os.UserHomeDir()
							if err != nil {
								t.Fatal(err)
							}
							root = filepath.Join(home, ".gemini", "antigravity-cli")
						}
					}
					if err := os.MkdirAll(root, 0o700); err != nil {
						t.Fatal(err)
					}
					fixture.host = filepath.Join(root, "capture.jsonc")
					if err := os.WriteFile(fixture.host, host, 0o600); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(fixture.paths.ManifestPath, []byte(manifest), 0o600); err != nil {
						t.Fatal(err)
					}
					if sourceAliasesOutput {
						if err := os.Remove(fixture.source); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink(fixture.host, fixture.source); err != nil {
							t.Fatal(err)
						}
					}
					plan, err := BuildHostConfigUpdatePlan(t.Context(), HostConfigUpdateInput{Paths: fixture.paths, Name: "chosen"})
					if sourceAliasesOutput {
						if plan != nil {
							_ = plan.Close()
						}
						if !errors.Is(err, ErrHostConfigCaptureArtifactOverlap) {
							t.Fatal("capture accepted a source alias to its configured native output", err)
						}
						assertCaptureArtifact(t, fixture.host, host)
						assertCaptureArtifact(t, fixture.paths.ManifestPath, []byte(manifest))
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					defer plan.Close()
					if result, err := ExecuteHostConfigCapture(t.Context(), plan, plan.ApprovalPath()); err != nil || !result.Committed {
						t.Fatal("native root capture failed", result, err)
					}
					assertCaptureArtifact(t, fixture.host, host)
				})
			}
		}
	}
}

func TestCaptureRejectsSourceRetargetAndCancellationWithoutEffects(t *testing.T) {
	for _, cancelExecution := range []bool{false, true} {
		fixture := newCaptureWorkflowFixture(t, "keys", []byte(`{"nested":{"a":42}}`))
		before, _ := os.ReadFile(fixture.source)
		plan, err := BuildHostConfigUpdatePlan(t.Context(), HostConfigUpdateInput{Paths: fixture.paths, Name: "chosen"})
		if err != nil {
			t.Fatal(err)
		}
		defer plan.Close()
		ctx := t.Context()
		if cancelExecution {
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			ctx = canceled
		} else {
			moved := filepath.Join(fixture.paths.ManifestRoot, "same-source.jsonc")
			if err := os.Rename(fixture.source, moved); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("same-source.jsonc", fixture.source); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := ExecuteHostConfigCapture(ctx, plan, plan.ApprovalPath()); err == nil {
			t.Fatal("retarget/canceled capture was admitted")
		}
		assertCaptureArtifact(t, fixture.source, before)
		assertCaptureArtifact(t, fixture.paths.ManifestPath, fixture.manifest)
	}
}
