//go:build darwin

package metadataconversion

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
)

func TestCompilerRejectsCurrentEnvironmentAsLostLegacyUserLocator(t *testing.T) {
	fixture := compilerFixture(t, 1)
	root := filepath.Dir(fixture.paths.ManifestRoot)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "İstanbul"))
	user, err := daempaths.Resolve(filepath.Join(root, "config", "daem", "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	oldContent, err := statefile.Marshal(durable.EmptySnapshot())
	if err != nil {
		t.Fatal(err)
	}
	writeMetadata(t, user.StatefilePath, oldContent)
	original, err := mutation.ObservePersistedDirectoryEntryAuthority(user.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(original.Exact().Witness(), "darwin-case-v2:") {
		t.Skip("volume does not expose the distinct Unicode-folding namespace required by this legacy case")
	}
	positiveBudget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	positive, err := newReferenceQualification(positiveBudget, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	qualified, err := positive.require(t.Context(), original.Exact(), []lookupCandidate{{path: user.StatefilePath, origin: lookupCurrentRoot}})
	if err != nil || !qualified.Equal(original.Reference()) {
		t.Fatal("version-proven current-root lookup was over-rejected", err)
	}
	legacyKey, available := original.Exact().LegacyComparisonKey()
	if !available {
		t.Fatal("fixture has no legacy comparison spelling")
	}
	legacy, err := pathauthority.NewExact(legacyKey, "darwin-case-v1:"+strings.ReplaceAll(strings.TrimPrefix(original.Exact().Witness(), "darwin-case-v2:"), "u", "i"))
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(legacy, user.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	point := filepath.Join(user.ManifestRoot, "owner-document.json")
	writeMetadata(t, point, []byte(`{"original":true}`))
	document, err := mutation.ObservePersistedDirectoryEntryAuthority(point)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse("owner-document.json")
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
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "istanbul"))
	current, err := daempaths.Resolve(user.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	neighborSnapshot, err := durable.NewSnapshot(durable.SnapshotInput{NativeDocumentCommit: 99})
	if err != nil {
		t.Fatal(err)
	}
	neighbor, err := statefile.Marshal(neighborSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	writeMetadata(t, current.StatefilePath, neighbor)
	observed, err := mutation.ObservePersistedDirectoryEntryAuthority(current.StatefilePath)
	if err != nil || !observed.Exact().Equal(legacy) {
		t.Fatal("fixture did not expose the same-family legacy collision", err)
	}
	authority, err := recoverygate.NewEffectAuthority(t.Context(), fixture.paths)
	if err != nil {
		t.Fatal(err)
	}
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := Prepare(t.Context(), authority, budget, nil)
	if !errors.Is(err, mutation.ErrLegacyReferenceUnprovable) {
		t.Fatal("current environment selected the distinct folded neighbor as a legacy owner", current.StatefilePath, plan.ParticipantCount())
	}
	current, err = current.WithDataDir(fixture.paths.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	selectedAuthority, err := recoverygate.NewEffectAuthority(t.Context(), current)
	if err != nil {
		t.Fatal(err)
	}
	selectedBudget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(t.Context(), selectedAuthority, selectedBudget, nil); !errors.Is(err, mutation.ErrLegacyReferenceUnprovable) {
		t.Fatal("current selected namespace laundered the colliding historical owner")
	}
	got, readErr := os.ReadFile(current.StatefilePath)
	if readErr != nil || string(got) != string(neighbor) {
		t.Fatal("read-only qualification changed the unrelated neighbor", readErr)
	}
}

func TestCaseAliasSelectionRetainsManifestAndCanonicalNamespace(t *testing.T) {
	fixture := compilerFixture(t, 1)
	aliasRoot := filepath.Join(filepath.Dir(fixture.paths.ManifestRoot), strings.ToUpper(filepath.Base(fixture.paths.ManifestRoot)))
	actual, err := os.Stat(fixture.paths.ManifestRoot)
	if err != nil {
		t.Fatal(err)
	}
	alias, err := os.Stat(aliasRoot)
	if err != nil || !os.SameFile(actual, alias) {
		t.Skip("volume does not provide the case-insensitive alias required by this selection")
	}
	paths, err := daempaths.Resolve(filepath.Join(aliasRoot, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	paths, err = paths.WithDataDir(fixture.paths.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	plan := prepareFixture(t, paths)
	if plan.selection.manifestPath != paths.ManifestPath || plan.selection.statefilePath != fixture.paths.StatefilePath {
		t.Fatal("original manifest selection or canonical State namespace changed")
	}
	if err := plan.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(fixture.paths.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	image, err := statefile.UnmarshalImage(content)
	if err != nil || image.Reference().Locator() != fixture.paths.StatefilePath {
		t.Fatal("converted State lost its canonical namespace", err)
	}
}
