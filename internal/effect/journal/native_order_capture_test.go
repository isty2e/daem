package journal

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestCaptureNativeJournalPublishesPiOrderOnlyWork(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			input, order := nativeOrderCaptureInput(t, scope)
			captured, err := input.capture(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if err := captured.Validate(); err != nil {
				t.Fatal(err)
			}
			if !captured.BeforeState().Equal(durable.EmptySnapshot()) || !captured.AfterState().Equal(captured.BeforeState()) {
				t.Fatal("Pi order-only capture invented managed state")
			}
			documents := captured.Documents()
			if len(documents) != 1 {
				t.Fatalf("captured %d order documents, want one", len(documents))
			}
			subjects, err := documents[0].SelectedSubjects(t.Context())
			if err != nil || len(subjects) != 0 || len(documents[0].BindingIntents()) != 0 {
				t.Fatalf("Pi order-only work acquired native membership: %v", err)
			}
			expected, mode := documents[0].Expected()
			if mode == nil || *mode != 0o644 {
				t.Fatal("Pi order-only work changed the host mode")
			}
			if err := order.VerifyPostContent(expected.Content(), expected.Exists()); err != nil {
				t.Fatal(err)
			}
			input.requireUnchanged(t)
		})
	}
}

func TestCaptureMembersRetainsOnlyChangedObservedPiOrderNamespaces(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			input, order := nativeOrderCaptureInput(t, scope)
			selected, members, err := input.batch.CaptureMembers(input.after, "native-record-apply", order)
			if err != nil || len(selected.Namespaces()) != 1 || len(members) != 1 {
				t.Fatalf("changed Pi order lost its observed actor: %v", err)
			}
			if len(members[0].BindingIntents()) != 0 || members[0].RequiresPromotion() {
				t.Fatal("Pi order-only actor invented native membership")
			}
			selected, members, err = input.batch.CaptureMembers(input.after, "native-record-apply")
			if err != nil || len(selected.Namespaces())+len(members) != 0 {
				t.Fatalf("unselected namespace was retained: %v", err)
			}
			loaded, err := order.CaptureInput()
			if err != nil {
				t.Fatal(err)
			}
			content, exists := order.Candidate()
			if !exists {
				t.Fatal("changed order lost its candidate")
			}
			loaded.Document = aggregate.ExistingDocument(content)
			unchanged, err := pipackage.ReconstructOrder(loaded)
			if err != nil || unchanged.Changed() {
				t.Fatalf("converged order was not unchanged: %v", err)
			}
			selected, members, err = input.batch.CaptureMembers(input.after, "native-record-apply", unchanged)
			if err != nil || len(selected.Namespaces())+len(members) != 0 {
				t.Fatalf("unchanged order retained an actor: %v", err)
			}
			otherScope := target.ScopeProject
			if scope == otherScope {
				otherScope = target.ScopeGlobal
			}
			_, missing := piEvidenceFixture(t, otherScope)
			selected, members, err = input.batch.CaptureMembers(input.after, "native-record-apply", missing)
			if err == nil || len(selected.Namespaces())+len(members) != 0 {
				t.Fatal("order without an observed namespace acquired a result")
			}
			input.requireUnchanged(t)
		})
	}
}

func nativeOrderCaptureInput(t *testing.T, scope target.Scope) (nativeCaptureTestInput, pipackage.OrderObservation) {
	t.Helper()
	loaded, order := piEvidenceFixture(t, scope)
	command, known := loaded.Relations[0].SourceContext()
	if !known {
		t.Fatal("Pi order fixture has no command context")
	}
	roots := NativeDocumentRoots{Manifest: nativeNamespaceRoot(t, command.BaseRoot())}
	selectedRoot := roots.Manifest
	if scope == target.ScopeGlobal {
		roots.PiAgent = nativeNamespaceRoot(t, loaded.SettingsContext.BaseRoot())
		selectedRoot = roots.PiAgent
	}
	root := roots.Manifest.SelectedPath
	paths := Paths{ManifestRoot: root, StatefilePath: filepath.Join(root, "state.json"), RecoveryDir: filepath.Join(root, "recovery")}
	manifest := filepath.Join(root, "daem.toml")
	requireWholeGroupWrite(t, manifest, []byte("version=1\ntargets=['pi']\n"), 0o600)
	fixture := nativeJournalFixture(t)
	before := durable.EmptySnapshot()
	stateBytes, err := fixture.codec.Encode(before)
	if err != nil {
		t.Fatal(err)
	}
	requireWholeGroupWrite(t, paths.StatefilePath, stateBytes, 0o600)
	budget := physicalwork.NewCaptureBudget(nil)
	state, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(paths.StatefilePath, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(state.Exact(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := ownership.NewRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	registryBytes, err := ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(root, "ownership.json")
	requireWholeGroupWrite(t, registryPath, registryBytes, 0o600)
	reader, err := ownershipstore.NewRecoveryReader(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	alias, err := piOrderDocumentAlias(scope)
	if err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(selectedRoot.SelectedPath, filepath.FromSlash(alias.AggregateRoot().RelativePath()))
	requireWholeGroupWrite(t, host, loaded.Document.Content(), 0o644)
	batch, err := ObserveNativeDocuments(t.Context(), NativeDocumentObservationInput{Owner: owner, Before: before, Aliases: []aggregate.DocumentAddress{alias}, Roots: roots, Filesystem: journalTestFilesystem(), Registry: reader, Limits: fixture.read.limits, Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := rootedpath.BindSelectedEntryAuthorityBounded(roots.Manifest.Root, root, filepath.Join(paths.RecoveryDir, "native-record-apply"), physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = entry.Close() })
	return nativeCaptureTestInput{
		paths: paths, batch: batch, after: before,
		options: NativeCaptureOptions{CaptureOptions: CaptureOptions{Filesystem: journalTestFilesystem(), StateCodec: fixture.codec, Codecs: fixture.read.codecs, ManifestRoot: roots.Manifest.Root, OperationAuthority: entry}, OrderObservations: []pipackage.OrderObservation{order}},
		host:    host, registry: registryPath, original: loaded.Document, stateBytes: stateBytes, registryBytes: registryBytes,
	}, order
}
