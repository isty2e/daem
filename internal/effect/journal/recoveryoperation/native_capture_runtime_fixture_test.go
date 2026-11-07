package recoveryoperation

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"

	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired/hostconfig"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/effect/journal/namespace/namespacetest"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output/ownership"

	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/realization/aggregate"

	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

type nativeCaptureTestInput struct {
	paths         journalpublication.Paths
	batch         journalpublication.NativeDocumentObservationBatch
	budget        *physicalwork.Budget
	after         durable.Snapshot
	edits         []compose.ProjectionEdit
	options       journalpublication.NativeCaptureOptions
	host          string
	registry      string
	original      aggregate.Document
	stateBytes    []byte
	registryBytes []byte
}

func nativeCaptureInput(t *testing.T, create bool, scope target.Scope) nativeCaptureTestInput {
	t.Helper()
	fixture := nativeJournalFixture(t)
	rootPath := t.TempDir()
	rootPath, err := filepath.EvalSymlinks(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	paths := journalpublication.Paths{ManifestRoot: rootPath, StatefilePath: filepath.Join(rootPath, "state.json"), RecoveryDir: filepath.Join(rootPath, "recovery")}
	manifest := filepath.Join(rootPath, "daem.toml")
	requireWholeGroupWrite(t, manifest, []byte("version=1\ntargets=['codex']\n"), 0o600)
	nativePath := "config.toml"
	if scope == target.ScopeGlobal {
		nativePath = "extension-settings/preferences.toml"
	}
	placement, err := profile.ResolveHostConfigPlacement(configuration.PlacementInput{Target: target.TargetCodex, Scope: scope, Path: nativePath, Ownership: hostconfig.OwnershipKeys})
	if err != nil {
		t.Fatal(err)
	}
	destination := placement.Destination()
	alias, err := aggregate.NewDocumentAddress(target.TargetCodex, scope, destination)
	if err != nil {
		t.Fatal(err)
	}
	before := membershipNativeRow(t, alias, "native-toml-v1", "model")
	before = selectionNativeRow(t, before.Contribution().Contract(), nativeRecordCanonical(t, "true"))
	after := selectionNativeRow(t, before.Contribution().Contract(), nativeRecordCanonical(t, `"fast"`))
	fixture.before, fixture.after = selectionState(t, before), selectionState(t, after)
	original := aggregate.ExistingDocument([]byte("model=true\n# user comment\nunmanaged=1\n"))
	if create {
		fixture.before, original = durable.EmptySnapshot(), aggregate.AbsentDocument()
	}
	fixture.read.Codecs, err = aggregatecodec.ForOperation(t.Context(), []aggregate.CodecContractID{after.Contribution().CodecContractID()}, fixture.read.Limits, fixture.read.Limits)
	if err != nil {
		t.Fatal(err)
	}
	fixture.codec, err = statefile.NewNativeCodec(fixture.read.Codecs.Contributions())
	if err != nil {
		t.Fatal(err)
	}
	stateBytes, err := fixture.codec.Encode(fixture.before)
	if err != nil {
		t.Fatal(err)
	}
	requireWholeGroupWrite(t, paths.StatefilePath, stateBytes, 0o600)
	host := filepath.Join(rootPath, ".codex", "config.toml")
	roots := namespacetest.LegacyRoots(t, map[output.RootRole]journalnamespace.RootSelection{output.RootProject: nativeNamespaceRoot(t, rootPath)})
	if scope == target.ScopeGlobal {
		roots = namespacetest.WithLegacyRoot(t, roots, output.RootCodexHome, nativeNamespaceRoot(t, filepath.Join(rootPath, "codex-home")))
		host = filepath.Join(namespacetest.LegacyRoot(t, roots, output.RootCodexHome).SelectedPath, filepath.FromSlash(destination.RelativePath()))
	}
	if original.Exists() {
		requireWholeGroupWrite(t, host, original.Content(), 0o644)
	}
	budget := physicalwork.NewCaptureBudget(nil)
	actualBefore, err := statefile.LoadOptionalWithOptions(t.Context(), paths.StatefilePath, statefile.ReadOptions{Codec: fixture.codec, PhysicalPathBudget: budget})
	if err != nil {
		t.Fatal(err)
	}
	stateAuthority, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(paths.StatefilePath, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(stateAuthority.Exact(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	var bindings []ownership.DocumentBinding
	var claims []ownership.Claim
	if original.Exists() {
		namespace, err := journalnamespace.Observe(t.Context(), []aggregate.DocumentAddress{alias}, roots, namespacetest.Reservations(t), journalTestFilesystem(), budget)
		if err != nil {
			t.Fatal(err)
		}
		exact, _ := namespace.Observation().Exact()
		binding, err := ownership.NewDocumentBinding(owner, destination, exact)
		if err != nil {
			t.Fatal(err)
		}
		bindings = append(bindings, binding)
		if scope == target.ScopeGlobal {
			address, err := ownership.NewManagedAddress(exact, before.Contribution().Contract().Address().Footprint())
			if err != nil {
				t.Fatal(err)
			}
			claim, err := ownership.NewActiveClaim(address, owner)
			if err != nil {
				t.Fatal(err)
			}
			claims = append(claims, claim)
		}
	}
	registry, err := ownership.NewRegistry(claims, bindings...)
	if err != nil {
		t.Fatal(err)
	}
	registryBytes, err := ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(rootPath, "ownership.json")
	requireWholeGroupWrite(t, registryPath, registryBytes, 0o600)
	reader, err := ownershipstore.NewRecoveryReader(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := journalpublication.ObserveNativeDocuments(t.Context(), journalobservation.Input{Owner: owner, Before: actualBefore, Aliases: []aggregate.DocumentAddress{alias}, Roots: roots, Protection: namespacetest.Reservations(t), Filesystem: journalTestFilesystem(), Registry: reader, Limits: fixture.read.Limits, Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := rootedpath.BindSelectedEntryAuthorityBounded(namespacetest.LegacyRoot(t, roots, output.RootProject).Root, rootPath, filepath.Join(paths.RecoveryDir, fixture.journal.OperationID()), physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = entry.Close() })
	return nativeCaptureTestInput{
		paths: paths, batch: batch, budget: budget, after: fixture.after,
		edits:   selectedCaptureEdits(t, fixture, original, []aggregate.ProjectionContract{after.Contribution().Contract()}, []durable.ManagedAggregateState{after}),
		options: journalpublication.NativeCaptureOptions{CaptureOptions: journalpublication.CaptureOptions{Filesystem: journalTestFilesystem(), StateCodec: fixture.codec, Codecs: fixture.read.Codecs, ManifestRoot: namespacetest.LegacyRoot(t, roots, output.RootProject).Root, OperationAuthority: entry}},
		host:    host, registry: registryPath, original: original, stateBytes: stateBytes, registryBytes: registryBytes,
	}
}

func (input nativeCaptureTestInput) capture(ctx context.Context) (journalpublication.CapturedNativeJournal, error) {
	return journalpublication.CaptureNativeJournalWithOptions(ctx, input.paths, "native-record-apply", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), input.batch, input.after, input.edits, input.options)
}

func (input nativeCaptureTestInput) requireUnchanged(t *testing.T) {
	t.Helper()
	content, err := os.ReadFile(input.host)
	if input.original.Exists() {
		if err != nil || !bytes.Equal(content, input.original.Content()) {
			t.Fatalf("capture changed host content: %q %v", content, err)
		}
		info, err := os.Stat(input.host)
		if err != nil || info.Mode().Perm() != 0o644 {
			t.Fatalf("capture changed host mode: %v", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("capture created a host document: %v", err)
	}
	for path, expected := range map[string][]byte{input.paths.StatefilePath: input.stateBytes, input.registry: input.registryBytes} {
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("capture changed durable state %q: %v", path, err)
		}
	}
}

func selectedCaptureEdits(t *testing.T, fixture nativeRecordFixture, original aggregate.Document, contracts []aggregate.ProjectionContract, desired []durable.ManagedAggregateState) []compose.ProjectionEdit {
	t.Helper()
	selection, err := aggregate.NewSelection(contracts)
	if err != nil {
		t.Fatal(err)
	}
	codec, found := fixture.read.Codecs.Lookup(selection.CodecContractID())
	if !found {
		t.Fatal("selected fixture codec missing")
	}
	baseline, failure := codec.Read(original, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	var intents []aggregate.ProjectionIntent
	for _, state := range baseline.States() {
		var values []aggregate.SubjectContribution
		for _, row := range desired {
			if row.Contribution().Contract().Equal(state.Contract()) {
				value, err := aggregate.NewSubjectContribution(row.Subject(), row.Contribution())
				if err != nil {
					t.Fatal(err)
				}
				values = append(values, value)
			}
		}
		var set *aggregate.ContributionSet
		if len(values) != 0 {
			value, err := aggregate.NewContributionSet(values)
			if err != nil {
				t.Fatal(err)
			}
			set = &value
		}
		intent, err := aggregate.NewProjectionIntent(state, set)
		if err != nil {
			t.Fatal(err)
		}
		intents = append(intents, intent)
	}
	plan, err := aggregate.NewPlan(baseline, intents)
	if err != nil {
		t.Fatal(err)
	}
	participant, err := journaldocument.Participant(selection, fixture.read.Codecs)
	if err != nil {
		t.Fatal(err)
	}
	edit, err := compose.NewProjectionEdit(codec, plan, participant.Syntax(), participant.RewriteAreas())
	if err != nil {
		t.Fatal(err)
	}
	return []compose.ProjectionEdit{edit}
}

func requireWholeGroupWrite(t *testing.T, path string, content []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatal(err)
	}
}
