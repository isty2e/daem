//go:build darwin || linux

package execute

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestRemovalBindingReusesCanonicalGlobalDestination(t *testing.T) {
	firstParent := filepath.Join(t.TempDir(), "first")
	secondParent := filepath.Join(t.TempDir(), "second")
	for _, parent := range []string{firstParent, secondParent} {
		if err := os.Mkdir(parent, 0o700); err != nil {
			t.Fatalf("create global parent %q: %v", parent, err)
		}
	}
	selectedPath := filepath.Join(firstParent, "config")
	destination := outputtest.Parse(t, "~/config")
	state, err := recovery.NewBeforeRemovalState(recovery.BeforePathState{
		Existed:       true,
		PathExisted:   true,
		ParentExisted: true,
		PathMode:      recovery.NewPermissionMode(0o600),
		Kind:          recovery.PathKindFile,
		ContentHash:   string(artifact.HashFileContent([]byte("inside"))),
	})
	if err != nil {
		t.Fatalf("construct removal state: %v", err)
	}
	demand, err := recovery.NewRemovalDemand(target.ScopeGlobal, destination, []recovery.RemovalState{state})
	if err != nil {
		t.Fatalf("construct removal demand: %v", err)
	}
	demands, err := recovery.NewRemovalDemandSet([]recovery.RemovalDemand{demand})
	if err != nil {
		t.Fatalf("construct removal demand set: %v", err)
	}
	resolverCalls := 0
	authority, err := captureScopedDestinationsForTest(
		Paths{},
		[]scopedDestinationForTest{{
			scope: target.ScopeGlobal, destination: destination,
		}},
		demands,
		nil,
		func(output.Destination) (string, error) {
			resolverCalls++
			if resolverCalls == 1 {
				return selectedPath, nil
			}
			return filepath.Join(secondParent, "config"), nil
		},
		testFilesystem(),
	)
	if err != nil {
		t.Fatalf("construct mutation authority: %v", err)
	}
	defer authority.close()
	if resolverCalls != 1 {
		t.Fatalf("global resolver calls = %d, want one bounded selection", resolverCalls)
	}
	canonical, err := authority.destinations.Resolve(target.ScopeGlobal, destination)
	if err != nil {
		t.Fatalf("resolve canonical global destination: %v", err)
	}
	provenance, err := canonical.RootedPath().Root().Provenance()
	if err != nil {
		t.Fatal(err)
	}
	parent, err := recovery.NewRootProvenance(provenance.PhysicalRoot(), provenance.ObjectFingerprint(), provenance.MountFingerprint())
	if err != nil {
		t.Fatal(err)
	}
	names, err := mutationfs.NewLogicalRemovalNames(".daem-tombstone-0123456789abcdef0123456789abcdef", ".daem-cleanup-0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := recovery.NewExistingParentAuthority(parent, names)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := recovery.NewRemovalIntent(demand, namespace)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.removals.BindIntents([]recovery.RemovalIntent{intent}); err != nil {
		t.Fatal(err)
	}
	removal, err := authority.removals.DestinationFor(target.ScopeGlobal, destination)
	if err != nil {
		t.Fatalf("prepared removal destination is missing: %v", err)
	}
	if removal.HostPath() != canonical.HostPath() {
		t.Fatalf(
			"removal destination = %q, want canonical mutation destination %q",
			removal.HostPath(),
			canonical.HostPath(),
		)
	}
	if !removal.RootedPath().Equal(canonical.RootedPath()) {
		t.Fatal("general mutation and removal did not retain one physical binding")
	}
}

func TestRemovalDemandBoundsProjectRootBeforeMutationBinding(t *testing.T) {
	physicalRoot := t.TempDir()
	components := make([]string, physicalwork.MaximumPhysicalPathDepth+1)
	for index := range components {
		components[index] = "d"
	}
	deepRoot := filepath.Join(append([]string{physicalRoot}, components...)...)
	if err := os.MkdirAll(deepRoot, 0o700); err != nil {
		t.Fatalf("create deep project root: %v", err)
	}
	alias := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(deepRoot, alias); err != nil {
		t.Fatalf("create project alias: %v", err)
	}
	destination := outputtest.Parse(t, ".agent/config")
	state, err := recovery.NewBeforeRemovalState(recovery.BeforePathState{
		Existed:       true,
		PathExisted:   true,
		ParentExisted: true,
		PathMode:      recovery.NewPermissionMode(0o600),
		Kind:          recovery.PathKindFile,
		ContentHash:   string(artifact.HashFileContent([]byte("inside"))),
	})
	if err != nil {
		t.Fatalf("construct removal state: %v", err)
	}
	demand, err := recovery.NewRemovalDemand(target.ScopeProject, destination, []recovery.RemovalState{state})
	if err != nil {
		t.Fatalf("construct removal demand: %v", err)
	}
	demands, err := recovery.NewRemovalDemandSet([]recovery.RemovalDemand{demand})
	if err != nil {
		t.Fatalf("construct removal demand set: %v", err)
	}
	authority, err := captureScopedDestinationsForTest(
		Paths{ManifestRoot: alias},
		[]scopedDestinationForTest{{
			scope: target.ScopeProject, destination: destination,
		}},
		demands,
		nil,
		destinationResolver(Paths{ManifestRoot: alias}),
		testFilesystem(),
	)
	if authority != nil {
		_ = authority.close()
	}
	if err == nil || !strings.Contains(err.Error(), "physical path depth") {
		t.Fatalf("construct mutation authority error = %v, want bounded project-root rejection", err)
	}
}
