package journal

import (
	"context"
	"os"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeInitialObservationRefusesMissingSelectionAndWrongPhase(t *testing.T) {
	for _, fault := range []string{"reader", "budget", "admitted empty", "duplicate alias", "closed root", "canceled"} {
		t.Run(fault, func(t *testing.T) {
			fixture, input, reader, _ := nativeInitialFixture(t, target.ScopeProject, "record")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch fault {
			case "reader":
				input.registry = nil
			case "budget":
				input.budget = nil
			case "admitted empty":
				if err := input.budget.AdmitRemovalDemands(recovery.RemovalDemandSet{}); err != nil {
					t.Fatal(err)
				}
			case "duplicate alias":
				input.aliases = append(input.aliases, input.aliases[0])
			case "closed root":
				if err := input.roots.Manifest.Root.Close(); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				cancel()
			}
			observed, err := observeNativeDocumentBatch(ctx, input)
			if err == nil || observed.initialized || len(observed.originals) != 0 || len(observed.namespaces) != 0 || reader.loads != 0 {
				t.Fatalf("invalid initial selection exposed facts: %v", err)
			}
			fixture.requireUnchangedHost(t)
		})
	}
}

func TestNativeInitialObservationLateFailureDiscardsAllFacts(t *testing.T) {
	for _, fault := range []string{"cancellation", "byte budget", "phase change"} {
		t.Run(fault, func(t *testing.T) {
			fixture, input, reader, _ := nativeInitialFixture(t, target.ScopeGlobal, "record")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			reader.afterRead = func() {
				switch fault {
				case "cancellation":
					cancel()
				case "byte budget":
					work, err := recovery.NewArtifactWork(0, input.budget.RemainingBytes())
					if err != nil || input.budget.AdmitTree(work) != nil {
						t.Fatal("could not consume shared allocation", err)
					}
				case "phase change":
					if err := input.budget.AdmitRemovalDemands(recovery.RemovalDemandSet{}); err != nil {
						t.Fatal(err)
					}
				}
			}
			observed, err := observeNativeDocumentBatch(ctx, input)
			if err == nil || observed.initialized || len(observed.originals) != 0 || reader.loads != 1 {
				t.Fatal("late failure exposed a usable observation prefix", err)
			}
			fixture.requireUnchangedHost(t)
		})
	}
}

func TestNativeInitialContinuationRefusesOriginalAndLifetimeDrift(t *testing.T) {
	for _, fault := range []string{"content", "mode", "replacement", "closed root", "admitted", "canceled"} {
		t.Run(fault, func(t *testing.T) {
			fixture, input, reader, _ := nativeInitialFixture(t, target.ScopeProject, "record")
			batch, err := observeNativeDocumentBatch(t.Context(), input)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch fault {
			case "content":
				err = os.WriteFile(fixture.path, []byte("different"), 0o644)
			case "mode":
				err = os.Chmod(fixture.path, 0o400)
			case "replacement":
				err = os.Rename(fixture.path, fixture.path+".old")
				if err == nil {
					err = os.WriteFile(fixture.path, fixture.original.Content(), 0o644)
				}
			case "closed root":
				err = input.roots.Manifest.Root.Close()
			case "admitted":
				err = input.budget.AdmitRemovalDemands(recovery.RemovalDemandSet{})
			case "canceled":
				cancel()
			}
			if err != nil {
				t.Fatal(err)
			}
			captures, err := batch.prepareOwnership(ctx, fixture.envelope.after, fixture.input.operationID)
			if err == nil || captures != nil || reader.loads != 1 {
				t.Fatal("continuation accepted changed original/lifetime or reread Registry", err)
			}
		})
	}
	var absent nativeDocumentObservationBatch
	if captures, err := absent.prepareOwnership(t.Context(), durable.EmptySnapshot(), "operation"); err == nil || captures != nil {
		t.Fatal("zero observation became a usable continuation")
	}
}

func TestNativeInitialObservationPreservesMissingExactHistory(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			fixture, input, reader, registryPath := nativeInitialFixture(t, scope, "record")
			if err := os.Remove(fixture.path); err != nil {
				t.Fatal(err)
			}
			namespace, err := ObserveNativeDocumentNamespace(t.Context(), input.aliases, input.roots, input.filesystem, input.budget)
			if err != nil {
				t.Fatal(err)
			}
			if _, provisional := namespace.observation.Provisional(); !provisional {
				t.Skip("filesystem gives exact authority for the missing path")
			}
			beforeBytes, err := os.ReadFile(registryPath)
			if err != nil {
				t.Fatal(err)
			}
			batch, err := observeNativeDocumentBatch(t.Context(), input)
			if err != nil || !batch.initialized || reader.loads != 1 || reader.qualified != 1 {
				t.Fatal("actual missing history was not read under its namespace", err)
			}
			prepared, err := batch.prepareOwnership(t.Context(), durable.EmptySnapshot(), fixture.input.operationID)
			if err != nil || len(prepared) != 1 || len(prepared[0].bindings) != 1 {
				t.Fatal("matching history could not retire without a host write", err)
			}
			after, known := prepared[0].bindings[0].KnownAfter()
			if _, present := after.Get(); !known || present {
				t.Fatal("last native binding was not retired")
			}
			if scope == target.ScopeGlobal && (prepared[0].claims == nil || len(prepared[0].claims.Before()) != 1 || len(prepared[0].claims.After()) != 0) {
				t.Fatal("global exact history was manufactured or lost")
			}
			afterBytes, err := os.ReadFile(registryPath)
			if err != nil || string(beforeBytes) != string(afterBytes) {
				t.Fatal("preparation changed actual Registry content", err)
			}
			if _, err := os.Lstat(fixture.path); !os.IsNotExist(err) {
				t.Fatal("historical retirement wrote the missing host document", err)
			}
			if scope == target.ScopeGlobal {
				if _, err := reader.store.Load(t.Context()); err == nil {
					t.Fatal("default strict read was widened for missing history")
				}
			}
		})
	}
}
