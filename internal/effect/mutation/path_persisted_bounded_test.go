package mutation

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
)

type pathAuthorityTestBudget struct {
	limit  int
	visits int
}

func (budget *pathAuthorityTestBudget) AdmitPathComponents(count int) error {
	if count < 0 || budget.visits+count > budget.limit {
		return errors.New("injected path-authority budget exhausted")
	}
	budget.visits += count
	return nil
}

func TestDirectoryEntryAuthorityBoundedRequiresCapacityBeforeObservation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write authority fixture: %v", err)
	}

	exhausted := &pathAuthorityTestBudget{}
	if _, err := CanonicalDirectoryEntryKeyBounded(path, 256, exhausted); err == nil {
		t.Fatal("bounded directory-entry key accepted an exhausted budget")
	}
	if exhausted.visits != 0 {
		t.Fatalf("failed key observation charged %d visits, want atomic rejection", exhausted.visits)
	}
	if _, err := ObserveDirectoryEntryAuthorityBounded(path, 256, exhausted); err == nil {
		t.Fatal("bounded directory-entry authority accepted an exhausted budget")
	}
	if authority, err := ObservePersistedDirectoryEntryAuthorityBounded(path, 256, exhausted); err == nil || !authority.Reference().IsZero() {
		t.Fatal("bounded persisted observation produced reference evidence without capacity", err)
	}
	if exhausted.visits != 0 {
		t.Fatalf("failed authority observation charged %d visits, want atomic rejection", exhausted.visits)
	}

	unboundedKey, err := CanonicalDirectoryEntryKey(path)
	if err != nil {
		t.Fatalf("unbounded directory-entry key: %v", err)
	}
	available := &pathAuthorityTestBudget{limit: 100_000}
	if key, err := CanonicalDirectoryEntryKeyBounded(path, 256, available); err != nil {
		t.Fatalf("bounded directory-entry key: %v", err)
	} else if key != unboundedKey {
		t.Fatalf("bounded directory-entry key = %q, want canonical key %q", key, unboundedKey)
	}
	general, err := ObserveDirectoryEntryAuthorityBounded(path, 256, available)
	if err != nil {
		t.Fatalf("bounded directory-entry authority: %v", err)
	}
	if available.visits == 0 {
		t.Fatal("bounded directory-entry authority performed no charged path work")
	}

	unboundedPersisted, err := ObservePersistedDirectoryEntryAuthority(path)
	if err != nil {
		t.Fatalf("unbounded persisted authority: %v", err)
	}
	persistedBudget := &pathAuthorityTestBudget{limit: 100_000}
	boundedPersisted, err := ObservePersistedDirectoryEntryAuthorityBounded(path, 256, persistedBudget)
	if err != nil {
		t.Fatalf("bounded persisted authority: %v", err)
	}
	if !boundedPersisted.Exact().Equal(unboundedPersisted.Exact()) {
		t.Fatal("bounded persisted authority changed canonical path semantics")
	}
	if !boundedPersisted.Reference().Equal(unboundedPersisted.Reference()) {
		t.Fatal("bounded persisted authority changed selected access spelling")
	}
	if reference, exact := general.Reference(); !exact || !reference.Equal(boundedPersisted.Reference()) {
		t.Fatal("general observation lost its same-epoch lookup spelling")
	}
	if persistedBudget.visits == 0 {
		t.Fatal("bounded persisted authority performed no charged path work")
	}
}

func TestDirectoryEntryObservationEqualityDoesNotPromoteProvisionalPaths(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Futuré.json")
	observe := func(path string) DirectoryEntryAuthorityObservation {
		t.Helper()
		provisional, err := pathauthority.NewProvisional(path, pathtest.DarwinCaseSensitive(path).Witness(), root, pathtest.DarwinCaseSensitive(root).Witness())
		if err != nil {
			t.Fatal(err)
		}
		return DirectoryEntryAuthorityObservation{provisional: provisional}
	}
	missing := observe(path)
	sibling := observe(filepath.Join(root, "Autré.json"))
	if !missing.Equal(observe(path)) || missing.Equal(sibling) || sibling.Equal(missing) {
		t.Fatal("missing observations lost their namespace distinction")
	}

	physical, err := pathauthority.NewExact(path, pathtest.DarwinCaseSensitive(path).Witness())
	if err != nil {
		t.Fatal(err)
	}
	exact := DirectoryEntryAuthorityObservation{exact: physical}
	if !exact.Equal(exact) || exact.Equal(missing) || missing.Equal(exact) {
		t.Fatal("equality promoted a missing observation to an exact path")
	}
	if !exact.MayOverlap(missing) || !missing.MayOverlap(exact) || !missing.MayOverlap(sibling) || !missing.MayOverlapExact(physical) {
		t.Fatal("provisional exclusion was mistaken for exact equality or disjointness")
	}
	var invalid DirectoryEntryAuthorityObservation
	if invalid.Equal(invalid) || exact.Equal(invalid) || invalid.Equal(missing) {
		t.Fatal("invalid observation compared equal")
	}
	if !invalid.MayOverlap(exact) || !exact.MayOverlap(invalid) || !invalid.MayOverlapExact(physical) {
		t.Fatal("invalid observations discarded possible overlap")
	}
	if reference, present := missing.Reference(); present || !reference.IsZero() {
		t.Fatal("provisional observation supplied exact lookup evidence")
	}
	if reference, present := exact.Reference(); present || !reference.IsZero() {
		t.Fatal("comparison-only identity invented lookup evidence")
	}
	if !exact.Equal(DirectoryEntryAuthorityObservation{exact: physical, locator: filepath.Join(root, "lookup.json")}) {
		t.Fatal("lookup spelling changed namespace-identity equality")
	}
}

func TestDirectoryEntryExclusionRetainsTheHistoricalDarwinWitness(t *testing.T) {
	root := t.TempDir()
	rootModes := strings.TrimPrefix(pathtest.DarwinCaseSensitive(root).Witness(), "darwin-case-v1:")
	current, err := pathauthority.NewExact(filepath.Join(root, "İtem.json"), "darwin-case-v2:"+rootModes+"u")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := pathauthority.NewExact(filepath.Join(root, "item.json"), "darwin-case-v1:"+rootModes+"i")
	if err != nil {
		t.Fatal(err)
	}
	observed := DirectoryEntryAuthorityObservation{exact: current}
	retained := DirectoryEntryAuthorityObservation{exact: legacy}
	if observed.Equal(retained) || !observed.MayOverlap(retained) || !retained.MayOverlap(observed) || !observed.MayOverlapExact(legacy) {
		t.Fatal("historical comparison was dropped or promoted to exact identity")
	}
	if reference, present := observed.Reference(); present || !reference.IsZero() {
		t.Fatal("comparison-only native spelling invented lookup evidence")
	}
}

func TestDirectoryEntryObservationRetainsCanonicalLookupReference(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("canonical lookup-spelling activation is Darwin/Linux only")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	physical := filepath.Join(root, "Physical")
	if err := os.Mkdir(physical, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(physical, alias); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(physical, "Document.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := &pathAuthorityTestBudget{limit: 100_000}
	observation, err := ObserveDirectoryEntryAuthorityBounded(filepath.Join(alias, "Document.json"), 256, budget)
	if err != nil {
		t.Fatal(err)
	}
	reference, exact := observation.Reference()
	identity, visible := observation.Exact()
	if !exact || !visible || !reference.Identity().Equal(identity) || reference.Locator() != filepath.Join(physical, "Document.json") {
		t.Fatal("exact observation reconstructed lookup spelling from a key or raw alias")
	}
}
