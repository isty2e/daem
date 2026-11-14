//go:build darwin || linux

package metadataconversion

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
)

func TestCompilerQualifiesNineOwnersAndMaterializesOnlyOwnBefore(t *testing.T) {
	fixture := compilerFixture(t, 9)
	plan := compileFixture(t, fixture.paths)
	if !plan.Changed() || plan.ParticipantCount() != 9 || len(plan.intent.Targets()) != 11 {
		t.Fatal("compiler lost the whole binding-only owner closure")
	}
	for _, candidate := range plan.intent.Targets() {
		before := fixture.before[candidate.Path()]
		materialize := plan.materialize[candidate.Path()]
		if materialize == nil {
			t.Fatal("legacy source has no lazy materializer", candidate.Path())
		}
		after, err := materialize(t.Context(), before)
		if err != nil {
			t.Fatal(err)
		}
		if candidate.Path() == fixture.paths.OwnershipRegistryPath {
			image, err := ownershipstore.UnmarshalImage(after)
			if err != nil || !image.Registry().Equal(fixture.registry) || len(image.References()) != 18 {
				t.Fatal("ownership materialization changed facts or lost references", err)
			}
		} else if candidate.Path() == fixture.paths.CarrierClaimRegistryPath {
			image, err := carrierclaim.UnmarshalImage(after)
			if err != nil || !image.Registry().Equal(durablecarrier.EmptyGlobalCarrierClaims()) {
				t.Fatal("carrier materialization changed facts", err)
			}
		} else {
			image, err := statefile.UnmarshalImage(after)
			if err != nil || !image.Snapshot().Equal(fixture.snapshots[candidate.Path()]) {
				t.Fatal("State materialization changed durable facts", err)
			}
		}
		got, err := fileset.NewPublicationImage(after, candidate.After().Mode())
		if err != nil || !got.Equal(candidate.After()) {
			t.Fatal("lazy output differs from compiled image facts", err)
		}
	}
	fixture.assertUnchanged(t)
	if _, err := os.Stat(fixture.paths.GlobalMetadataDir()); !os.IsNotExist(err) {
		t.Fatal("read-only compilation created publication storage", err)
	}
	decoded, err := decodeSelection(plan.intent.Selection())
	if err != nil {
		t.Fatal(err)
	}
	if decoded.manifestPath != fixture.paths.ManifestPath || decoded.statefilePath != fixture.paths.StatefilePath || len(decoded.references.References()) != 18 {
		t.Fatal("retained selection lost original selection or complete membership")
	}
}

func TestCompilerRetainsMissingForeignStateWithoutFabrication(t *testing.T) {
	fixture := compilerFixture(t, 3)
	missing := fixture.states[2]
	if err := os.Remove(missing); err != nil {
		t.Fatal(err)
	}
	delete(fixture.before, missing)
	plan := compileFixture(t, fixture.paths)
	if plan.ParticipantCount() != 3 {
		t.Fatal("missing foreign State disappeared from the closure")
	}
	for _, candidate := range plan.intent.Targets() {
		if candidate.Path() == missing && (candidate.Before().Exists() || candidate.After().Exists() || plan.materialize[missing] != nil) {
			t.Fatal("missing foreign State became a fabricated empty image")
		}
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("compiler created missing State", err)
	}
	fixture.assertUnchanged(t)
}

func TestCompilerKeepsLocatedBytesAndRecognizesNoop(t *testing.T) {
	fixture := compilerFixture(t, 2)
	first := compileFixture(t, fixture.paths)
	for path, materialize := range first.materialize {
		after, err := materialize(t.Context(), fixture.before[path])
		if err != nil {
			t.Fatal(err)
		}
		// Whitespace is not rewritten merely because the image is already located.
		after = append(after, '\n', ' ')
		writeMetadata(t, path, after)
		fixture.before[path] = after
	}
	second := compileFixture(t, fixture.paths)
	if second.Changed() || len(second.materialize) != 0 {
		t.Fatal("located metadata was rewritten by a true no-op")
	}
	fixture.assertUnchanged(t)
}

func TestCompilerRejectsOtherOwnerPermissionsBeforeProducingAPlan(t *testing.T) {
	fixture := compilerFixture(t, 2)
	if err := os.Chmod(fixture.states[1], 0o640); err != nil {
		t.Fatal(err)
	}
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := compilePlan(t.Context(), compilerInput{Paths: fixture.paths, PhysicalWorkBudget: budget}, liveSources(budget), nil)
	if err == nil || plan.Changed() || len(plan.materialize) != 0 {
		t.Fatal("foreign owner failure admitted a partial conversion", err)
	}
	fixture.assertUnchanged(t)
}

func TestLiveSourceDoesNotProveAbsenceThroughAncestorAlias(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "original")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "other"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(parent); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "other"), parent); err != nil {
		t.Fatal(err)
	}
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := liveSources(budget)(t.Context(), filepath.Join(parent, "state.json"), stateSource)
	if err == nil || entry.image.Validate() == nil {
		t.Fatal("ancestor alias was admitted as proven absence", err)
	}
}

type compilationFixture struct {
	paths     daempaths.Paths
	states    []string
	before    map[string][]byte
	hosts     map[string][]byte
	snapshots map[string]durable.Snapshot
	registry  ownership.Registry
}

func compilerFixture(t *testing.T, count int) compilationFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fixture := compilationFixture{
		before: make(map[string][]byte), hosts: make(map[string][]byte), snapshots: make(map[string]durable.Snapshot),
	}
	var bindings []ownership.DocumentBinding
	for index := range count {
		paths, err := daempaths.Resolve(filepath.Join(root, fmt.Sprintf("project-%02d", index), "daem.toml"))
		if err != nil {
			t.Fatal(err)
		}
		paths, err = paths.WithDataDir(filepath.Join(root, "data"))
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			fixture.paths = paths
		}
		snapshot, err := durable.NewSnapshot(durable.SnapshotInput{NativeDocumentCommit: uint64(index + 4)})
		if err != nil {
			t.Fatal(err)
		}
		content, err := statefile.Marshal(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		writeMetadata(t, paths.StatefilePath, content)
		fixture.states = append(fixture.states, paths.StatefilePath)
		fixture.before[paths.StatefilePath] = content
		fixture.snapshots[paths.StatefilePath] = snapshot
		observed, err := mutation.ObservePersistedDirectoryEntryAuthority(paths.StatefilePath)
		if err != nil {
			t.Fatal(err)
		}
		owner, err := stateauthority.New(observed.Exact(), paths.ManifestPath)
		if err != nil {
			t.Fatal(err)
		}
		document := filepath.Join(paths.ManifestRoot, "document.json")
		host := []byte(fmt.Sprintf("{\"untouched\":%d}", index))
		writeMetadata(t, document, host)
		fixture.hosts[document] = host
		point, err := mutation.ObservePersistedDirectoryEntryAuthority(document)
		if err != nil {
			t.Fatal(err)
		}
		destination, err := output.Parse("document.json")
		if err != nil {
			t.Fatal(err)
		}
		binding, err := ownership.NewDocumentBinding(owner, destination, point.Exact())
		if err != nil {
			t.Fatal(err)
		}
		bindings = append(bindings, binding)
	}
	fixture.registry, err = ownership.NewRegistry(nil, bindings...)
	if err != nil {
		t.Fatal(err)
	}
	outputs, err := ownershipstore.Marshal(fixture.registry)
	if err != nil {
		t.Fatal(err)
	}
	writeMetadata(t, fixture.paths.OwnershipRegistryPath, outputs)
	fixture.before[fixture.paths.OwnershipRegistryPath] = outputs
	carriers, err := carrierclaim.Marshal(durablecarrier.EmptyGlobalCarrierClaims())
	if err != nil {
		t.Fatal(err)
	}
	writeMetadata(t, fixture.paths.CarrierClaimRegistryPath, carriers)
	fixture.before[fixture.paths.CarrierClaimRegistryPath] = carriers
	return fixture
}

func compileFixture(t *testing.T, paths daempaths.Paths) Plan {
	t.Helper()
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := compilePlan(t.Context(), compilerInput{Paths: paths, PhysicalWorkBudget: budget}, liveSources(budget), nil)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func writeMetadata(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
}

func (fixture compilationFixture) assertUnchanged(t *testing.T) {
	t.Helper()
	for _, collection := range []map[string][]byte{fixture.before, fixture.hosts} {
		for path, want := range collection {
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatal("read-only compilation changed a metadata or host file", path, err)
			}
		}
	}
}
