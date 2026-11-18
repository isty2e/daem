package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestHostConfigDeclarationAuthoringSharedLifecycle(t *testing.T) {
	root := t.TempDir()
	nativefixture.SetRecoveryEnvironment(t, root)
	workspace := filepath.Join(root, "workspace")
	manifest := filepath.Join(workspace, "daem.toml")
	const original = `version = 1
targets = ["codex"]
host_config = [{ id = "retained", path = "config.toml", source = "retained.toml" }]
`
	const unmanaged = "# kept by the user\nmanual = true\n"
	const retained = "other = 'unchanged'\n"
	const selected = "model = 'locked-model'\n"
	testkit.WriteFile(t, workspace, "daem.toml", original)
	testkit.WriteFile(t, workspace, "retained.toml", retained)
	testkit.WriteFile(t, workspace, "selected.toml", selected)
	testkit.WriteFile(t, workspace, ".codex/config.toml", unmanaged)
	project, err := paths.Resolve(manifest)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(workspace, ".codex", "config.toml")
	add := []string{
		"add", "host-config", "preferences", filepath.Join(workspace, "selected.toml"),
		"--manifest", manifest, "--target", "codex", "--scope", "project", "--host-path", "config.toml",
	}
	diff := nativefixture.RunLifecycleCommand(t, append(slices.Clone(add), "--dry-run", "--diff")...)
	if !strings.Contains(diff, `id = "preferences"`) {
		t.Fatalf("preview did not disclose the declaration: %s", diff)
	}
	testkit.AssertFileContent(t, manifest, original)
	testkit.AssertPathMissing(t, project.LockfilePath)
	testkit.AssertFileContent(t, destination, unmanaged)

	added := clijson.DecodeManifestAuthoring(t, []byte(nativefixture.RunLifecycleCommand(t, append(slices.Clone(add), "--json")...)))
	if added.Operation != "add" || added.Lockfile == nil || added.Lockfile.Status != "written" || len(added.Changes) != 1 ||
		added.Changes[0].Resource.Kind != "host_config" || added.Changes[0].ResourceID != "host_config/preferences" || added.Host != nil ||
		!strings.HasPrefix(added.Changes[0].ManifestBlock, "{") {
		t.Fatalf("unexpected add result: %+v", added)
	}
	testkit.AssertFileContent(t, destination, unmanaged)
	testkit.AssertPathMissing(t, project.StatefilePath)
	before := testkit.ReadFile(t, manifest)
	if err := os.Remove(project.LockfilePath); err != nil {
		t.Fatal(err)
	}
	satisfied := clijson.DecodeManifestAuthoring(t, []byte(nativefixture.RunLifecycleCommand(t, append(slices.Clone(add), "--json")...)))
	if len(satisfied.Changes) != 0 || len(satisfied.Unchanged) != 1 || satisfied.Unchanged[0].Name != "preferences" || satisfied.Lockfile == nil || satisfied.Lockfile.Status != "written" {
		t.Fatalf("satisfied add did not restore the lock: %+v", satisfied)
	}
	if !bytes.Equal(before, testkit.ReadFile(t, manifest)) {
		t.Fatal("satisfied add changed the authored declaration bytes")
	}
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
	applied := testkit.ReadFile(t, destination)
	nativefixture.RequireSharedValues(t, configdocument.TOML, applied, selected, true)
	nativefixture.RequireSharedValues(t, configdocument.TOML, applied, retained+unmanaged, true)
	state := testkit.ReadFile(t, project.StatefilePath)
	registry := testkit.ReadFile(t, project.OwnershipRegistryPath)
	locked := testkit.ReadFile(t, project.LockfilePath)
	drifted := bytes.Replace(applied, []byte("locked-model"), []byte("drifted-model"), 1)
	if bytes.Equal(drifted, applied) {
		t.Fatal("fixture does not contain the selected value")
	}
	if err := os.WriteFile(destination, drifted, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"selected.toml", "retained.toml"} {
		if err := os.Remove(filepath.Join(workspace, file)); err != nil {
			t.Fatal(err)
		}
	}
	remove := []string{"remove", "host-config", "preferences", "--manifest", manifest, "--json"}
	if exit, stdout, stderr := testkit.RunCapturedCLI(remove...); exit != 1 || stderr != "" {
		t.Fatalf("remove with an unavailable retained source: %d %s %s", exit, stdout, stderr)
	} else {
		clijson.DecodeFailure(t, stdout, "remove host-config", "operation")
	}
	for path, expected := range map[string][]byte{manifest: before, project.LockfilePath: locked, destination: drifted} {
		testkit.AssertFileContent(t, path, string(expected))
	}
	testkit.WriteFile(t, workspace, "retained.toml", retained)
	removed := clijson.DecodeManifestAuthoring(t, []byte(nativefixture.RunLifecycleCommand(t, remove...)))
	if removed.Operation != "remove" || len(removed.Changes) != 1 || removed.Changes[0].ResourceID != "host_config/preferences" ||
		removed.Changes[0].ChangeKind != "remove selected-keys host-config declaration" || removed.Management != nil || removed.Host != nil {
		t.Fatalf("remove did not report an authoring-only change: %+v", removed)
	}
	for path, expected := range map[string][]byte{destination: drifted, project.StatefilePath: state, project.OwnershipRegistryPath: registry} {
		testkit.AssertFileContent(t, path, string(expected))
	}
	afterRemove, err := lockfile.Load(t.Context(), project.LockfilePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, subject := range afterRemove.Locked.Subjects() {
		if subject.EntityID().Kind() != entity.KindHostConfig || subject.EntityID().Name() != "retained" {
			t.Fatalf("remove kept an unexpected lock subject: %s", subject.EntityID())
		}
	}
	if afterRemove.Locked.Len() != 2 {
		t.Fatalf("want a source and one key for the retained declaration, got %d subjects", afterRemove.Locked.Len())
	}
	lockAfterRemove := testkit.ReadFile(t, project.LockfilePath)

	exit, stdout, stderr := testkit.RunCapturedCLI("apply", "--manifest", manifest, "--yes", "--json")
	if exit != 1 || stderr != "" {
		t.Fatalf("apply did not refuse drifted managed key: %d %s %s", exit, stdout, stderr)
	}
	blocked := clijson.DecodeApplyResult(t, []byte(stdout))
	driftReported := false
	for _, action := range blocked.Actions {
		driftReported = driftReported || (action.Resource.Name == "preferences" && action.Reason == "drifted_output")
	}
	if !blocked.HasErrors || !driftReported {
		t.Fatalf("drifted declaration did not block apply: %s", stdout)
	}
	for path, expected := range map[string][]byte{destination: drifted, project.StatefilePath: state, project.OwnershipRegistryPath: registry, project.LockfilePath: lockAfterRemove} {
		testkit.AssertFileContent(t, path, string(expected))
	}
	if err := os.WriteFile(destination, applied, 0o644); err != nil {
		t.Fatal(err)
	}
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
	remaining := testkit.ReadFile(t, destination)
	nativefixture.RequireSharedValues(t, configdocument.TOML, remaining, selected, false)
	nativefixture.RequireSharedValues(t, configdocument.TOML, remaining, retained+unmanaged, true)
	if !bytes.Contains(remaining, []byte("# kept by the user")) {
		t.Fatalf("cleanup removed an unrelated comment: %s", remaining)
	}
	nativefixture.RequireLifecycleMetadata(t, manifest, true)
	nativefixture.RequireLifecycleClean(t, nativefixture.RunLifecycleCommand(t, "status", "--manifest", manifest, "--check", "--json"))
}

func TestHostConfigDeclarationAuthoringWholeLifecycle(t *testing.T) {
	root := t.TempDir()
	nativefixture.SetRecoveryEnvironment(t, root)
	workspace := filepath.Join(root, "workspace")
	testkit.RequireGit(t)
	repository := testkit.InitGitRepository(t, root)
	const content = "# versioned preferences\nmodel = 'native-test'\n"
	testkit.WriteFile(t, repository, "settings/prefs.toml", content)
	commit := testkit.CommitRepository(t, repository, "versioned configuration")
	manifest := filepath.Join(workspace, "daem.toml")
	testkit.WriteFile(t, workspace, "daem.toml", "version = 1\ntargets = ['pi']\n")
	destination := filepath.Join(workspace, ".codex", "prefs.toml")
	nativefixture.RunLifecycleCommand(t, "add", "host-config", "dedicated", repository, "--path", "settings/prefs.toml", "--ref", commit,
		"--manifest", manifest, "--target", "codex", "--scope", "project", "--host-path", "prefs.toml", "--ownership", "document")
	testkit.AssertPathMissing(t, destination)
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
	testkit.AssertFileContent(t, destination, content)
	nativefixture.RequireLifecycleMetadata(t, manifest, true)

	removed := nativefixture.RunLifecycleCommand(t, "remove", "host-config", "dedicated", "--manifest", manifest)
	if !strings.Contains(removed, "change: remove whole-file host-config declaration") {
		t.Fatalf("whole-file declaration removal was not identified: %s", removed)
	}
	testkit.AssertFileContent(t, destination, content)
	nativefixture.RequireLifecycleMetadata(t, manifest, true)
	locked, err := lockfile.Load(t.Context(), filepath.Join(workspace, "daem.lock.toml"))
	if err != nil || locked.Locked.Len() != 0 {
		t.Fatalf("last declaration did not leave an empty lock: %v", err)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte(content+"# locally changed\n"), info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	assertRetainedDocumentAdvice := func() {
		t.Helper()
		code, stdout, stderr := testkit.RunCapturedCLI("apply", "--dry-run", "--manifest", manifest, "--target", "codex")
		if code != 1 || !strings.Contains(stdout+stderr, "daem unmanage host-config dedicated") {
			t.Errorf("blocked whole-file removal must point to management release: %d\n%s%s", code, stdout, stderr)
		}
	}
	assertRetainedDocumentAdvice()
	if err := os.Remove(destination); err != nil {
		t.Fatal(err)
	}
	assertRetainedDocumentAdvice()
	nativefixture.RunLifecycleCommand(t, "unmanage", "host-config", "dedicated", "--manifest", manifest, "--target", "codex", "--scope", "project", "--dry-run")
	nativefixture.RequireLifecycleMetadata(t, manifest, true)
	if err := os.WriteFile(destination, []byte(content), info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--target", "codex", "--yes")
	testkit.AssertPathMissing(t, destination)
	nativefixture.RequireLifecycleMetadata(t, manifest, false)
	nativefixture.RequireLifecycleClean(t, nativefixture.RunLifecycleCommand(t, "status", "--manifest", manifest, "--check", "--json"))
}
