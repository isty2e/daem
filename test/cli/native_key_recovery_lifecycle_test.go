package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
	"github.com/isty2e/daem/test/testkit"
)

const (
	nativeRecoveryHelperEnv = "GO_WANT_NATIVE_KEY_RECOVERY_HELPER"
	nativeRecoveryHostEnv   = "DAEM_TEST_NATIVE_RECOVERY_HOST"
	nativeRecoveryExit      = 86
	nativeRecoveryOutside   = "# User preferences\nunmanaged = true\n[tui]\n# User theme\ntheme = 'dark'\n"
	nativeRecoveryLater     = "# Added after process loss\nrecovery_note = 'keep'\n"
)

type nativeRecoveryFixture struct {
	manifest    string
	destination string
	paths       daempaths.Paths
	kind        string
}

func TestNativeKeyRecoveryProducerControl(t *testing.T) {
	for _, kind := range []string{"existing", "absent", "mixed", "mixed-new"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newNativeRecoveryFixture(t, kind)
			_, stdout, stderr := runNativeRecoveryCommand(t, fixture, "none", "apply", "--yes")
			for _, path := range []string{fixture.destination, fixture.paths.StatefilePath} {
				if !strings.Contains(stderr, nativeRecoveryReceipt(path)) {
					t.Fatalf("no-fault Apply did not confirm actual rooted publication at %s: stdout=%s stderr=%s", path, stdout, stderr)
				}
			}
			requireNativeRecoveryDesired(t, fixture)
		})
	}
}

func TestNativeKeyRecoveryAfterProcessLoss(t *testing.T) {
	for _, kind := range []string{"existing", "absent", "mixed", "mixed-new"} {
		for _, boundary := range []string{"host", "state"} {
			t.Run(kind+"/"+boundary, func(t *testing.T) {
				fixture := newNativeRecoveryFixture(t, kind)
				before := snapshotNativeRecoveryPaths(t, fixture.paths.StatefilePath)
				interruptNativeRecoveryApply(t, fixture, boundary)
				requireNativeRecoveryJournalState(t, fixture, boundary)
				if boundary == "host" && !reflect.DeepEqual(before, snapshotNativeRecoveryPaths(t, fixture.paths.StatefilePath)) {
					t.Fatal("host interruption did not retain the actual Before state")
				}

				if kind != "absent" {
					prependNativeRecoveryUserEdit(t, fixture)
				}
				unchangedHost, err := os.Stat(fixture.destination)
				if err != nil {
					t.Fatal(err)
				}
				unchangedHostImage := snapshotNativeRecoveryPaths(t, fixture.destination)
				classification := "needs_rollback"
				if boundary == "state" {
					classification = "clean_after"
					if kind == "absent" {
						classification = "needs_finalize"
					}
				}
				requireNativeRecoveryDryRun(t, fixture, classification)
				runNativeRecoveryCommand(t, fixture, "none", "recover", "--yes")
				testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.manifest))

				if boundary == "host" {
					if !reflect.DeepEqual(before, snapshotNativeRecoveryPaths(t, fixture.paths.StatefilePath)) {
						t.Fatal("rollback changed the persisted Before state")
					}
					requireNativeRecoveryBeforeHost(t, fixture, kind != "absent")
					requireNativeLifecycleMetadata(t, fixture.manifest, kind != "absent")
				} else {
					after, err := os.Stat(fixture.destination)
					if err != nil || !os.SameFile(unchangedHost, after) || !reflect.DeepEqual(unchangedHostImage, snapshotNativeRecoveryPaths(t, fixture.destination)) {
						t.Fatal("state-After finalization replayed the already committed host document", err)
					}
					requireNativeLifecycleMetadata(t, fixture.manifest, true)
					_, status, _ := runNativeRecoveryCommand(t, fixture, "none", "status", "--check", "--json")
					requireNativeLifecycleClean(t, status)
				}
				runNativeRecoveryCommand(t, fixture, "none", "apply", "--yes")
				requireNativeRecoveryDesired(t, fixture)
				if kind != "absent" {
					requireNativeRecoveryLaterEdit(t, fixture)
				}
			})
		}
	}
}

func TestNativeKeyRecoveryRefusesSelectedThirdValue(t *testing.T) {
	fixture := newNativeRecoveryFixture(t, "existing")
	interruptNativeRecoveryApply(t, fixture, "host")
	requireNativeRecoveryJournalState(t, fixture, "host")
	// First prove that this real journal is recoverable before introducing drift.
	requireNativeRecoveryDryRun(t, fixture, "needs_rollback")
	content, err := os.ReadFile(fixture.destination)
	if err != nil {
		t.Fatal(err)
	}
	changed := bytes.ReplaceAll(content, []byte("after-model"), []byte("third-model"))
	if bytes.Equal(changed, content) {
		t.Fatal("third-value fixture did not change the selected value")
	}
	if err := os.WriteFile(fixture.destination, changed, 0o644); err != nil {
		t.Fatal(err)
	}
	before := snapshotNativeRecoveryEvidence(t, fixture)
	for _, mode := range []string{"--dry-run", "--yes"} {
		code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture, "none", "recover", mode, "--json")
		if code == 0 {
			t.Fatalf("selected third value was accepted by %s: stdout=%s stderr=%s", mode, stdout, stderr)
		}
		payload := decodeRecoverJSONTestPayload(t, []byte(stdout))
		if payload.Classification != "blocked" || !payload.HasErrors {
			t.Fatalf("selected third value did not produce a blocked recovery plan: %s %s", stdout, stderr)
		}
		if !reflect.DeepEqual(before, snapshotNativeRecoveryEvidence(t, fixture)) {
			t.Fatal("refused recovery changed host, state, Registry or journal evidence")
		}
	}
}

func TestNativeKeyRecoveryCompensatesReturnedStorageError(t *testing.T) {
	for _, kind := range []string{"existing", "absent", "mixed", "mixed-new"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newNativeRecoveryFixture(t, kind)
			before := snapshotNativeRecoveryPaths(t, fixture.paths.StatefilePath)
			code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture, "return-error", "apply", "--yes", "--verbose")
			t.Logf("fault result exit=%d stdout=%s stderr=%s", code, stdout, stderr)
			if code == 0 || code == nativeRecoveryExit || !strings.Contains(stderr, "native recovery fixture: returned storage error") || !strings.Contains(stderr, nativeRecoveryReceipt(fixture.destination)) {
				t.Fatalf("Apply did not return the injected error after actual host publication: exit=%d stdout=%s stderr=%s", code, stdout, stderr)
			}
			requireNativeRecoveryBeforeHost(t, fixture, false)
			if !reflect.DeepEqual(before, snapshotNativeRecoveryPaths(t, fixture.paths.StatefilePath)) {
				t.Fatal("automatic compensation changed persisted Before state")
			}
			testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.manifest))
			runNativeRecoveryCommand(t, fixture, "none", "apply", "--yes")
			requireNativeRecoveryDesired(t, fixture)
		})
	}
}

func TestNativeKeyRecoveryCompensatesCancellation(t *testing.T) {
	for _, kind := range []string{"existing", "absent", "mixed", "mixed-new"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newNativeRecoveryFixture(t, kind)
			before := snapshotNativeRecoveryPaths(t, fixture.paths.StatefilePath)
			code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture, "cancel", "apply", "--yes", "--verbose")
			t.Logf("fault result exit=%d stdout=%s stderr=%s", code, stdout, stderr)
			if code == 0 || code == nativeRecoveryExit || !strings.Contains(stderr, "native recovery requested cancellation") || !strings.Contains(stderr, nativeRecoveryReceipt(fixture.destination)) {
				t.Fatalf("cancellation did not follow actual host publication: exit=%d stdout=%s stderr=%s", code, stdout, stderr)
			}
			requireNativeRecoveryBeforeHost(t, fixture, false)
			if !reflect.DeepEqual(before, snapshotNativeRecoveryPaths(t, fixture.paths.StatefilePath)) {
				t.Fatal("cancellation compensation changed persisted Before state")
			}
			testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.manifest))
			runNativeRecoveryCommand(t, fixture, "none", "apply", "--yes")
			requireNativeRecoveryDesired(t, fixture)
		})
	}
}

func TestNativeKeyRecoveryFinalizesReturnedStateErrorWithoutHostReplay(t *testing.T) {
	for _, kind := range []string{"existing", "absent", "mixed", "mixed-new"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newNativeRecoveryFixture(t, kind)
			code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture, "return-state-error", "apply", "--yes", "--verbose")
			t.Logf("fault result exit=%d stdout=%s stderr=%s", code, stdout, stderr)
			if code == 0 || code == nativeRecoveryExit || !strings.Contains(stderr, "native recovery fixture: returned storage error") || !strings.Contains(stderr, nativeRecoveryReceipt(fixture.paths.StatefilePath)) {
				t.Fatalf("Apply did not return the injected error after actual state publication: exit=%d stdout=%s stderr=%s", code, stdout, stderr)
			}
			if strings.Count(stderr, nativeRecoveryReceipt(fixture.destination)) != 1 {
				t.Fatalf("state-After settlement replayed host publication: %s", stderr)
			}
			requireNativeRecoveryDesired(t, fixture)
		})
	}
}

func newNativeRecoveryFixture(t *testing.T, kind string) nativeRecoveryFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	setNativeRecoveryEnvironment(t, root)
	workspace := filepath.Join(root, "workspace")
	fixture := nativeRecoveryFixture{manifest: filepath.Join(workspace, "daem.toml"), destination: filepath.Join(workspace, ".codex", "config.toml"), kind: kind}
	if kind != "absent" {
		testkit.WriteFile(t, workspace, ".codex/config.toml", nativeRecoveryOutside)
		if err := os.Chmod(fixture.destination, 0o644); err != nil {
			t.Fatal(err)
		}
		writeNativeRecoveryDesired(t, fixture, "before-model", "old.js")
		runNativeRecoveryCommand(t, fixture, "none", "lock")
		runNativeRecoveryCommand(t, fixture, "none", "apply", "--yes")
		requireNativeSharedMode(t, fixture.destination, nativeRecoveryBeforeMode(kind))
		requireNativeLifecycleMetadata(t, fixture.manifest, true)
	}
	writeNativeRecoveryDesired(t, fixture, "after-model", "new.js")
	paths, err := daempaths.Resolve(fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	fixture.paths = paths
	runNativeRecoveryCommand(t, fixture, "none", "lock")
	if kind == "absent" {
		for _, path := range []string{fixture.destination, paths.StatefilePath, paths.OwnershipRegistryPath} {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("absent fixture invented prior native history at %s: %v", path, err)
			}
		}
	}
	return fixture
}

func setNativeRecoveryEnvironment(t *testing.T, root string) {
	t.Helper()
	nativeRecoveryEnvironment(t, root, true)
}

func nativeRecoveryEnvironment(t *testing.T, root string, create bool) {
	t.Helper()
	for _, variable := range []string{"HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "CODEX_HOME", "PI_CODING_AGENT_DIR", "CLAUDE_CONFIG_DIR", "TMPDIR", "USERPROFILE", "APPDATA", "LOCALAPPDATA"} {
		path := filepath.Join(root, strings.ToLower(variable))
		if create {
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		t.Setenv(variable, path)
	}
}

func writeNativeRecoveryDesired(t *testing.T, fixture nativeRecoveryFixture, model, server string) {
	t.Helper()
	manifest := `version = 1
targets = ["codex"]
[[host_config]]
id = "preferences"
path = "config.toml"
source = "./preferences.toml"
ownership = "keys"
`
	if fixture.kind == "mixed" || fixture.kind == "mixed-new" && model == "after-model" {
		manifest += fmt.Sprintf("[[mcp_server]]\nname = 'search'\ntransport = 'stdio'\ncommand = 'node'\nargs = [%q]\n", server)
	}
	workspace := filepath.Dir(fixture.manifest)
	testkit.WriteFile(t, workspace, "daem.toml", manifest)
	testkit.WriteFile(t, workspace, "preferences.toml", fmt.Sprintf("model = %q\n", model))
}

func nativeRecoveryMode(kind string) os.FileMode {
	if kind == "mixed" || kind == "mixed-new" || kind == "absent" {
		return 0o600
	}
	return 0o644
}

func nativeRecoveryBeforeMode(kind string) os.FileMode {
	if kind == "mixed-new" {
		return 0o644
	}
	return nativeRecoveryMode(kind)
}

func interruptNativeRecoveryApply(t *testing.T, fixture nativeRecoveryFixture, boundary string) {
	t.Helper()
	code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture, boundary, "apply", "--yes")
	path := fixture.destination
	if boundary == "state" {
		path = fixture.paths.StatefilePath
	}
	if code != nativeRecoveryExit || !strings.Contains(stderr, nativeRecoveryReceipt(path)) {
		t.Fatalf("helper did not lose the process after confirmed %s publication: exit=%d stdout=%s stderr=%s", boundary, code, stdout, stderr)
	}
	if !strings.Contains(stderr, nativeRecoveryReceipt(fixture.destination)) {
		t.Fatal("interruption did not pass actual host publication", stderr)
	}
	if boundary == "host" && strings.Contains(stderr, nativeRecoveryReceipt(fixture.paths.StatefilePath)) {
		t.Fatal("host interruption crossed the state publication boundary", stderr)
	}
	t.Logf("confirmed rooted %s publication; helper exited %d without unwinding Apply", boundary, code)
	content, err := os.ReadFile(fixture.destination)
	if err != nil {
		t.Fatal(err)
	}
	requireNativeSharedValues(t, configdocument.TOML, content, "model='after-model'\n", true)
	if fixture.kind == "mixed" || fixture.kind == "mixed-new" {
		requireNativeSharedValues(t, configdocument.TOML, content, "[mcp_servers.search]\ncommand='node'\nargs=['new.js']\n", true)
	}
	requireNativeSharedMode(t, fixture.destination, nativeRecoveryMode(fixture.kind))
}

func requireNativeRecoveryJournalState(t *testing.T, fixture nativeRecoveryFixture, boundary string) {
	t.Helper()
	entries, err := os.ReadDir(fixture.paths.RecoveryDir)
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("interrupted producer did not leave one actual operation directory: %v %v", entries, err)
	}
	content, err := os.ReadFile(filepath.Join(fixture.paths.RecoveryDir, entries[0].Name(), "journal.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		Version         int               `json:"version"`
		DocumentGroups  []json.RawMessage `json:"document_groups"`
		Entries         []json.RawMessage `json:"entries"`
		StatefileBefore json.RawMessage   `json:"statefile_before"`
		StatefileAfter  json.RawMessage   `json:"statefile_after"`
	}
	if err := json.Unmarshal(content, &record); err != nil || record.Version != 16 || len(record.DocumentGroups) != 1 || len(record.Entries) != 0 {
		t.Fatalf("producer did not capture a native-only v16 document journal: %s; %v", content, err)
	}
	codecs, err := nativeconfig.Codecs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	codec, err := statefile.NewNativeCodec(codecs.Contributions())
	if err != nil {
		t.Fatal(err)
	}
	before, err := codec.Decode(record.StatefileBefore)
	if err != nil {
		t.Fatal(err)
	}
	after, err := codec.Decode(record.StatefileAfter)
	if err != nil || before.Equal(after) {
		t.Fatal("producer journal has no distinct actual state transition", err)
	}
	current, err := statefile.LoadOptionalWithOptions(t.Context(), fixture.paths.StatefilePath, statefile.ReadOptions{Codec: codec, PhysicalPathBudget: recovery.NewPhysicalCaptureBudget(nil)})
	if err != nil {
		t.Fatal(err)
	}
	want := before
	if boundary == "state" {
		want = after
	}
	if !current.Equal(want) {
		t.Fatalf("%s interruption has the wrong persisted state generation", boundary)
	}
	t.Logf("actual v16 journal retained; persisted state is %s", map[string]string{"host": "Before", "state": "After"}[boundary])
}

func requireNativeRecoveryDryRun(t *testing.T, fixture nativeRecoveryFixture, classification string) {
	t.Helper()
	before := snapshotNativeRecoveryEvidence(t, fixture)
	code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture, "none", "recover", "--dry-run", "--json")
	if !reflect.DeepEqual(before, snapshotNativeRecoveryEvidence(t, fixture)) {
		t.Fatal("fresh recover --dry-run mutated host, state, Registry or recovery artifacts")
	}
	if code != 0 {
		t.Fatalf("fresh recover --dry-run failed after actual producer interruption (want %s): exit=%d stdout=%s stderr=%s", classification, code, stdout, stderr)
	}
	payload := decodeRecoverJSONTestPayload(t, []byte(stdout))
	if payload.Classification != classification || payload.HasErrors || payload.Phase != "planned" {
		t.Fatalf("fresh recovery did not classify the actual producer transition as %s: %s", classification, stdout)
	}
}

func requireNativeRecoveryBeforeHost(t *testing.T, fixture nativeRecoveryFixture, later bool) {
	t.Helper()
	content, err := os.ReadFile(fixture.destination)
	if fixture.kind == "absent" {
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatal("rollback retained the originally absent destination", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	requireNativeSharedValues(t, configdocument.TOML, content, "model='before-model'\n", true)
	requireNativeSharedValues(t, configdocument.TOML, content, nativeRecoveryOutside, true)
	requireNativeMCPUserComments(t, content)
	if fixture.kind == "mixed" {
		requireNativeSharedValues(t, configdocument.TOML, content, "[mcp_servers.search]\ncommand='node'\nargs=['old.js']\n", true)
	}
	if fixture.kind == "mixed-new" {
		requireNativeSharedValues(t, configdocument.TOML, content, "[mcp_servers.search]\ncommand='node'\nargs=['new.js']\n", false)
	}
	requireNativeSharedMode(t, fixture.destination, nativeRecoveryBeforeMode(fixture.kind))
	if later {
		requireNativeRecoveryLaterEdit(t, fixture)
	}
}

func requireNativeRecoveryDesired(t *testing.T, fixture nativeRecoveryFixture) {
	t.Helper()
	content, err := os.ReadFile(fixture.destination)
	if err != nil {
		t.Fatal(err)
	}
	requireNativeSharedValues(t, configdocument.TOML, content, "model='after-model'\n", true)
	if fixture.kind != "absent" {
		requireNativeSharedValues(t, configdocument.TOML, content, nativeRecoveryOutside, true)
		requireNativeMCPUserComments(t, content)
	}
	if fixture.kind == "mixed" || fixture.kind == "mixed-new" {
		requireNativeSharedValues(t, configdocument.TOML, content, "[mcp_servers.search]\ncommand='node'\nargs=['new.js']\n", true)
	}
	requireNativeSharedMode(t, fixture.destination, nativeRecoveryMode(fixture.kind))
	requireNativeLifecycleMetadata(t, fixture.manifest, true)
	_, status, _ := runNativeRecoveryCommand(t, fixture, "none", "status", "--check", "--json")
	requireNativeLifecycleClean(t, status)
	_, _, _ = runNativeRecoveryCommand(t, fixture, "none", "apply", "--yes")
	repeated, err := os.ReadFile(fixture.destination)
	if err != nil || !bytes.Equal(content, repeated) {
		t.Fatal("repeat Apply rewrote the recovered shared document", err)
	}
	testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.manifest))
}

func prependNativeRecoveryUserEdit(t *testing.T, fixture nativeRecoveryFixture) {
	t.Helper()
	content, err := os.ReadFile(fixture.destination)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.destination, append([]byte(nativeRecoveryLater), content...), 0o600); err != nil {
		t.Fatal(err)
	}
}

func requireNativeRecoveryLaterEdit(t *testing.T, fixture nativeRecoveryFixture) {
	t.Helper()
	content, err := os.ReadFile(fixture.destination)
	if err != nil || !bytes.Contains(content, []byte(nativeRecoveryLater)) {
		t.Fatal("recovery lost an unrelated value/comment added after process loss", err)
	}
}

type nativeRecoveryImage struct {
	Mode    fs.FileMode
	Content string
}

func snapshotNativeRecoveryEvidence(t *testing.T, fixture nativeRecoveryFixture) map[string]nativeRecoveryImage {
	t.Helper()
	return snapshotNativeRecoveryPaths(t, fixture.destination, fixture.paths.StatefilePath, fixture.paths.OwnershipRegistryPath, fixture.paths.RecoveryDir)
}

func snapshotNativeRecoveryPaths(t *testing.T, paths ...string) map[string]nativeRecoveryImage {
	t.Helper()
	images := make(map[string]nativeRecoveryImage)
	for _, path := range paths {
		err := filepath.WalkDir(path, func(path string, entry fs.DirEntry, err error) error {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			image := nativeRecoveryImage{Mode: info.Mode()}
			if info.Mode().IsRegular() {
				content, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				image.Content = string(content)
			} else if !info.IsDir() {
				return fmt.Errorf("unexpected non-regular fixture evidence at %s", path)
			}
			images[path] = image
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return images
}

func runNativeRecoveryCommand(t *testing.T, fixture nativeRecoveryFixture, boundary string, args ...string) (int, string, string) {
	t.Helper()
	code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture, boundary, args...)
	if code != 0 {
		t.Fatalf("%q failed: exit=%d stdout=%s stderr=%s", args, code, stdout, stderr)
	}
	return code, stdout, stderr
}

func invokeNativeRecoveryCommand(t *testing.T, fixture nativeRecoveryFixture, boundary string, args ...string) (int, string, string) {
	t.Helper()
	commandArgs := []string{"-test.run=^TestNativeKeyRecoveryCLIHelperProcess$", "--", boundary, fixture.manifest}
	commandArgs = append(commandArgs, args...)
	commandArgs = append(commandArgs, "--manifest", fixture.manifest)
	command := exec.CommandContext(t.Context(), os.Args[0], commandArgs...)
	command.Env = append(os.Environ(), nativeRecoveryHelperEnv+"=1", nativeRecoveryHostEnv+"="+fixture.destination)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || t.Context().Err() != nil {
			t.Fatalf("native recovery helper failed to execute: %v; stdout=%s stderr=%s", err, stdout.String(), stderr.String())
		}
		return exit.ExitCode(), stdout.String(), stderr.String()
	}
	return 0, stdout.String(), stderr.String()
}

func TestNativeKeyRecoveryCLIHelperProcess(t *testing.T) {
	if os.Getenv(nativeRecoveryHelperEnv) != "1" {
		return
	}
	separator := -1
	for index, arg := range os.Args {
		if arg == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+3 >= len(os.Args) {
		fmt.Fprintln(os.Stderr, "native recovery helper arguments are missing")
		os.Exit(2)
	}
	boundary, manifest := os.Args[separator+1], os.Args[separator+2]
	// TestMain gives each subprocess new default roots; reselect this fixture's
	// shared, temp-contained roots before invoking the real CLI.
	nativeRecoveryEnvironment(t, filepath.Dir(filepath.Dir(manifest)), false)
	paths, err := daempaths.Resolve(manifest)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host := os.Getenv(nativeRecoveryHostEnv)
	if host == "" {
		host = filepath.Join(filepath.Dir(manifest), ".codex", "config.toml")
	}
	store := &nativeRecoveryStore{
		Adapter: storagecommit.Adapter{}, boundary: boundary,
		host: host, state: paths.StatefilePath, cancel: cancel,
	}
	nativeInvocation := boundary == "native-ordinary-host" || boundary == "native-ordinary-state" || boundary == "native-removal-command"
	if boundary == "native-removal-command" {
		if err := os.Setenv("DAEM_TEST_REMOVAL_PARENT_PID", fmt.Sprint(os.Getpid())); err != nil {
			t.Fatal(err)
		}
	}
	if nativeInvocation {
		store.boundary = strings.TrimPrefix(boundary, "native-ordinary-")
		store.waitHost = true
	}
	execution := applyworkflow.ExecuteOptions{NativeFilesystem: store}
	execution.HostRouteExecutor = subprocess.NewCommandExecutor(subprocess.CommandOptions{Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
		if boundary != "carrier-install" || request.Command != "pi" || len(request.Args) != 3 || request.Args[0] != "install" || request.Args[2] != "-l" {
			t.Fatalf("native recovery fixture invoked an unexpected host route: %#v", request)
		}
		pending := nativeExtensionCompletionState(t, paths)
		if len(pending.PendingCarrierInstalls()) != 1 || len(pending.ManagedCarrierClaims()) != 0 {
			t.Fatal("host invocation preceded its exact durable pending install")
		}
		content, err := json.Marshal(map[string]any{"packages": []string{request.Args[1]}})
		if err != nil {
			t.Fatal(err)
		}
		testkit.WriteFile(t, filepath.Dir(manifest), ".pi/settings.json", string(content))
		os.Exit(nativeRecoveryExit)
		return subprocess.CommandResult{}
	}})
	if nativeInvocation {
		execution.HostRouteExecutor = subprocess.NewCommandExecutor(subprocess.CommandOptions{})
	}
	code := clipkg.RunWithOptions(os.Args[separator+3:], clipkg.RunOptions{
		Context: ctx, Stdout: os.Stdout, Stderr: os.Stderr,
		ApplyExecuteOptions: execution,
	})
	os.Exit(code)
}

type nativeRecoveryStore struct {
	storagecommit.Adapter
	boundary      string
	host          string
	state         string
	failed        bool
	cancel        context.CancelFunc
	waitHost      bool
	hostPublished bool
}

func (store *nativeRecoveryStore) CreateRootedFile(ctx context.Context, capability rootedpath.CommitCapability, content []byte, mode fs.FileMode) error {
	path, err := capability.Destination().LexicalPath()
	if err != nil {
		return err
	}
	if err := store.Adapter.CreateRootedFile(ctx, capability, content, mode); err != nil {
		return err
	}
	return store.afterPublication(path, content, mode)
}

func (store *nativeRecoveryStore) ReplaceRootedFile(ctx context.Context, capability rootedpath.CommitCapability, content []byte, mode fs.FileMode, expected mutationfs.EntryIdentity) error {
	path, err := capability.Destination().LexicalPath()
	if err != nil {
		return err
	}
	if err := store.Adapter.ReplaceRootedFile(ctx, capability, content, mode, expected); err != nil {
		return err
	}
	return store.afterPublication(path, content, mode)
}

func nativeRecoveryReceipt(path string) string {
	return fmt.Sprintf("native recovery confirmed rooted publication %q", path)
}

func (store *nativeRecoveryStore) afterPublication(path string, expected []byte, mode fs.FileMode) error {
	if path != store.host && path != store.state {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(content, expected) || info.Mode().Perm() != mode.Perm() {
		return fmt.Errorf("native recovery publication was not confirmed at %s", path)
	}
	fmt.Fprintln(os.Stderr, nativeRecoveryReceipt(path))
	if path == store.host {
		store.hostPublished = true
	}
	if store.waitHost && !store.hostPublished {
		// A suffix checkpoint must not exit at the prefix's state publication.
		return nil
	}
	if store.boundary == "host" && path == store.host || store.boundary == "state" && path == store.state {
		// Exit here, without returning to Apply or running its deferred cleanup.
		os.Exit(nativeRecoveryExit)
	}
	if store.boundary == "cancel" && path == store.host && !store.failed {
		store.failed = true
		store.cancel()
		fmt.Fprintln(os.Stderr, "native recovery requested cancellation")
		return context.Canceled
	}
	if !store.failed && (store.boundary == "return-error" && path == store.host || store.boundary == "return-state-error" && path == store.state) {
		store.failed = true
		return errors.New("native recovery fixture: returned storage error")
	}
	return nil
}
