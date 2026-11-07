package namespace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

func TestWriterObservationConsumesCapabilityWithoutOwningRoot(t *testing.T) {
	for _, failure := range []string{"none", "close", "canceled"} {
		t.Run(failure, func(t *testing.T) {
			root := nativeNamespaceRoot(t, t.TempDir())
			if err := os.MkdirAll(filepath.Join(root.SelectedPath, ".pi"), 0o700); err != nil {
				t.Fatal(err)
			}
			budget := imageBudget(t)
			qualified, err := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: root}).Qualify(t.Context(), budget)
			if err != nil {
				t.Fatal(err)
			}
			manifest, present := qualified.Manifest()
			if !present {
				t.Fatal("fixture has no manifest grant")
			}
			address := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/settings.json")
			relative, err := rootedpath.NewRelativeDestination(address.AggregateRoot().RelativePath())
			if err != nil {
				t.Fatal(err)
			}
			capability, err := manifest.acquireRoute(t.Context(), relative, budget)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			closeFailure := errors.New("injected writer close failure")
			incoming := capability
			var expected error
			switch failure {
			case "close":
				incoming, expected = writerCloseFailure{capability, closeFailure}, closeFailure
			case "canceled":
				cancel()
				expected = context.Canceled
			}

			observation, err := ObserveWriterCapability(ctx, incoming, budget)
			if expected != nil {
				if !errors.Is(err, expected) || observation.Validate() == nil {
					t.Fatalf("failed lease returned usable evidence: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if err := observation.Validate(); err != nil {
					t.Fatal("writer observation returned invalid path facts", err)
				}
			}
			if capability.Validate() == nil {
				_ = capability.Close()
				t.Fatal("writer observation retained the acquired capability")
			}
			if _, err := root.Root.Authority(); err != nil {
				t.Fatalf("writer observation closed its borrowed root: %v", err)
			}
		})
	}
	if value, err := ObserveWriterCapability(t.Context(), nil, imageBudget(t)); err == nil || value.Validate() == nil {
		t.Fatal("zero capability supplied a writer observation")
	}
}

type writerCloseFailure struct {
	rootedpath.CommitCapability
	err error
}

func (capability writerCloseFailure) Close() error {
	return errors.Join(capability.CommitCapability.Close(), capability.err)
}
