//go:build darwin || linux

package metadataconversion

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
)

func TestNoopDoesNotCreateMissingStateOrDataDirectories(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	paths, err := daempaths.Resolve(filepath.Join(root, "new-project", "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	paths, err = paths.WithDataDir(filepath.Join(root, "absent-data"))
	if err != nil {
		t.Fatal(err)
	}
	plan := prepareFixture(t, paths)
	if plan.Changed() {
		t.Fatal("absent metadata invented a conversion write")
	}
	if err := plan.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{paths.StateDir, paths.DataDir, paths.ManifestRoot} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("no-op created an absent namespace", path, err)
		}
	}
}

func TestLocatedRegistriesAdmitNewlySelectedLegacyStateWithoutRewritingPeers(t *testing.T) {
	fixture := compilerFixture(t, 2)
	initial := prepareFixture(t, fixture.paths)
	if err := initial.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}
	retained := make(map[string][]byte)
	for _, candidate := range initial.intent.Targets() {
		content, err := os.ReadFile(candidate.Path())
		if err != nil {
			t.Fatal(err)
		}
		retained[candidate.Path()] = content
	}
	paths, err := daempaths.Resolve(filepath.Join(filepath.Dir(fixture.paths.ManifestRoot), "late-project", "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	paths, err = paths.WithDataDir(fixture.paths.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := statefile.Marshal(durable.EmptySnapshot())
	if err != nil {
		t.Fatal(err)
	}
	writeMetadata(t, paths.StatefilePath, legacy)
	plan := prepareFixture(t, paths)
	if plan.ParticipantCount() != 3 || len(plan.materialize) != 1 || plan.materialize[paths.StatefilePath] == nil {
		t.Fatal("new selected State did not join the complete current cohort")
	}
	if err := plan.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}
	for path, before := range retained {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("late selected-State conversion rewrote a current peer", path, err)
		}
	}
}

func TestOrdinaryLocatedStateSelfReferenceDoesNotMakeItImmovable(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(root, "old-location", ".daem", "state.json")
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o700); err != nil {
		t.Fatal(err)
	}
	old, err := mutation.ObservePersistedDirectoryEntryAuthority(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{NativeDocumentCommit: 17})
	if err != nil {
		t.Fatal(err)
	}
	image, err := statefile.NewStateImage(snapshot, old.Reference())
	if err != nil {
		t.Fatal(err)
	}
	content, err := statefile.MarshalImage(image)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := daempaths.Resolve(filepath.Join(root, "new-location", "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	paths, err = paths.WithDataDir(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	writeMetadata(t, paths.StatefilePath, content)
	plan := prepareFixture(t, paths)
	if err := plan.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}
	result, err := os.ReadFile(paths.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	current, err := statefile.UnmarshalImage(result)
	if err != nil || !current.Snapshot().Equal(snapshot) || current.Reference().Locator() != paths.StatefilePath {
		t.Fatal("ordinary State acquired a new immovability rule", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("conversion followed or created the old self locator", err)
	}
}

func TestForeignLegacyUserIncludesItsRelocationTargetAsLocalGuard(t *testing.T) {
	fixture := compilerFixture(t, 1)
	root := filepath.Dir(fixture.paths.ManifestRoot)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	user, err := daempaths.Resolve(filepath.Join(root, "config", "daem", "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := user.LegacyUserState()
	if err != nil {
		t.Fatal(err)
	}
	state, err := statefile.Marshal(durable.EmptySnapshot())
	if err != nil {
		t.Fatal(err)
	}
	writeMetadata(t, legacy.StatefilePath, state)
	observed, err := mutation.ObservePersistedDirectoryEntryAuthority(legacy.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(observed.Exact(), user.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	point := filepath.Join(user.ManifestRoot, "document.json")
	writeMetadata(t, point, []byte(`{"unchanged":true}`))
	document, err := mutation.ObservePersistedDirectoryEntryAuthority(point)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse("document.json")
	if err != nil {
		t.Fatal(err)
	}
	binding, err := ownership.NewDocumentBinding(owner, destination, document.Exact())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := ownership.NewRegistry(nil, append(fixture.registry.DocumentBindings(), binding)...)
	if err != nil {
		t.Fatal(err)
	}
	content, err := ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	writeMetadata(t, fixture.paths.OwnershipRegistryPath, content)
	plan := prepareFixture(t, fixture.paths)
	if !slices.Contains(plan.selection.barrierDirs, user.StateDir) || plan.ParticipantCount() != 2 {
		t.Fatal("reachable legacy-user relocation peer was omitted")
	}
	unchangedBudget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compilePlan(t.Context(), compilerInput{Paths: fixture.paths, PhysicalWorkBudget: unchangedBudget}, liveSources(unchangedBudget), &plan.selection); err != nil {
		t.Fatal("unchanged retained-selection control failed", err)
	}
	originalStateHome := os.Getenv("XDG_STATE_HOME")
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "different-state"))
	replayBudget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compilePlan(t.Context(), compilerInput{Paths: fixture.paths, PhysicalWorkBudget: replayBudget}, liveSources(replayBudget), &plan.selection); err == nil {
		t.Fatal("changed user-root environment silently substituted another recovery peer")
	}
	t.Setenv("XDG_STATE_HOME", originalStateHome)
	alias, err := daempaths.Resolve(filepath.Join(user.ManifestRoot, "other.toml"))
	if err != nil {
		t.Fatal(err)
	}
	alias, err = alias.WithDataDir(fixture.paths.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	selectedLegacy := prepareFixture(t, alias)
	if !slices.Contains(selectedLegacy.selection.barrierDirs, user.StateDir) || selectedLegacy.selection.manifestPath != alias.ManifestPath {
		t.Fatal("selected legacy identity overwrote its recorded user provenance or original selection")
	}
	localMarker, err := fileset.FileSetAuthorityPath(user.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(localMarker, 0o700); err != nil {
		t.Fatal(err)
	}
	authority, err := recoverygate.NewEffectAuthority(t.Context(), fixture.paths)
	if err != nil {
		t.Fatal(err)
	}
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(t.Context(), authority, budget, nil); err == nil {
		t.Fatal("conversion ignored its reachable relocation peer's local marker")
	}
}

func TestLocatedLookupAliasIsQualifiedWithoutReplacingItsStoredSpelling(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	paths, err := daempaths.Resolve(filepath.Join(root, "project", "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	paths, err = paths.WithDataDir(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	writeMetadata(t, paths.StatefilePath, []byte("placeholder"))
	observed, err := mutation.ObservePersistedDirectoryEntryAuthority(paths.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "lookup")
	if err := os.Symlink(paths.StateDir, alias); err != nil {
		t.Fatal(err)
	}
	reference, err := pathauthority.NewReference(observed.Exact(), filepath.Join(alias, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	image, err := statefile.NewStateImage(durable.EmptySnapshot(), reference)
	if err != nil {
		t.Fatal(err)
	}
	content, err := statefile.MarshalImage(image)
	if err != nil {
		t.Fatal(err)
	}
	writeMetadata(t, paths.StatefilePath, content)
	plan := prepareFixture(t, paths)
	if plan.Changed() {
		t.Fatal("valid recorded lookup spelling was silently replaced")
	}
	stored, present := plan.selection.references.Reference(observed.Exact())
	if !present || !stored.Equal(reference) {
		t.Fatal("selection did not retain the recorded locator")
	}
	if err := plan.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(paths.StatefilePath)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatal("no-op changed existing located bytes", err)
	}
}

func TestManagedDocumentCannotAlsoBeAConversionStateTarget(t *testing.T) {
	fixture := compilerFixture(t, 1)
	_ = prepareFixture(t, fixture.paths)
	owner := fixture.registry.DocumentBindings()[0].Owner()
	destination, err := output.Parse(".daem/state.json")
	if err != nil {
		t.Fatal(err)
	}
	binding, err := ownership.NewDocumentBinding(owner, destination, owner.StatefileAuthority())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := ownership.NewRegistry(nil, append(fixture.registry.DocumentBindings(), binding)...)
	if err != nil {
		t.Fatal(err)
	}
	content, err := ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	writeMetadata(t, fixture.paths.OwnershipRegistryPath, content)
	fixture.before[fixture.paths.OwnershipRegistryPath] = content
	authority, err := recoverygate.NewEffectAuthority(t.Context(), fixture.paths)
	if err != nil {
		t.Fatal(err)
	}
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(t.Context(), authority, budget, nil); err == nil {
		t.Fatal("host document was admitted as a metadata rewrite target")
	}
	fixture.assertUnchanged(t)
}

func TestManagedDocumentCannotOccupyContainingEvidenceNamespace(t *testing.T) {
	fixture := compilerFixture(t, 1)
	_ = prepareFixture(t, fixture.paths)
	point := filepath.Join(fixture.paths.GlobalMetadataDir(), "host-document.json")
	host := []byte(`{"unrelated":true}`)
	writeMetadata(t, point, host)
	observed, err := mutation.ObservePersistedDirectoryEntryAuthority(point)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse("@data/metadata/host-document.json")
	if err != nil {
		t.Fatal(err)
	}
	binding, err := ownership.NewDocumentBinding(fixture.registry.DocumentBindings()[0].Owner(), destination, observed.Exact())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := ownership.NewRegistry(nil, append(fixture.registry.DocumentBindings(), binding)...)
	if err != nil {
		t.Fatal(err)
	}
	content, err := ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	writeMetadata(t, fixture.paths.OwnershipRegistryPath, content)
	fixture.before[fixture.paths.OwnershipRegistryPath] = content
	fixture.hosts[point] = host
	authority, err := recoverygate.NewEffectAuthority(t.Context(), fixture.paths)
	if err != nil {
		t.Fatal(err)
	}
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(t.Context(), authority, budget, []pathauthority.Reference{observed.Reference()}); err == nil {
		t.Fatal("evidence directory overlapped a managed document")
	}
	fixture.assertUnchanged(t)
}
