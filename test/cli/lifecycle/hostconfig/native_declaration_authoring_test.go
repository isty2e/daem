package cli_test

import (
	"bytes"
	"maps"
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

func TestAgentConfigRemovedKeysWithAbsentDocument(t *testing.T) {
	const removed = "version=1\ntargets=['codex']\n"
	const newKey = "notifications = true\n"
	for _, tc := range []struct {
		name       string
		manifest   string
		withNewKey bool
	}{
		{name: "retire without a parent directory", manifest: removed},
		{name: "add a disjoint key", manifest: removed + `[[agent_config]]
id = "new"
path = "config.toml"
source = "new.toml"
`, withNewKey: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := nativefixture.NewPresentation(t, "model = 'original'\n", "")
			nativefixture.RunLifecycleCommand(t, "apply", "--manifest", fixture.Manifest, "--yes")
			root := filepath.Dir(fixture.Destination)
			if err := os.Remove(fixture.Destination); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(root); err != nil {
				t.Fatal(err)
			}
			workspace := filepath.Dir(fixture.Manifest)
			testkit.WriteFile(t, workspace, "daem.toml", tc.manifest)
			if tc.withNewKey {
				testkit.WriteFile(t, workspace, "new.toml", newKey)
			}
			nativefixture.RunLifecycleCommand(t, "lock", "--manifest", fixture.Manifest)

			project, err := paths.Resolve(fixture.Manifest)
			if err != nil {
				t.Fatal(err)
			}
			observed := []string{root, project.StatefilePath, project.OwnershipRegistryPath, project.RecoveryDir}
			before := nativefixture.SnapshotRecoveryPaths(t, observed...)
			preview := nativefixture.RunLifecycleCommand(t, "apply", "--manifest", fixture.Manifest, "--dry-run", "--diff")
			if !maps.Equal(before, nativefixture.SnapshotRecoveryPaths(t, observed...)) {
				t.Fatal("preview changed output or recovery evidence")
			}
			applied := nativefixture.RunLifecycleCommand(t, "apply", "--manifest", fixture.Manifest, "--yes")

			for _, result := range []string{preview, applied} {
				if !strings.Contains(result, "key: model: retire management of missing output") {
					t.Fatalf("the absent key was not reported as record-only: %s", result)
				}
			}
			if tc.withNewKey {
				actual := testkit.ReadFile(t, fixture.Destination)
				nativefixture.RequireSharedValues(t, configdocument.TOML, actual, newKey, true)
				nativefixture.RequireSharedValues(t, configdocument.TOML, actual, "model = 'original'\n", false)
			} else {
				testkit.AssertPathMissing(t, root)
				if strings.Contains(preview, "@@") {
					t.Fatalf("the absent retirement reported a file diff: %s", preview)
				}
			}
			nativefixture.RequireLifecycleMetadata(t, fixture.Manifest, tc.withNewKey)
			nativefixture.RequireLifecycleClean(t, nativefixture.RunLifecycleCommand(t, "status", "--manifest", fixture.Manifest, "--check", "--json"))
			testkit.AssertNoRecoveryArtifacts(t, workspace)
		})
	}
}

func TestAgentConfigDeclarationAuthoringSharedLifecycle(t *testing.T) {
	root := t.TempDir()
	nativefixture.SetRecoveryEnvironment(t, root)
	workspace := filepath.Join(root, "workspace")
	manifest := filepath.Join(workspace, "daem.toml")
	const original = `version = 1
targets = ["codex"]
agent_config = [{ id = "retained", path = "config.toml", source = "retained.toml" }]
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
		"add", "agent-config", "preferences", filepath.Join(workspace, "selected.toml"),
		"--manifest", manifest, "--target", "codex", "--scope", "project", "--config-path", "config.toml",
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
		added.Changes[0].Resource.Kind != "agent-config" || added.Changes[0].ResourceID != "agent-config/preferences" || added.Host != nil ||
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
	remove := []string{"remove", "agent-config", "preferences", "--manifest", manifest, "--json"}
	if exit, stdout, stderr := testkit.RunCapturedCLI(remove...); exit != 1 || stderr != "" {
		t.Fatalf("remove with an unavailable retained source: %d %s %s", exit, stdout, stderr)
	} else {
		clijson.DecodeFailure(t, stdout, "remove agent-config", "operation")
	}
	for path, expected := range map[string][]byte{manifest: before, project.LockfilePath: locked, destination: drifted} {
		testkit.AssertFileContent(t, path, string(expected))
	}
	testkit.WriteFile(t, workspace, "retained.toml", retained)
	removed := clijson.DecodeManifestAuthoring(t, []byte(nativefixture.RunLifecycleCommand(t, remove...)))
	if removed.Operation != "remove" || len(removed.Changes) != 1 || removed.Changes[0].ResourceID != "agent-config/preferences" ||
		removed.Changes[0].ChangeKind != "remove selected-keys agent-config declaration" || removed.Management != nil || removed.Host != nil {
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
		if subject.EntityID().Kind() != entity.KindAgentConfig || subject.EntityID().Name() != "retained" {
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

func TestAgentConfigDeclarationAuthoringWholeLifecycle(t *testing.T) {
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
	nativefixture.RunLifecycleCommand(t, "add", "agent-config", "dedicated", repository, "--path", "settings/prefs.toml", "--ref", commit,
		"--manifest", manifest, "--target", "codex", "--scope", "project", "--config-path", "prefs.toml", "--ownership", "document")
	testkit.AssertPathMissing(t, destination)
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
	testkit.AssertFileContent(t, destination, content)
	nativefixture.RequireLifecycleMetadata(t, manifest, true)

	removed := nativefixture.RunLifecycleCommand(t, "remove", "agent-config", "dedicated", "--manifest", manifest)
	if !strings.Contains(removed, "change: remove whole-file agent-config declaration") {
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
	code, stdout, stderr := testkit.RunCapturedCLI("apply", "--dry-run", "--manifest", manifest, "--target", "codex")
	if code != 1 || !strings.Contains(stdout+stderr, "daem unmanage agent-config dedicated") {
		t.Fatalf("drifted whole-file removal must point to management release: %d\n%s%s", code, stdout, stderr)
	}
	nativefixture.RunLifecycleCommand(t, "unmanage", "agent-config", "dedicated", "--manifest", manifest, "--target", "codex", "--scope", "project", "--dry-run")
	nativefixture.RequireLifecycleMetadata(t, manifest, true)
	if err := os.WriteFile(destination, []byte(content), info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--target", "codex", "--yes")
	testkit.AssertPathMissing(t, destination)
	nativefixture.RequireLifecycleMetadata(t, manifest, false)
	nativefixture.RequireLifecycleClean(t, nativefixture.RunLifecycleCommand(t, "status", "--manifest", manifest, "--check", "--json"))
}

func TestAgentConfigRemovedWholeDocumentAlreadyAbsent(t *testing.T) {
	root := t.TempDir()
	nativefixture.SetRecoveryEnvironment(t, root)
	workspace := filepath.Join(root, "workspace")
	manifest := filepath.Join(workspace, "daem.toml")
	testkit.WriteFile(t, workspace, "daem.toml", `version = 1
targets = ["codex"]
[[agent_config]]
id = "dedicated"
path = "prefs.toml"
source = "prefs.toml"
ownership = "document"
`)
	const content = "model = 'original'\n"
	testkit.WriteFile(t, workspace, "prefs.toml", content)
	nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
	nativefixture.RequireLifecycleMetadata(t, manifest, true)
	dir := filepath.Join(workspace, ".codex")
	destination := filepath.Join(dir, "prefs.toml")
	removeOutput := func() {
		t.Helper()
		for _, name := range []string{destination, dir} {
			if err := os.Remove(name); err != nil {
				t.Fatal(err)
			}
		}
	}
	removeOutput()

	stillDesired := nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--dry-run", "--json")
	recreate := clijson.DecodePlan(t, []byte(stillDesired))
	if recreate.HasErrors || len(recreate.Actions) != 1 || recreate.Actions[0].Kind != "create" || recreate.Actions[0].Reason != "missing_output" {
		t.Fatalf("still-declared whole file should be recreated, not retired: %s", stillDesired)
	}
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
	testkit.AssertFileContent(t, destination, content)
	removeOutput()

	testkit.WriteFile(t, workspace, "daem.toml", "version = 1\ntargets = ['codex']\n")
	nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
	project, err := paths.Resolve(manifest)
	if err != nil {
		t.Fatal(err)
	}
	observed := []string{dir, project.StatefilePath, project.OwnershipRegistryPath, project.RecoveryDir}
	before := nativefixture.SnapshotRecoveryPaths(t, observed...)
	preview := nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--dry-run", "--diff")
	jsonPreview := nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--dry-run", "--json")
	plan := clijson.DecodePlan(t, []byte(jsonPreview))
	if plan.HasErrors || len(plan.Actions) != 1 || plan.Actions[0].Kind != "record" || plan.Actions[0].Reason != "removed_from_manifest" ||
		plan.Actions[0].Safety != "" || plan.Actions[0].DesiredHash != "" || strings.Contains(jsonPreview, `"desired_file_mode"`) {
		t.Fatalf("expected one record-only retirement without a requested file update: %s", jsonPreview)
	}
	if !maps.Equal(before, nativefixture.SnapshotRecoveryPaths(t, observed...)) {
		t.Fatal("preview changed the absent output or recovery evidence")
	}
	applied := nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
	if !strings.Contains(preview, "retire management of missing output") || strings.Contains(preview, "@@") ||
		!strings.Contains(applied, "retire management of missing output") {
		t.Fatalf("the file retirement was not disclosed as record-only: %s\n%s", preview, applied)
	}

	testkit.AssertPathMissing(t, dir)
	nativefixture.RequireLifecycleMetadata(t, manifest, false)
	nativefixture.RequireLifecycleClean(t, nativefixture.RunLifecycleCommand(t, "status", "--manifest", manifest, "--check", "--json"))
	settled := nativefixture.SnapshotRecoveryPaths(t, observed...)
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
	if !maps.Equal(settled, nativefixture.SnapshotRecoveryPaths(t, observed...)) {
		t.Fatal("reapplying changed settled retirement evidence")
	}
	testkit.AssertNoRecoveryArtifacts(t, workspace)
}
