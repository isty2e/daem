package nativefixture

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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/statefile"
	clipkg "github.com/isty2e/daem/internal/cli"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/stateimage"
)

const (
	RecoveryHostEnv = "DAEM_TEST_NATIVE_RECOVERY_HOST"
	RecoveryExit    = 86
	RecoveryOutside = "# User preferences\nunmanaged = true\n[tui]\n# User theme\ntheme = 'dark'\n"
	RecoveryLater   = "# Added after process loss\nrecovery_note = 'keep'\n"
)

type RecoveryFixture struct {
	Manifest    string
	Destination string
	Paths       daempaths.Paths
	Kind        string
}

func NewRecoveryFixture(t *testing.T, kind string) RecoveryFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	SetRecoveryEnvironment(t, root)
	workspace := filepath.Join(root, "workspace")
	fixture := RecoveryFixture{Manifest: filepath.Join(workspace, "daem.toml"), Destination: filepath.Join(workspace, ".codex", "config.toml"), Kind: kind}
	if kind != "absent" {
		testkit.WriteFile(t, workspace, ".codex/config.toml", RecoveryOutside)
		if err := os.Chmod(fixture.Destination, 0o644); err != nil {
			t.Fatal(err)
		}
		WriteRecoveryDesired(t, fixture, "before-model", "old.js")
		RunRecoveryCommand(t, fixture, "none", "lock")
		RunRecoveryCommand(t, fixture, "none", "apply", "--yes")
		RequireSharedMode(t, fixture.Destination, RecoveryBeforeMode(kind))
		RequireLifecycleMetadata(t, fixture.Manifest, true)
	}
	WriteRecoveryDesired(t, fixture, "after-model", "new.js")
	paths, err := daempaths.Resolve(fixture.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	fixture.Paths = paths
	RunRecoveryCommand(t, fixture, "none", "lock")
	if kind == "absent" {
		for _, path := range []string{fixture.Destination, paths.StatefilePath, paths.OwnershipRegistryPath} {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("absent fixture invented prior native history at %s: %v", path, err)
			}
		}
	}
	return fixture
}

func SetRecoveryEnvironment(t *testing.T, root string) {
	t.Helper()
	RecoveryEnvironment(t, root, true)
}

func RecoveryEnvironment(t *testing.T, root string, create bool) {
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

func WriteRecoveryDesired(t *testing.T, fixture RecoveryFixture, model, server string) {
	t.Helper()
	manifest := `version = 1
targets = ["codex"]
[[host_config]]
id = "preferences"
path = "config.toml"
source = "./preferences.toml"
ownership = "keys"
`
	if fixture.Kind == "mixed" || fixture.Kind == "mixed-new" && model == "after-model" {
		manifest += fmt.Sprintf("[[mcp_server]]\nname = 'search'\ntransport = 'stdio'\ncommand = 'node'\nargs = [%q]\n", server)
	}
	workspace := filepath.Dir(fixture.Manifest)
	testkit.WriteFile(t, workspace, "daem.toml", manifest)
	testkit.WriteFile(t, workspace, "preferences.toml", fmt.Sprintf("model = %q\n", model))
}

func RecoveryMode(kind string) os.FileMode {
	if kind == "mixed" || kind == "mixed-new" || kind == "absent" {
		return 0o600
	}
	return 0o644
}

func RecoveryBeforeMode(kind string) os.FileMode {
	if kind == "mixed-new" {
		return 0o644
	}
	return RecoveryMode(kind)
}

func InterruptRecoveryApply(t *testing.T, fixture RecoveryFixture, boundary string) {
	t.Helper()
	code, stdout, stderr := InvokeRecoveryCommand(t, fixture, boundary, "apply", "--yes")
	path := fixture.Destination
	if boundary == "state" {
		path = fixture.Paths.StatefilePath
	}
	if code != RecoveryExit || !strings.Contains(stderr, RecoveryReceipt(path)) {
		t.Fatalf("helper did not lose the process after confirmed %s publication: exit=%d stdout=%s stderr=%s", boundary, code, stdout, stderr)
	}
	if !strings.Contains(stderr, RecoveryReceipt(fixture.Destination)) {
		t.Fatal("interruption did not pass actual host publication", stderr)
	}
	if boundary == "host" && strings.Contains(stderr, RecoveryReceipt(fixture.Paths.StatefilePath)) {
		t.Fatal("host interruption crossed the state publication boundary", stderr)
	}
	t.Logf("confirmed rooted %s publication; helper exited %d without unwinding Apply", boundary, code)
	content, err := os.ReadFile(fixture.Destination)
	if err != nil {
		t.Fatal(err)
	}
	RequireSharedValues(t, configdocument.TOML, content, "model='after-model'\n", true)
	if fixture.Kind == "mixed" || fixture.Kind == "mixed-new" {
		RequireSharedValues(t, configdocument.TOML, content, "[mcp_servers.search]\ncommand='node'\nargs=['new.js']\n", true)
	}
	RequireSharedMode(t, fixture.Destination, RecoveryMode(fixture.Kind))
}

func RequireRecoveryJournalState(t *testing.T, fixture RecoveryFixture, boundary string) {
	t.Helper()
	entries, err := os.ReadDir(fixture.Paths.RecoveryDir)
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("interrupted producer did not leave one actual operation directory: %v %v", entries, err)
	}
	content, err := os.ReadFile(filepath.Join(fixture.Paths.RecoveryDir, entries[0].Name(), "journal.json"))
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
	if err := json.Unmarshal(content, &record); err != nil || record.Version != 17 || len(record.DocumentGroups) != 1 || len(record.Entries) != 0 {
		t.Fatalf("producer did not capture a native-only v17 document journal: version=%d documents=%d entries=%d: %v", record.Version, len(record.DocumentGroups), len(record.Entries), err)
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
	current, err := statefile.LoadOptionalWithOptions(t.Context(), fixture.Paths.StatefilePath, statefile.ReadOptions{Codec: stateimage.Physical(t, codec), PhysicalPathBudget: physicalwork.NewCaptureBudget(nil)})
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
	t.Logf("actual v17 journal retained; persisted state is %s", map[string]string{"host": "Before", "state": "After"}[boundary])
}

func RequireRecoveryDryRun(t *testing.T, fixture RecoveryFixture, classification string, alternatives ...string) clijson.Recover {
	t.Helper()
	before := SnapshotRecoveryEvidence(t, fixture)
	code, stdout, stderr := InvokeRecoveryCommand(t, fixture, "none", "recover", "--dry-run", "--json")
	if !reflect.DeepEqual(before, SnapshotRecoveryEvidence(t, fixture)) {
		t.Fatal("fresh recover --dry-run mutated host, state, Registry or recovery artifacts")
	}
	if code != 0 {
		t.Fatalf("fresh recover --dry-run failed after actual producer interruption (want %s): exit=%d stdout=%s stderr=%s", classification, code, stdout, stderr)
	}
	payload := clijson.DecodeRecover(t, []byte(stdout))
	classifications := append([]string{classification}, alternatives...)
	if !slices.Contains(classifications, payload.Classification) || payload.HasErrors || payload.Phase != "planned" {
		t.Fatalf("fresh recovery did not classify the actual producer transition as one of %v: %s", classifications, stdout)
	}
	return payload
}

func RequireRecoveryBeforeHost(t *testing.T, fixture RecoveryFixture, later bool) {
	t.Helper()
	content, err := os.ReadFile(fixture.Destination)
	if fixture.Kind == "absent" {
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatal("rollback retained the originally absent destination", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	RequireSharedValues(t, configdocument.TOML, content, "model='before-model'\n", true)
	RequireSharedValues(t, configdocument.TOML, content, RecoveryOutside, true)
	RequireMCPUserComments(t, content)
	if fixture.Kind == "mixed" {
		RequireSharedValues(t, configdocument.TOML, content, "[mcp_servers.search]\ncommand='node'\nargs=['old.js']\n", true)
	}
	if fixture.Kind == "mixed-new" {
		RequireSharedValues(t, configdocument.TOML, content, "[mcp_servers.search]\ncommand='node'\nargs=['new.js']\n", false)
	}
	RequireSharedMode(t, fixture.Destination, RecoveryBeforeMode(fixture.Kind))
	if later {
		RequireRecoveryLaterEdit(t, fixture)
	}
}

func RequireRecoveryDesired(t *testing.T, fixture RecoveryFixture) {
	t.Helper()
	content, err := os.ReadFile(fixture.Destination)
	if err != nil {
		t.Fatal(err)
	}
	RequireSharedValues(t, configdocument.TOML, content, "model='after-model'\n", true)
	if fixture.Kind != "absent" {
		RequireSharedValues(t, configdocument.TOML, content, RecoveryOutside, true)
		RequireMCPUserComments(t, content)
	}
	if fixture.Kind == "mixed" || fixture.Kind == "mixed-new" {
		RequireSharedValues(t, configdocument.TOML, content, "[mcp_servers.search]\ncommand='node'\nargs=['new.js']\n", true)
	}
	RequireSharedMode(t, fixture.Destination, RecoveryMode(fixture.Kind))
	RequireLifecycleMetadata(t, fixture.Manifest, true)
	_, status, _ := RunRecoveryCommand(t, fixture, "none", "status", "--check", "--json")
	RequireLifecycleClean(t, status)
	_, _, _ = RunRecoveryCommand(t, fixture, "none", "apply", "--yes")
	repeated, err := os.ReadFile(fixture.Destination)
	if err != nil || !bytes.Equal(content, repeated) {
		t.Fatal("repeat Apply rewrote the recovered shared document", err)
	}
	testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.Manifest))
}

func PrependRecoveryUserEdit(t *testing.T, fixture RecoveryFixture) {
	t.Helper()
	content, err := os.ReadFile(fixture.Destination)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.Destination, append([]byte(RecoveryLater), content...), 0o600); err != nil {
		t.Fatal(err)
	}
}

func RequireRecoveryLaterEdit(t *testing.T, fixture RecoveryFixture) {
	t.Helper()
	content, err := os.ReadFile(fixture.Destination)
	if err != nil || !bytes.Contains(content, []byte(RecoveryLater)) {
		t.Fatal("recovery lost an unrelated value/comment added after process loss", err)
	}
}

type RecoveryImage struct {
	Mode    fs.FileMode
	Content string
}

func SnapshotRecoveryEvidence(t *testing.T, fixture RecoveryFixture) map[string]RecoveryImage {
	t.Helper()
	return SnapshotRecoveryPaths(t, fixture.Destination, fixture.Paths.StatefilePath, fixture.Paths.OwnershipRegistryPath, fixture.Paths.RecoveryDir)
}

func SnapshotRecoveryPaths(t *testing.T, paths ...string) map[string]RecoveryImage {
	t.Helper()
	images := make(map[string]RecoveryImage)
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
			image := RecoveryImage{Mode: info.Mode()}
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

func RunRecoveryCommand(t *testing.T, fixture RecoveryFixture, boundary string, args ...string) (int, string, string) {
	t.Helper()
	code, stdout, stderr := InvokeRecoveryCommand(t, fixture, boundary, args...)
	if code != 0 {
		t.Fatalf("%q failed: exit=%d stdout=%s stderr=%s", args, code, stdout, stderr)
	}
	return code, stdout, stderr
}

func InvokeRecoveryCommand(t *testing.T, fixture RecoveryFixture, boundary string, args ...string) (int, string, string) {
	t.Helper()
	if !packageMainInstalled {
		t.Fatal("native recovery invocation requires nativefixture.RunPackage in TestMain")
	}
	commandArgs := []string{"-test.run=^TestNativeKeyRecoveryCLIHelperProcess$", "--", boundary, fixture.Manifest}
	commandArgs = append(commandArgs, args...)
	commandArgs = append(commandArgs, "--manifest", fixture.Manifest)
	command := exec.CommandContext(t.Context(), os.Args[0], commandArgs...)
	command.Env = append(os.Environ(), RecoveryHelperEnv+"=1", RecoveryHostEnv+"="+fixture.Destination)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || t.Context().Err() != nil {
			t.Fatalf("native recovery helper failed to execute: %v; stdout=%s stderr=%s", err, stdout.String(), stderr.String())
		}
		if exit.ExitCode() == helperUnavailableExit {
			t.Fatalf("native recovery helper was unavailable: stdout=%s stderr=%s", stdout.String(), stderr.String())
		}
		return exit.ExitCode(), stdout.String(), stderr.String()
	}
	return 0, stdout.String(), stderr.String()
}

func RecoveryCLIHelperProcess(t *testing.T) {
	if os.Getenv(RecoveryHelperEnv) != "1" {
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
		os.Exit(helperUnavailableExit)
	}
	boundary, manifest := os.Args[separator+1], os.Args[separator+2]
	// TestMain gives each subprocess new default roots; reselect this fixture's
	// shared, temp-contained roots before invoking the real CLI.
	RecoveryEnvironment(t, filepath.Dir(filepath.Dir(manifest)), false)
	paths, err := daempaths.Resolve(manifest)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(helperUnavailableExit)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host := os.Getenv(RecoveryHostEnv)
	if host == "" {
		host = filepath.Join(filepath.Dir(manifest), ".codex", "config.toml")
	}
	store := &nativeRecoveryStore{
		Adapter: storagecommit.Adapter{}, boundary: boundary,
		host: host, state: paths.StatefilePath, cancel: cancel,
	}
	nativeAdoption := strings.HasPrefix(boundary, "native-adoption-")
	nativeInvocation := strings.HasPrefix(boundary, "native-ordinary-") || boundary == "native-removal-command" || nativeAdoption
	if boundary == "native-removal-command" {
		if err := os.Setenv("DAEM_TEST_REMOVAL_PARENT_PID", fmt.Sprint(os.Getpid())); err != nil {
			t.Fatal(err)
		}
	}
	if boundary == "native-refresh-command" {
		if err := os.Setenv("DAEM_TEST_REFRESH_PARENT_PID", fmt.Sprint(os.Getpid())); err != nil {
			t.Fatal(err)
		}
	}
	if boundary == "native-codex-command" {
		if err := os.Setenv("DAEM_TEST_CODEX_PARENT_PID", fmt.Sprint(os.Getpid())); err != nil {
			t.Fatal(err)
		}
	}
	if boundary == "native-opaque-command" {
		if err := os.Setenv("DAEM_TEST_OPAQUE_PARENT_PID", fmt.Sprint(os.Getpid())); err != nil {
			t.Fatal(err)
		}
	}
	if nativeInvocation {
		store.boundary = strings.TrimPrefix(boundary, "native-ordinary-")
		store.waitHost = true
		if nativeAdoption {
			store.boundary = strings.TrimPrefix(boundary, "native-adoption-")
			store.waitHost, store.waitWholeRecord = false, true
		}
	}
	execution := applyworkflow.ExecuteOptions{NativeFilesystem: store}
	execution.HostRouteExecutor = subprocess.NewCommandExecutor(subprocess.CommandOptions{Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
		if boundary != "carrier-install" || request.Command != "pi" || len(request.Args) != 3 || request.Args[0] != "install" || request.Args[2] != "-l" {
			t.Fatalf("native recovery fixture invoked an unexpected host route: %#v", request)
		}
		pending := ExtensionCompletionState(t, paths)
		if len(pending.PendingCarrierInstalls()) != 1 || len(pending.ManagedCarrierClaims()) != 0 {
			t.Fatal("host invocation preceded its exact durable pending install")
		}
		content, err := json.Marshal(map[string]any{"packages": []string{request.Args[1]}})
		if err != nil {
			t.Fatal(err)
		}
		testkit.WriteFile(t, filepath.Dir(manifest), ".pi/settings.json", string(content))
		os.Exit(RecoveryExit)
		return subprocess.CommandResult{}
	}})
	if nativeInvocation || boundary == "native-codex-command" || boundary == "native-opaque-command" {
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
	boundary        string
	host            string
	state           string
	failed          bool
	cancel          context.CancelFunc
	waitHost        bool
	hostPublished   bool
	waitWholeRecord bool
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
	if store.boundary == "before-state" && path == store.state && store.acceptsStatePublication(content) {
		os.Exit(RecoveryExit)
	}
	if err := store.Adapter.ReplaceRootedFile(ctx, capability, content, mode, expected); err != nil {
		return err
	}
	return store.afterPublication(path, content, mode)
}

func RecoveryReceipt(path string) string {
	return fmt.Sprintf("native recovery confirmed rooted publication %q", path)
}

func (store *nativeRecoveryStore) acceptsStatePublication(content []byte) bool {
	if !store.waitWholeRecord {
		return true
	}
	state, err := statefile.DecodePersistedFacts(context.Background(), content)
	if err != nil {
		return false
	}
	for _, record := range state.ManagedPaths() {
		if aggregate.IsHostConfigDocumentSubject(record.Subject()) {
			return true
		}
	}
	return false
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
	fmt.Fprintln(os.Stderr, RecoveryReceipt(path))
	if path == store.host {
		store.hostPublished = true
	}
	if store.boundary == "replace-host-with-symlink" && path == store.host && !store.failed {
		store.failed = true
		other := filepath.Join(filepath.Dir(path), "unselected-other.toml")
		for _, destination := range []string{other, other + ".before"} {
			if err := os.WriteFile(destination, expected, mode); err != nil {
				return err
			}
			if err := os.Chmod(destination, mode); err != nil {
				return err
			}
			past := time.Unix(1, 0)
			if err := os.Chtimes(destination, past, past); err != nil {
				return err
			}
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		if err := os.Symlink(filepath.Base(other), path); err != nil {
			return err
		}
		for _, destination := range []string{other, path} {
			info, err := os.Lstat(destination)
			if err != nil {
				return err
			}
			stat, err := json.Marshal(info.Sys())
			if err != nil {
				return err
			}
			if err := os.WriteFile(destination+".stat", stat, 0o600); err != nil {
				return err
			}
			if err := os.Chmod(destination+".stat", 0o600); err != nil {
				return err
			}
		}
	}
	if store.waitHost && !store.hostPublished {
		// A suffix checkpoint must not exit at the prefix's state publication.
		return nil
	}
	if path == store.state && !store.acceptsStatePublication(content) {
		return nil
	}
	if store.boundary == "host" && path == store.host || store.boundary == "state" && path == store.state {
		// Exit here, without returning to Apply or running its deferred cleanup.
		os.Exit(RecoveryExit)
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
