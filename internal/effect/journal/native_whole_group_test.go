package journal

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/payload"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeWholeGroupCapturesActualFilesAndReconstructs(t *testing.T) {
	for _, selected := range []target.Target{target.TargetPi, target.TargetCodex} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			for _, action := range []string{"create", "replace", "record", "remove"} {
				t.Run(string(selected)+"/"+string(scope)+"/"+action, func(t *testing.T) {
					fixture := wholeGroupFixture(t, selected, scope, action, []byte("opaque original\x00\xff\r\n"), []byte("desired\x00\xff\n"))
					group, comparison, err := prepareWholeDocumentGroup(t.Context(), fixture.namespace, fixture.input)
					if err != nil {
						t.Fatal(err)
					}
					if fixture.reader.imageReads != 1 || !comparison.BeforeDocument().Equal(fixture.original) || !comparison.ExpectedDocument().Equal(fixture.expected) {
						t.Fatal("capture did not derive images from one real rooted read and the selected mutation")
					}
					fixture.requireUnchangedHost(t)
					fixture.stageAndReconstruct(t, group, comparison)
					fixture.requireUnchangedHost(t)
				})
			}
		}
	}
}

func TestNativeWholeGroupPreservesEmptyFileAndActualPermission(t *testing.T) {
	fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeGlobal, "replace", nil, nil)
	group, comparison, err := prepareWholeDocumentGroup(t.Context(), fixture.namespace, fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	if group.Original.Kind != "file" || group.Expected.Kind != "file" || *group.Original.Mode != 0o644 || *group.Expected.Mode != 0o600 {
		t.Fatal("empty file or actual host permission was collapsed into absence/private witness permission")
	}
	fixture.stageAndReconstruct(t, group, comparison)
}

func TestNativeWholeGroupRetainsActualAliasesAndWriter(t *testing.T) {
	fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeGlobal, "replace", []byte("before"), []byte("after"))
	roots := fixture.namespace.roots
	if err := os.Symlink(roots.PiAgent.SelectedPath, filepath.Join(roots.Manifest.SelectedPath, ".pi")); err != nil {
		t.Fatal(err)
	}
	project := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/prompts/Caf\u00e9.md")
	aliases := append(fixture.namespace.Addresses(), project)
	namespace, err := ObserveNativeDocumentNamespace(t.Context(), aliases, roots, fixture.reader, fixture.input.budget)
	if err != nil {
		t.Fatal(err)
	}
	group, comparison, err := prepareWholeDocumentGroup(t.Context(), namespace, fixture.input)
	if err != nil || len(group.Aliases) != 2 || !comparison.BeforeDocument().Equal(fixture.original) {
		t.Fatalf("actual alias grouping failed: %v", err)
	}
	authority, err := roots.PiAgent.Root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := authority.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	if group.Writer.Root != persistedNativeRoot(provenance) || group.Writer.RelativePath != "prompts/Caf\u00e9.md" {
		t.Fatal("logical project alias replaced the independently selected physical writer")
	}
	for _, alias := range group.Aliases {
		if !alias.ObservedBefore.equal(group.CapturedTarget) || !alias.SelectedObservation.equal(group.CapturedTarget) {
			t.Fatal("alias does not retain the actual common referent")
		}
	}
	fixture.requireUnchangedHost(t)
}

func TestNativeWholeGroupRejectsProvisionalClaimMisjoinsBeforeRead(t *testing.T) {
	for _, mismatch := range []string{"owner", "operation", "alias", "exact claims"} {
		t.Run(mismatch, func(t *testing.T) {
			fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeGlobal, "create", nil, []byte("after"))
			if _, pending := fixture.namespace.observation.Provisional(); !pending {
				t.Skip("filesystem supplies exact authority for this absent normalization-sensitive path")
			}
			switch mismatch {
			case "owner":
				fixture.input.owner = documentClaimsForTest(t).owner
			case "operation":
				fixture.input.operationID = "another-operation"
			case "alias":
				fixture.input.provisional[0].alias = nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/other.md")
			case "exact claims":
				other := wholeGroupFixture(t, target.TargetPi, target.ScopeGlobal, "replace", []byte("before"), []byte("after"))
				fixture.input.claims, fixture.input.provisional = other.input.claims, nil
			}
			group, comparison, err := prepareWholeDocumentGroup(t.Context(), fixture.namespace, fixture.input)
			if err == nil || len(group.Aliases) != 0 || comparison.Validate() == nil || fixture.reader.imageReads != 0 {
				t.Fatalf("provisional mismatch escaped or read content: %v", err)
			}
			fixture.requireUnchangedHost(t)
		})
	}
}

func TestNativeWholeGroupRetainedBaselineModeMustMatch(t *testing.T) {
	for _, action := range []string{"record", "remove"} {
		t.Run(action, func(t *testing.T) {
			fixture := wholeGroupFixture(t, target.TargetCodex, target.ScopeProject, action, []byte("before"), nil)
			if err := os.Chmod(fixture.path, 0o600); err != nil {
				t.Fatal(err)
			}
			group, comparison, err := prepareWholeDocumentGroup(t.Context(), fixture.namespace, fixture.input)
			if err == nil || len(group.Aliases) != 0 || comparison.Validate() == nil {
				t.Fatalf("stale retained baseline mode escaped: %v", err)
			}
		})
	}
}

func TestNativeWholeGroupRejectsStaticMisjoinsBeforeContentRead(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*wholeGroupTestFixture)
	}{
		{"zero mutation", func(f *wholeGroupTestFixture) { f.input.mutation = ManagedPathMutation{} }},
		{"wrong alias", func(f *wholeGroupTestFixture) {
			other := wholeGroupFixture(t, target.TargetCodex, target.ScopeGlobal, "replace", []byte("other"), []byte("next"))
			f.input.mutation = other.input.mutation
		}},
		{"missing manifest grant", func(f *wholeGroupTestFixture) { f.namespace.roots.Manifest = NativeDocumentRoot{} }},
		{"wrong claim owner", func(f *wholeGroupTestFixture) { f.input.owner = documentClaimsForTest(t).owner }},
		{"wrong operation reservation", func(f *wholeGroupTestFixture) {
			transition, err := ownershipmutation.NewDocumentClaimTransition(nil, f.input.claims.After(), f.input.operationID)
			if err != nil {
				t.Fatal(err)
			}
			f.input.claims = &transition
			f.input.operationID = "another-operation"
		}},
		{"mixed claims", func(f *wholeGroupTestFixture) { f.input.provisional = []documentProvisionalAcquire{{}} }},
		{"wrong binding", func(f *wholeGroupTestFixture) { f.input.bindings = []ownership.DocumentBindingIntent{{}} }},
		{"negative ordinal", func(f *wholeGroupTestFixture) { f.input.groupIndex = -1 }},
		{"missing budget", func(f *wholeGroupTestFixture) { f.input.budget = nil }},
		{"invalid limits", func(f *wholeGroupTestFixture) { f.input.limits.MaximumBytes = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeGlobal, "replace", []byte("before"), []byte("after"))
			test.edit(&fixture)
			group, comparison, err := prepareWholeDocumentGroup(t.Context(), fixture.namespace, fixture.input)
			if err == nil || comparison.Validate() == nil || len(group.Aliases) != 0 || fixture.reader.imageReads != 0 {
				t.Fatalf("static misjoin escaped or read host content: reads=%d error=%v", fixture.reader.imageReads, err)
			}
			fixture.requireUnchangedHost(t)
		})
	}
}

func TestNativeWholeGroupRejectsValidButUnrelatedMetadataBeforeRead(t *testing.T) {
	for _, mismatch := range []string{"claim namespace", "binding namespace"} {
		t.Run(mismatch, func(t *testing.T) {
			fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeGlobal, "replace", []byte("before"), []byte("after"))
			other := wholeGroupFixture(t, target.TargetCodex, target.ScopeProject, "remove", []byte("other"), nil)
			exact, known := other.namespace.observation.Exact()
			if !known {
				t.Fatal("existing comparison namespace has no exact authority")
			}
			switch mismatch {
			case "claim namespace":
				claim := documentActiveClaim(t, documentClaimFixture{owner: fixture.input.owner, exact: exact}, output.WholePathFootprint())
				transition, err := ownershipmutation.NewDocumentClaimTransition([]ownership.Claim{claim}, []ownership.Claim{claim}, fixture.input.operationID)
				if err != nil {
					t.Fatal(err)
				}
				fixture.input.claims = &transition
			case "binding namespace":
				binding, err := ownership.NewDocumentBinding(fixture.input.owner, fixture.input.mutation.facts().destination, exact)
				if err != nil {
					t.Fatal(err)
				}
				value, err := ownership.PresentDocumentBinding(binding)
				if err != nil {
					t.Fatal(err)
				}
				intent, err := ownership.NewExactDocumentBindingIntent(fixture.input.owner, binding.Destination(), value, value)
				if err != nil {
					t.Fatal(err)
				}
				fixture.input.bindings = []ownership.DocumentBindingIntent{intent}
			}
			group, comparison, err := prepareWholeDocumentGroup(t.Context(), fixture.namespace, fixture.input)
			if err == nil || comparison.Validate() == nil || len(group.Aliases) != 0 || fixture.reader.imageReads != 0 {
				t.Fatalf("valid unrelated metadata escaped or read content: %v", err)
			}
			fixture.requireUnchangedHost(t)
		})
	}
}

func TestNativeWholeGroupRejectsStaleNamespaceAndLiveFacts(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*testing.T, *wholeGroupTestFixture)
	}{
		{"changed bytes", func(t *testing.T, f *wholeGroupTestFixture) {
			t.Helper()
			requireWholeGroupWrite(t, f.path, []byte("external"), 0o644)
		}},
		{"removed original", func(t *testing.T, f *wholeGroupTestFixture) {
			t.Helper()
			if err := os.Remove(f.path); err != nil {
				t.Fatal(err)
			}
		}},
		{"redirected parent", func(t *testing.T, f *wholeGroupTestFixture) {
			t.Helper()
			if err := os.Rename(filepath.Dir(f.path), filepath.Dir(f.path)+"-old"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Dir(f.path)+"-old", filepath.Dir(f.path)); err != nil {
				t.Fatal(err)
			}
		}},
		{"wrong payload", func(t *testing.T, f *wholeGroupTestFixture) {
			t.Helper()
			f.input.payloads = wholeGroupPayload(t, f.input.mutation.facts(), []byte("wrong"))
		}},
		{"small read limit", func(t *testing.T, f *wholeGroupTestFixture) { f.input.limits.MaximumBytes = 2 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeGlobal, "replace", []byte("before"), []byte("after"))
			test.edit(t, &fixture)
			group, comparison, err := prepareWholeDocumentGroup(t.Context(), fixture.namespace, fixture.input)
			if err == nil || comparison.Validate() == nil || len(group.Aliases) != 0 {
				t.Fatalf("stale capture returned usable facts: %v", err)
			}
		})
	}
}

func TestNativeWholeGroupCancellationAndFailedReadKeepNoFacts(t *testing.T) {
	fixture := wholeGroupFixture(t, target.TargetCodex, target.ScopeProject, "replace", []byte("before"), []byte("after"))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	group, comparison, err := prepareWholeDocumentGroup(ctx, fixture.namespace, fixture.input)
	if !errors.Is(err, context.Canceled) || comparison.Validate() == nil || len(group.Aliases) != 0 || fixture.reader.imageReads != 0 {
		t.Fatalf("pre-cancel returned facts or performed a read: %v", err)
	}
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	reader := &imageReader{RootedReader: journalTestFilesystem(), afterRead: cancel}
	fixture.namespace.filesystem = reader
	remaining := fixture.input.budget.RemainingBytes()
	group, comparison, err = prepareWholeDocumentGroup(ctx, fixture.namespace, fixture.input)
	if !errors.Is(err, context.Canceled) || comparison.Validate() == nil || len(group.Aliases) != 0 || reader.calls != 1 || fixture.input.budget.RemainingBytes() >= remaining {
		t.Fatalf("post-read cancellation returned facts or lost work charge: %v", err)
	}
	fixture.requireUnchangedHost(t)
}

type wholeGroupTestFixture struct {
	namespace          NativeDocumentNamespace
	input              wholeDocumentCaptureInput
	envelope           nativeRecordFixture
	reader             *nativeNamespaceReader
	path               string
	original, expected aggregate.Document
}

func wholeGroupFixture(t *testing.T, selected target.Target, scope target.Scope, action string, original, desired []byte) wholeGroupTestFixture {
	t.Helper()
	envelope := nativeJournalFixture(t)
	manifest := nativeNamespaceRoot(t, t.TempDir())
	host := nativeNamespaceRoot(t, filepath.Join(manifest.SelectedPath, "global"))
	roots := NativeDocumentRoots{Manifest: manifest}
	if selected == target.TargetPi {
		roots.PiAgent = host
	} else {
		roots.CodexHome = host
	}
	placement, err := profile.ResolveHostConfigPlacement(profile.HostConfigPlacementInput{Target: selected, Scope: scope, Path: "prompts/Caf\u00e9.md", Ownership: "document"})
	if err != nil {
		t.Fatal(err)
	}
	address, err := aggregate.NewDocumentAddress(selected, scope, placement.Destination())
	if err != nil {
		t.Fatal(err)
	}
	root := host
	if scope == target.ScopeProject {
		root = manifest
	}
	path := filepath.Join(root.SelectedPath, filepath.FromSlash(address.AggregateRoot().RelativePath()))
	if action != "create" {
		requireWholeGroupWrite(t, path, original, 0o644)
	}
	reader := &nativeNamespaceReader{RootedReader: journalTestFilesystem()}
	budget, err := physicalwork.NewBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := ObserveNativeDocumentNamespace(t.Context(), []aggregate.DocumentAddress{address}, roots, reader, budget)
	if err != nil {
		t.Fatal(err)
	}
	id, err := entity.New(entity.KindHostConfig, "document")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := aggregate.HostConfigDocumentSubject(id, selected, scope)
	if err != nil {
		t.Fatal(err)
	}
	before, err := durable.NewManagedPathState(subject, []target.Target{selected}, scope, placement.Destination(), artifact.HashFileContent(original), realization.PathProjectionFile, realization.PathPermissionsExact, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	var mutation ManagedPathMutation
	switch action {
	case "create":
		mutation, err = NewManagedPathCreateMutation(subject, []target.Target{selected}, scope, placement.Destination(), artifact.HashFileContent(desired), realization.PathProjectionFile, 0o600, nil)
	case "replace":
		mutation, err = NewManagedPathReplaceMutation(subject, []target.Target{selected}, scope, placement.Destination(), artifact.HashFileContent(desired), artifact.HashFileContent(original), realization.PathProjectionFile, 0o600, before)
	case "record":
		mutation, err = NewManagedPathRecordMutation(subject, []target.Target{selected}, scope, placement.Destination(), artifact.HashFileContent(original), artifact.HashFileContent(original), realization.PathProjectionFile, 0o644, &before)
	case "remove":
		mutation, err = NewManagedPathRemoveMutation(before, artifact.HashFileContent(original))
	default:
		t.Fatalf("unknown action %s", action)
	}
	if err != nil {
		t.Fatal(err)
	}
	input := wholeDocumentCaptureInput{mutation: mutation, nativeDocumentCaptureInput: nativeDocumentCaptureInput{owner: envelope.claims.owner, operationID: envelope.record.OperationID, groupIndex: 3, limits: envelope.read.limits, budget: budget}}
	input.payloads = wholeGroupPayload(t, mutation.facts(), desired)
	beforeImage, expectedImage := aggregate.ExistingDocument(original), aggregate.ExistingDocument(desired)
	var beforeRows, afterRows []durable.ManagedPathState
	if action != "create" {
		beforeRows = []durable.ManagedPathState{before}
	}
	switch action {
	case "create":
		beforeImage = aggregate.AbsentDocument()
	case "record":
		expectedImage = beforeImage
		input.payloads = payload.PayloadSet{}
	case "remove":
		expectedImage = aggregate.AbsentDocument()
		input.payloads = payload.PayloadSet{}
	}
	if action != "remove" {
		mode := os.FileMode(0o600)
		if action == "record" {
			mode = 0o644
		}
		after, err := durable.NewManagedPathState(subject, []target.Target{selected}, scope, placement.Destination(), artifact.HashFileContent(expectedImage.Content()), realization.PathProjectionFile, realization.PathPermissionsExact, mode)
		if err != nil {
			t.Fatal(err)
		}
		afterRows = []durable.ManagedPathState{after}
	}
	envelope.before, err = durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: beforeRows})
	if err != nil {
		t.Fatal(err)
	}
	envelope.after, err = durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: afterRows})
	if err != nil {
		t.Fatal(err)
	}

	beforeBinding, afterBinding := ownership.NoDocumentBinding(), ownership.NoDocumentBinding()
	var binding ownership.DocumentBindingIntent
	if exact, known := namespace.observation.Exact(); known {
		value, err := ownership.NewDocumentBinding(input.owner, placement.Destination(), exact)
		if err != nil {
			t.Fatal(err)
		}
		present, err := ownership.PresentDocumentBinding(value)
		if err != nil {
			t.Fatal(err)
		}
		if action != "create" {
			beforeBinding = present
		}
		if action != "remove" {
			afterBinding = present
		}
		binding, err = ownership.NewExactDocumentBindingIntent(input.owner, placement.Destination(), beforeBinding, afterBinding)
		if err != nil {
			t.Fatal(err)
		}
		if scope == target.ScopeGlobal {
			claim := documentActiveClaim(t, documentClaimFixture{owner: input.owner, exact: exact}, output.WholePathFootprint())
			var beforeClaims, afterClaims []ownership.Claim
			if action != "create" {
				beforeClaims = []ownership.Claim{claim}
			}
			if action != "remove" {
				afterClaims = []ownership.Claim{claim}
			}
			transition, err := ownershipmutation.NewDocumentClaimTransition(beforeClaims, afterClaims, input.operationID)
			if err != nil {
				t.Fatal(err)
			}
			input.claims = &transition
		}
	} else {
		pending, _ := namespace.observation.Provisional()
		binding, err = ownership.NewProvisionalDocumentBindingIntent(input.owner, placement.Destination(), pending)
		if err != nil {
			t.Fatal(err)
		}
		if scope == target.ScopeGlobal {
			intent, err := ownership.NewProvisionalAcquireIntent(placement.Destination(), output.WholePathFootprint(), pending, input.owner, input.operationID)
			if err != nil {
				t.Fatal(err)
			}
			input.provisional = []documentProvisionalAcquire{{alias: address, intent: intent}}
		}
	}
	input.bindings = []ownership.DocumentBindingIntent{binding}
	return wholeGroupTestFixture{namespace: namespace, input: input, envelope: envelope, reader: reader, path: path, original: beforeImage, expected: expectedImage}
}

func wholeGroupPayload(t *testing.T, facts managedPathMutationFacts, content []byte) payload.PayloadSet {
	t.Helper()
	value, err := payload.NewFilePayload(facts.subject, content, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	set, err := payload.NewPayloadSet([]payload.Payload{value}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return set
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

func (fixture wholeGroupTestFixture) requireUnchangedHost(t *testing.T) {
	t.Helper()
	content, err := os.ReadFile(fixture.path)
	if !fixture.original.Exists() {
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("capture created the host document: %v", err)
		}
		return
	}
	if err != nil || !bytes.Equal(content, fixture.original.Content()) {
		t.Fatalf("capture changed the original: %v", err)
	}
	info, err := os.Stat(fixture.path)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("capture changed original permission: %v", err)
	}
}

func (fixture wholeGroupTestFixture) stageAndReconstruct(t *testing.T, group recoveryDocumentGroup, comparison journaldocument.Comparison) {
	t.Helper()
	destination, capability := imageStageDestination(t)
	prepared, err := journalTestFilesystem().PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
		original, expected, err := stageDocumentImages(t.Context(), comparison, fixture.input.groupIndex, writer, fixture.input.budget)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(original, group.Original) || !reflect.DeepEqual(expected, group.Expected) {
			t.Fatal("capture and staging disagree on private image references")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Abort(context.Background())
	if err := prepared.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	root, err := rootedpath.CaptureRoot(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	fixture.envelope.record.DocumentGroups = []recoveryDocumentGroup{group}
	manifest, err := fixture.namespace.roots.Manifest.Root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := manifest.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	fixture.envelope.record.ManifestRootProvenance = persistedNativeRoot(provenance)
	content, err := marshalNativeJournalRecord(t.Context(), fixture.envelope.record, fixture.envelope.before, fixture.envelope.after, fixture.envelope.codec)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeNativeJournalRecord(t.Context(), content, fixture.envelope.codec)
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := decoded.prepareDocuments(t.Context(), []journaldocument.Comparison{comparison}, fixture.input.limits, fixture.envelope.read.codecs)
	if err != nil || len(admitted.groups) != 1 {
		t.Fatalf("prepared group does not pass full snapshot admission: %v", err)
	}
	fixture.envelope.read.artifacts, fixture.envelope.read.budget = root, imageBudget(t)
	loaded, err := decoded.loadDocuments(t.Context(), fixture.envelope.read)
	if err != nil || len(loaded.groups) != 1 {
		t.Fatalf("actual group cannot reconstruct: %v", err)
	}
	var expectedMode *recovery.PermissionMode
	if mode, present := comparison.ExpectedState().RegularFileMode(); present {
		expectedMode = &mode
	}
	restored, err := loaded.groups[0].Comparison().RestoreBefore(t.Context(), fixture.expected, expectedMode)
	if err != nil || !restored.Document().Equal(fixture.original) {
		t.Fatalf("actual artifact-backed recovery lost original bytes/presence: %v", err)
	}
}
