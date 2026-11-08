package cli_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/effect/mutation"
	managedhostpath "github.com/isty2e/daem/internal/output/hostpath/managed"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/workflow/apply"
	lockgenerate "github.com/isty2e/daem/internal/workflow/lock/generate"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
	"github.com/isty2e/daem/internal/workflow/readiness"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestNativeKeyCLIPlanning(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "missing"
		if existing {
			name = "shared"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", filepath.Join(root, "home"))
			t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
			t.Setenv("CODEX_HOME", filepath.Join(root, "codex-home"))
			workspace := filepath.Join(root, "workspace")
			manifest := filepath.Join(workspace, "daem.toml")
			testkit.WriteFile(t, workspace, "daem.toml", `version = 1
targets = ["codex"]

[[host_config]]
id = "preferences"
path = "config.toml"
source = "./preferences.toml"
ownership = "keys"
`)
			testkit.WriteFile(t, workspace, "preferences.toml", "model = \"native-test\"\n")
			const original = "# User-owned comment\nunmanaged = true\n"
			if existing {
				testkit.WriteFile(t, workspace, ".codex/config.toml", original)
				if err := os.Chmod(filepath.Join(workspace, ".codex", "config.toml"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if code, stdout, stderr := testkit.RunCapturedCLI("lock", "--manifest", manifest); code != 0 {
				t.Fatalf("lock failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
			}
			for _, args := range [][]string{
				{"status", "--manifest", manifest, "--json"},
				{"apply", "--manifest", manifest, "--dry-run", "--json"},
			} {
				code, stdout, stderr := testkit.RunCapturedCLI(args...)
				if code != 0 {
					t.Fatalf("%q failed: code=%d stdout=%s stderr=%s", args, code, stdout, stderr)
				}
				plan := clijson.DecodePlan(t, []byte(stdout))
				if plan.HasErrors || plan.ActionCount != 1 || len(plan.Actions) != 1 || plan.Actions[0].Kind != "create" || !strings.Contains(stdout, "config.toml") {
					t.Fatalf("native pending key action missing: %s", stdout)
				}
			}
			requireNativeReadOnlyWritePlan(t, manifest)
			host, err := os.ReadFile(filepath.Join(workspace, ".codex", "config.toml"))
			if existing {
				if err != nil || !bytes.Equal(host, []byte(original)) {
					t.Fatalf("planning changed shared host file: %s %v", host, err)
				}
				info, err := os.Stat(filepath.Join(workspace, ".codex", "config.toml"))
				if err != nil || info.Mode().Perm() != 0o644 {
					t.Fatalf("planning changed the actual shared file mode: %v", err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("planning created missing host file: %s %v", host, err)
			}
			for _, path := range []string{filepath.Join(workspace, ".daem", "state.json"), filepath.Join(root, "data", "daem", "ownership", "claims.json")} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("planning created persistence %q: %v", path, err)
				}
			}
		})
	}
}

func TestNativeKeyCLIPlanningEmptyAndChangedSources(t *testing.T) {
	for _, fault := range []string{"empty", "stale", "missing"} {
		t.Run(fault, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", filepath.Join(root, "home"))
			t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
			manifest := filepath.Join(root, "workspace", "daem.toml")
			workspace := filepath.Dir(manifest)
			testkit.WriteFile(t, workspace, "daem.toml", `version = 1
targets = ["codex"]
[[host_config]]
id = "first"
path = "config.toml"
source = "./first.toml"
ownership = "keys"
[[host_config]]
id = "second"
path = "extension/second.toml"
source = "./second.toml"
ownership = "keys"
`)
			first := "first = 1\n"
			if fault == "empty" {
				first = ""
			}
			testkit.WriteFile(t, workspace, "first.toml", first)
			testkit.WriteFile(t, workspace, "second.toml", "")
			if code, stdout, stderr := testkit.RunCapturedCLI("lock", "--manifest", manifest); code != 0 {
				t.Fatalf("lock failed: %d %s %s", code, stdout, stderr)
			}
			if fault == "stale" {
				testkit.WriteFile(t, workspace, "second.toml", "changed = true\n")
			} else if fault == "missing" {
				if err := os.Remove(filepath.Join(workspace, "second.toml")); err != nil {
					t.Fatal(err)
				}
			}
			for _, args := range [][]string{
				{"status", "--manifest", manifest, "--json"},
				{"apply", "--manifest", manifest, "--dry-run", "--json"},
			} {
				code, stdout, stderr := testkit.RunCapturedCLI(args...)
				if fault == "empty" {
					if code != 0 {
						t.Fatalf("empty positive sources failed: %d %s %s", code, stdout, stderr)
					}
					plan := clijson.DecodePlan(t, []byte(stdout))
					if plan.HasErrors || plan.ActionCount != 0 || len(plan.Actions) != 0 {
						t.Fatalf("empty native intent invented key work: %s", stdout)
					}
				} else if code == 0 || strings.Contains(stdout, `"actions"`) {
					t.Fatalf("source failure exposed usable prefix: %d %s %s", code, stdout, stderr)
				}
			}
			if fault == "empty" {
				assessment, err := nativeconfig.Assess(t.Context(), nativeKeyPlanningInput(t, manifest))
				if err != nil || len(assessment.NativeSources) != 2 {
					t.Fatalf("empty selected sources lost typed presence: %#v %v", assessment.NativeSources, err)
				}
				requireNativeReadOnlyWritePlan(t, manifest)
			}
			for _, relative := range []string{".codex", ".daem/state.json"} {
				if _, err := os.Stat(filepath.Join(workspace, relative)); !os.IsNotExist(err) {
					t.Fatalf("planning created %s: %v", relative, err)
				}
			}
		})
	}
}

func nativeKeyPlanningInput(t *testing.T, manifest string) readiness.Input {
	t.Helper()
	paths, err := daempaths.Resolve(manifest)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := declarationmanifest.LoadSelected(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := lockfile.Load(t.Context(), paths.LockfilePath)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := targetselection.ForAvailableTargets(environment.Targets(), nil)
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := nativeconfig.LoadPersistenceEpoch(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	sourceEpoch, err := nativeconfig.ResolveSourceEpoch(t.Context(), paths, environment, locked, selection)
	if err != nil {
		t.Fatal(err)
	}
	return readiness.Input{
		Context: reconcile.ContextInspect, Paths: paths, Environment: environment, Lockfile: locked,
		Resolver:  liveobserve.DestinationResolver(managedhostpath.Resolver(paths, nil).Resolve),
		Selection: selection, SourceEpoch: &sourceEpoch, PersistenceEpoch: &epoch,
		HookContributionEncoder: hookcodec.CanonicalHookContribution,
		MCPContributionEncoder:  mcpcodec.CanonicalMCPBindingContribution,
	}
}

func TestNativeKeyCLIPlanningLastRemovalFromPreparedState(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	workspace := filepath.Join(root, "workspace")
	manifest := filepath.Join(workspace, "daem.toml")
	testkit.WriteFile(t, workspace, "daem.toml", `version = 1
targets = ["codex"]
[[host_config]]
id = "preferences"
path = "config.toml"
source = "./preferences.toml"
ownership = "keys"
`)
	testkit.WriteFile(t, workspace, "preferences.toml", "model = \"native-test\"\n")
	testkit.WriteFile(t, workspace, ".codex/config.toml", "# User-owned comment\nunmanaged = true\n")
	if code, stdout, stderr := testkit.RunCapturedCLI("lock", "--manifest", manifest); code != 0 {
		t.Fatalf("lock failed: %d %s %s", code, stdout, stderr)
	}
	input := nativeKeyPlanningInput(t, manifest)
	assessment, err := nativeconfig.Assess(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	codecs, err := nativeconfig.Codecs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	codec, err := statefile.NewNativeCodec(codecs.Contributions())
	if err != nil {
		t.Fatal(err)
	}
	// This is component-test preparation, not a successful real Apply producer.
	prepared, err := effectpreparation.PrepareNativeApplyState(t.Context(), effectpreparation.NativeApplyStateInput{
		Before: assessment.CurrentState, Aggregates: assessment.Reconciliation.Aggregates(), StateCodec: codec,
	})
	if err != nil || len(prepared.AggregateEffects()) != 1 {
		t.Fatalf("prepare actual key decisions: %v", err)
	}
	effect := prepared.AggregateEffects()[0]
	host := effect.Rendered().Document().Content()
	testkit.WriteFile(t, workspace, ".codex/config.toml", string(host))
	state, err := codec.Encode(prepared.AfterState())
	if err != nil {
		t.Fatal(err)
	}
	testkit.WriteFile(t, workspace, ".daem/state.json", string(state))
	stateAuthority, err := mutation.ObservePersistedDirectoryEntryAuthority(input.Paths.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(stateAuthority.Exact(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := mutation.ObserveDirectoryEntryAuthority(filepath.Join(workspace, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	exact, known := namespace.Exact()
	if !known {
		t.Fatal("prepared host fixture has no actual exact namespace")
	}
	binding, err := ownership.NewDocumentBinding(owner, effect.DocumentAddress().AggregateRoot(), exact)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := ownership.NewRegistry(nil, binding)
	if err != nil {
		t.Fatal(err)
	}
	registryBytes, err := ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	testkit.WriteFile(t, filepath.Dir(input.Paths.OwnershipRegistryPath), filepath.Base(input.Paths.OwnershipRegistryPath), string(registryBytes))
	testkit.WriteFile(t, workspace, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
	if code, stdout, stderr := testkit.RunCapturedCLI("lock", "--manifest", manifest); code != 0 {
		t.Fatalf("remove declaration lock failed: %d %s %s", code, stdout, stderr)
	}
	for _, args := range [][]string{
		{"status", "--manifest", manifest, "--json"},
		{"apply", "--manifest", manifest, "--dry-run", "--json"},
	} {
		code, stdout, stderr := testkit.RunCapturedCLI(args...)
		if code != 0 {
			t.Fatalf("last-removal planning failed: %d %s %s", code, stdout, stderr)
		}
		plan := clijson.DecodePlan(t, []byte(stdout))
		if plan.HasErrors || plan.ActionCount != 1 || len(plan.Actions) != 1 || plan.Actions[0].Kind != "delete" {
			t.Fatalf("last native key history was dropped or misclassified: %s", stdout)
		}
	}
	for path, expected := range map[string][]byte{
		filepath.Join(workspace, ".codex", "config.toml"): host,
		input.Paths.StatefilePath:                         state, input.Paths.OwnershipRegistryPath: registryBytes,
	} {
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("last-removal planning mutated %q: %v", path, err)
		}
	}
	requireNativeReadOnlyWritePlan(t, manifest)
}

func TestNativeKeyPlanningRetainsSuppliedSourceEpoch(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	manifest := filepath.Join(root, "workspace", "daem.toml")
	workspace := filepath.Dir(manifest)
	testkit.WriteFile(t, workspace, "daem.toml", `version = 1
targets = ["codex"]
[[host_config]]
id = "preferences"
path = "config.toml"
source = "./preferences.toml"
ownership = "keys"
`)
	testkit.WriteFile(t, workspace, "preferences.toml", "model = \"first\"\n")
	if code, stdout, stderr := testkit.RunCapturedCLI("lock", "--manifest", manifest); code != 0 {
		t.Fatalf("lock failed: %d %s %s", code, stdout, stderr)
	}
	input := nativeKeyPlanningInput(t, manifest)
	events := 0
	_, err := lockgenerate.Build(t.Context(), lockgenerate.Input{
		Paths: input.Paths, Environment: input.Environment,
		SourceEvents: func(acquisition.Event) { events++ },
	})
	if err == nil || events != 0 {
		t.Fatalf("native source generation omitted limit admission before source effects: %v events=%d", err, events)
	}
	generation, err := lockgenerate.Build(t.Context(), lockgenerate.Input{
		Paths: input.Paths, Environment: input.Environment, NativeDocumentLimits: nativeconfig.DocumentLimits(),
	})
	if err != nil || !generation.Snapshot().ContainsHostConfig() {
		t.Fatalf("shared generation omitted native work: %v", err)
	}
	testkit.WriteFile(t, workspace, "preferences.toml", "model = \"second\"\n")
	if code, stdout, stderr := testkit.RunCapturedCLI("lock", "--manifest", manifest); code != 0 {
		t.Fatalf("relock failed: %d %s %s", code, stdout, stderr)
	}
	input.Lockfile, err = lockfile.Load(t.Context(), input.Paths.LockfilePath)
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := nativeconfig.Assess(t.Context(), input)
	if err == nil || len(assessment.NativeSources) != 0 || len(assessment.Reconciliation.Aggregates()) != 0 {
		t.Fatalf("supplied stale source epoch was replaced or exposed a prefix: %#v %v", assessment, err)
	}
}

func TestNativeKeyCLIPlanningPreservesSelectedActors(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	workspace := filepath.Join(root, "workspace")
	manifest := filepath.Join(workspace, "daem.toml")
	testkit.WriteFile(t, workspace, "daem.toml", `version = 1
targets = ["pi", "codex"]
[[host_config]]
id = "pi-preferences"
targets = ["pi"]
path = "settings.json"
source = "./pi.json"
ownership = "keys"
[[host_config]]
id = "codex-preferences"
targets = ["codex"]
path = "config.toml"
source = "./codex.toml"
ownership = "keys"
`)
	testkit.WriteFile(t, workspace, "pi.json", `{"model":"pi-test"}`)
	testkit.WriteFile(t, workspace, "codex.toml", "model = \"codex-test\"\n")
	if code, stdout, stderr := testkit.RunCapturedCLI("lock", "--manifest", manifest); code != 0 {
		t.Fatalf("lock failed: %d %s %s", code, stdout, stderr)
	}
	for _, args := range [][]string{
		{"status", "--manifest", manifest, "--json"},
		{"apply", "--manifest", manifest, "--dry-run", "--json"},
	} {
		code, stdout, stderr := testkit.RunCapturedCLI(args...)
		if code != 0 {
			t.Fatalf("mixed selected actors failed: %d %s %s", code, stdout, stderr)
		}
		plan := clijson.DecodePlan(t, []byte(stdout))
		if plan.HasErrors || plan.ActionCount != 2 || len(plan.Actions) != 2 || plan.Actions[0].Target == plan.Actions[1].Target {
			t.Fatalf("selected native actor was dropped: %s", stdout)
		}
	}
}

func TestNativeKeyCLIPlanningGlobalRootAdmission(t *testing.T) {
	for _, target := range []string{"pi", "codex"} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", filepath.Join(root, "home"))
			t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
			hostRoot := filepath.Join(root, "selected-host-root")
			t.Setenv("PI_CODING_AGENT_DIR", hostRoot)
			t.Setenv("CODEX_HOME", hostRoot+string(filepath.Separator)+".")
			workspace := filepath.Join(root, "workspace")
			manifest := filepath.Join(workspace, "daem.toml")
			path, content := "settings.json", `{"model":"native-test"}`
			if target == "codex" {
				path, content = "config.toml", "model = \"native-test\"\n"
			}
			testkit.WriteFile(t, workspace, "fragment", content)
			testkit.WriteFile(t, workspace, "daem.toml", fmt.Sprintf(`version = 1
targets = [%q]
[[host_config]]
id = "preferences"
scope = "global"
path = %q
source = %q
ownership = "keys"
`, target, path, filepath.Join(workspace, "fragment")))
			if code, stdout, stderr := testkit.RunCapturedCLI("lock", "--manifest", manifest); code != 0 {
				t.Fatalf("lock failed: %d %s %s", code, stdout, stderr)
			}
			for _, args := range [][]string{{"status", "--manifest", manifest, "--json"}, {"apply", "--manifest", manifest, "--dry-run", "--json"}} {
				code, stdout, stderr := testkit.RunCapturedCLI(args...)
				if code != 0 {
					t.Fatalf("missing selected global root could not be planned: %d %s %s", code, stdout, stderr)
				}
				plan := clijson.DecodePlan(t, []byte(stdout))
				if plan.HasErrors || plan.ActionCount != 1 || len(plan.Actions) != 1 || plan.Actions[0].Scope != "global" || plan.Actions[0].Target != target {
					t.Fatalf("missing-root planning lost its selected native actor: %s", stdout)
				}
			}
			if _, err := os.Stat(hostRoot); !os.IsNotExist(err) {
				t.Fatalf("readiness created the selected root: %v", err)
			}
			if err := os.Mkdir(hostRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{
				{"status", "--manifest", manifest, "--json"},
				{"apply", "--manifest", manifest, "--dry-run", "--json"},
			} {
				code, stdout, stderr := testkit.RunCapturedCLI(args...)
				if code != 0 {
					t.Fatalf("existing selected global root failed: %d %s %s", code, stdout, stderr)
				}
				plan := clijson.DecodePlan(t, []byte(stdout))
				if plan.HasErrors || plan.ActionCount != 1 || len(plan.Actions) != 1 || plan.Actions[0].Scope != "global" {
					t.Fatalf("global native actor was dropped: %s", stdout)
				}
			}
			if _, err := os.Stat(filepath.Join(hostRoot, path)); !os.IsNotExist(err) {
				t.Fatalf("global native planning wrote host content: %v", err)
			}
		})
	}
}

func TestNativeDocumentCLIPlanningIsReadOnly(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	workspace := filepath.Join(root, "workspace")
	manifest := filepath.Join(workspace, "daem.toml")
	body := `version = 1
targets = ["codex"]
[[host_config]]
id = "preferences"
path = "extension/preferences.toml"
source = "./fragment"
ownership = "document"
`
	fragment := "model = \"native-test\"\n"
	testkit.WriteFile(t, workspace, "daem.toml", body)
	testkit.WriteFile(t, workspace, "fragment", fragment)
	if code, stdout, stderr := testkit.RunCapturedCLI("lock", "--manifest", manifest); code != 0 {
		t.Fatalf("lock failed: %d %s %s", code, stdout, stderr)
	}
	for _, args := range [][]string{
		{"status", "--manifest", manifest, "--json"},
		{"apply", "--manifest", manifest, "--dry-run", "--json"},
	} {
		code, stdout, stderr := testkit.RunCapturedCLI(args...)
		if code != 0 {
			t.Fatalf("whole document planning failed: %d %s %s", code, stdout, stderr)
		}
		plan := clijson.DecodePlan(t, []byte(stdout))
		if plan.HasErrors || len(plan.Actions) != 1 || plan.Actions[0].Kind != "create" {
			t.Fatalf("whole document creation was not disclosed: %+v", plan)
		}
	}
	if plan, err := apply.PlanDryRun(t.Context(), apply.CommandInput{ManifestPath: manifest}); err != nil || !plan.ReconciliationReady || plan.Reconciliation.ProjectionDecisionCount() != 1 {
		t.Fatalf("dry-run lost the actual whole document plan: %v", err)
	}
	for _, relative := range []string{".pi", ".codex", ".daem/state.json"} {
		if _, err := os.Stat(filepath.Join(workspace, relative)); !os.IsNotExist(err) {
			t.Fatalf("read-only planning created %s: %v", relative, err)
		}
	}
}
