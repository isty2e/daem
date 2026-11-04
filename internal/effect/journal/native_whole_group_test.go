package journal

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/topology"

	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

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
	expectedMode, present := comparison.ExpectedState().RegularFileMode()
	if !comparison.BeforeDocument().Exists() || comparison.BeforeDocument().ContentSize() != 0 || !comparison.ExpectedDocument().Exists() || comparison.ExpectedDocument().ContentSize() != 0 || comparison.BeforeMode() == nil || *comparison.BeforeMode() != 0o644 || !present || expectedMode != 0o600 {
		t.Fatal("empty file or actual host permission was collapsed into absence/private witness permission")
	}
	fixture.stageAndReconstruct(t, group, comparison)
}

func TestNativeWholeGroupRetainsActualAliasesAndWriter(t *testing.T) {
	fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeGlobal, "replace", []byte("before"), []byte("after"))
	roots := fixture.roots
	if err := os.Symlink(roots.PiAgent.SelectedPath, filepath.Join(roots.Manifest.SelectedPath, ".pi")); err != nil {
		t.Fatal(err)
	}
	project := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/prompts/Caf\u00e9.md")
	aliases := append(fixture.namespace.Addresses(), project)
	namespace, err := journalnamespace.Observe(t.Context(), aliases, roots, fixture.reader, fixture.input.Budget())
	if err != nil {
		t.Fatal(err)
	}
	group, comparison, err := prepareWholeDocumentGroup(t.Context(), namespace, fixture.input)
	if err != nil || !comparison.BeforeDocument().Equal(fixture.original) {
		t.Fatalf("actual alias grouping failed: %v", err)
	}
	journal := fixture.recordForGroups(t, []journalrecord.SealedDocumentGroup{group})
	recorded, err := journal.DocumentAt(0)
	if err != nil || len(recorded.Aliases()) != 2 {
		t.Fatal("actual alias grouping lost a recorded alias", err)
	}
	authority, err := roots.PiAgent.Root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := authority.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	writer, relative, err := recorded.Writer()
	if err != nil || writer != provenance || relative.Path() != "prompts/Caf\u00e9.md" {
		t.Fatal("logical project alias replaced the independently selected physical writer", err)
	}
	if err := recorded.RequireCapturedNamespace(namespace.Observation()); err != nil {
		t.Fatal(err)
	}
	for _, alias := range recorded.Aliases() {
		if err := alias.RequireObservations(namespace.Observation()); err != nil {
			t.Fatal("alias does not retain the actual common referent", err)
		}
	}
	fixture.requireUnchangedHost(t)
}

func TestNativeWholeGroupRejectsProvisionalClaimMisjoinsBeforeRead(t *testing.T) {
	for _, mismatch := range []string{"owner", "operation", "alias", "exact claims"} {
		t.Run(mismatch, func(t *testing.T) {
			fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeGlobal, "create", nil, []byte("after"))
			if _, pending := fixture.namespace.Observation().Provisional(); !pending {
				t.Skip("filesystem supplies exact authority for this absent normalization-sensitive path")
			}
			switch mismatch {
			case "owner":
				fixture.input.CaptureInput = reviseCaptureInput(fixture.input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) { spec.Owner = documentClaimsForTest(t).owner })
			case "operation":
				fixture.input.CaptureInput = reviseCaptureInput(fixture.input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) { spec.OperationID = "another-operation" })
			case "alias":
				fixture.input.CaptureInput = reviseCaptureInput(fixture.input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) {
					spec.Provisional[0] = journaldocument.NewProvisionalAcquire(nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/other.md"), spec.Provisional[0].Intent())
				})
			case "exact claims":
				other := wholeGroupFixture(t, target.TargetPi, target.ScopeGlobal, "replace", []byte("before"), []byte("after"))
				fixture.input.CaptureInput = reviseCaptureInput(fixture.input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) {
					spec.Claims, spec.Provisional = other.input.Claims(), nil
				})
			}
			group, comparison, err := prepareWholeDocumentGroup(t.Context(), fixture.namespace, fixture.input)
			if err == nil || !reflect.ValueOf(group).IsZero() || comparison.Validate() == nil || fixture.reader.imageReads != 0 {
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
			if err == nil || !reflect.ValueOf(group).IsZero() || comparison.Validate() == nil {
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
		{"zero mutation", func(f *wholeGroupTestFixture) { f.input.mutation = journalrecord.ManagedPathMutation{} }},
		{"wrong alias", func(f *wholeGroupTestFixture) {
			other := wholeGroupFixture(t, target.TargetCodex, target.ScopeGlobal, "replace", []byte("other"), []byte("next"))
			f.input.mutation = other.input.mutation
		}},
		{"missing manifest grant", func(f *wholeGroupTestFixture) {
			f.roots.Manifest = journalnamespace.RootSelection{}
			observed, err := journalnamespace.Observe(t.Context(), f.namespace.Addresses(), f.roots, f.reader, f.input.Budget())
			if err != nil {
				t.Fatal(err)
			}
			f.namespace = observed
		}},
		{"wrong claim owner", func(f *wholeGroupTestFixture) {
			f.input.CaptureInput = reviseCaptureInput(f.input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) { spec.Owner = documentClaimsForTest(t).owner })
		}},
		{"wrong operation reservation", func(f *wholeGroupTestFixture) {
			transition, err := ownershipmutation.NewDocumentClaimTransition(nil, f.input.Claims().After(), f.input.OperationID())
			if err != nil {
				t.Fatal(err)
			}
			f.input.CaptureInput = reviseCaptureInput(f.input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) { spec.Claims = &transition })
			f.input.CaptureInput = reviseCaptureInput(f.input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) { spec.OperationID = "another-operation" })
		}},
		{"mixed claims", func(f *wholeGroupTestFixture) {
			f.input.CaptureInput = reviseCaptureInput(f.input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) {
				spec.Provisional = []journaldocument.ProvisionalAcquire{{}}
			})
		}},
		{"wrong binding", func(f *wholeGroupTestFixture) {
			f.input.CaptureInput = reviseCaptureInput(f.input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) { spec.Bindings = []ownership.DocumentBindingIntent{{}} })
		}},
		{"negative ordinal", func(f *wholeGroupTestFixture) {
			f.input.CaptureInput = reviseCaptureInput(f.input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) { spec.GroupIndex = -1 })
		}},
		{"missing budget", func(f *wholeGroupTestFixture) {
			f.input.CaptureInput = reviseCaptureInput(f.input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) { spec.Budget = nil })
		}},
		{"invalid limits", func(f *wholeGroupTestFixture) {
			f.input.CaptureInput = reviseCaptureInput(f.input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) { spec.Limits.MaximumBytes = 0 })
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeGlobal, "replace", []byte("before"), []byte("after"))
			test.edit(&fixture)
			group, comparison, err := prepareWholeDocumentGroup(t.Context(), fixture.namespace, fixture.input)
			if err == nil || comparison.Validate() == nil || !reflect.ValueOf(group).IsZero() || fixture.reader.imageReads != 0 {
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
			exact, known := other.namespace.Observation().Exact()
			if !known {
				t.Fatal("existing comparison namespace has no exact authority")
			}
			switch mismatch {
			case "claim namespace":
				claim := documentActiveClaim(t, documentClaimFixture{owner: fixture.input.Owner(), exact: exact}, output.WholePathFootprint())
				transition, err := ownershipmutation.NewDocumentClaimTransition([]ownership.Claim{claim}, []ownership.Claim{claim}, fixture.input.OperationID())
				if err != nil {
					t.Fatal(err)
				}
				fixture.input.CaptureInput = reviseCaptureInput(fixture.input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) { spec.Claims = &transition })
			case "binding namespace":
				binding, err := ownership.NewDocumentBinding(fixture.input.Owner(), fixture.input.mutation.Destination(), exact)
				if err != nil {
					t.Fatal(err)
				}
				value, err := ownership.PresentDocumentBinding(binding)
				if err != nil {
					t.Fatal(err)
				}
				intent, err := ownership.NewExactDocumentBindingIntent(fixture.input.Owner(), binding.Destination(), value, value)
				if err != nil {
					t.Fatal(err)
				}
				fixture.input.CaptureInput = reviseCaptureInput(fixture.input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) {
					spec.Bindings = []ownership.DocumentBindingIntent{intent}
				})
			}
			group, comparison, err := prepareWholeDocumentGroup(t.Context(), fixture.namespace, fixture.input)
			if err == nil || comparison.Validate() == nil || !reflect.ValueOf(group).IsZero() || fixture.reader.imageReads != 0 {
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
			f.input.payloads = wholeGroupPayload(t, f.input.mutation.Subject(), []byte("wrong"))
		}},
		{"small read limit", func(t *testing.T, f *wholeGroupTestFixture) {
			f.input.CaptureInput = reviseCaptureInput(f.input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) { spec.Limits.MaximumBytes = 2 })
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeGlobal, "replace", []byte("before"), []byte("after"))
			test.edit(t, &fixture)
			group, comparison, err := prepareWholeDocumentGroup(t.Context(), fixture.namespace, fixture.input)
			if err == nil || comparison.Validate() == nil || !reflect.ValueOf(group).IsZero() {
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
	if !errors.Is(err, context.Canceled) || comparison.Validate() == nil || !reflect.ValueOf(group).IsZero() || fixture.reader.imageReads != 0 {
		t.Fatalf("pre-cancel returned facts or performed a read: %v", err)
	}
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	reader := &imageReader{RootedReader: journalTestFilesystem(), afterRead: cancel}
	fixture.namespace, err = journalnamespace.Observe(t.Context(), fixture.namespace.Addresses(), fixture.roots, reader, fixture.input.Budget())
	if err != nil {
		t.Fatal(err)
	}
	remaining := fixture.input.Budget().RemainingBytes()
	group, comparison, err = prepareWholeDocumentGroup(ctx, fixture.namespace, fixture.input)
	if !errors.Is(err, context.Canceled) || comparison.Validate() == nil || !reflect.ValueOf(group).IsZero() || reader.calls != 1 || fixture.input.Budget().RemainingBytes() >= remaining {
		t.Fatalf("post-read cancellation returned facts or lost work charge: %v", err)
	}
	fixture.requireUnchangedHost(t)
}

type wholeGroupTestFixture struct {
	roots              journalnamespace.Roots
	namespace          journalnamespace.DocumentNamespace
	input              wholeDocumentCaptureInput
	envelope           nativeRecordFixture
	reader             *nativeNamespaceReader
	path               string
	original, expected aggregate.Document
}

func (fixture wholeGroupTestFixture) recordForGroups(t *testing.T, groups []journalrecord.SealedDocumentGroup) journalrecord.Native {
	t.Helper()
	manifest, err := fixture.roots.Manifest.Root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := manifest.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	journal, err := journalrecord.NewNativeCapture(fixture.input.OperationID(), time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), fixture.input.Owner(), provenance, fixture.envelope.before, fixture.envelope.after, groups)
	if err != nil {
		t.Fatal(err)
	}
	return journal
}

func wholeGroupFixture(t *testing.T, selected target.Target, scope target.Scope, action string, original, desired []byte) wholeGroupTestFixture {
	t.Helper()
	envelope := nativeJournalFixture(t)
	manifest := nativeNamespaceRoot(t, t.TempDir())
	host := nativeNamespaceRoot(t, filepath.Join(manifest.SelectedPath, "global"))
	roots := journalnamespace.Roots{Manifest: manifest}
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
	namespace, err := journalnamespace.Observe(t.Context(), []aggregate.DocumentAddress{address}, roots, reader, budget)
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
	var mutation journalrecord.ManagedPathMutation
	switch action {
	case "create":
		mutation, err = journalrecord.NewManagedPathCreateMutation(subject, []target.Target{selected}, scope, placement.Destination(), artifact.HashFileContent(desired), realization.PathProjectionFile, 0o600, nil)
	case "replace":
		mutation, err = journalrecord.NewManagedPathReplaceMutation(subject, []target.Target{selected}, scope, placement.Destination(), artifact.HashFileContent(desired), artifact.HashFileContent(original), realization.PathProjectionFile, 0o600, before)
	case "record":
		mutation, err = journalrecord.NewManagedPathRecordMutation(subject, []target.Target{selected}, scope, placement.Destination(), artifact.HashFileContent(original), artifact.HashFileContent(original), realization.PathProjectionFile, 0o644, &before)
	case "remove":
		mutation, err = journalrecord.NewManagedPathRemoveMutation(before, artifact.HashFileContent(original))
	default:
		t.Fatalf("unknown action %s", action)
	}
	if err != nil {
		t.Fatal(err)
	}
	input := wholeDocumentCaptureInput{mutation: mutation, CaptureInput: journalobservation.NewCaptureInput(journalobservation.CaptureInputSpec{Owner: envelope.claims.owner, OperationID: envelope.journal.OperationID(), GroupIndex: 3, Limits: envelope.read.limits, Budget: budget})}
	input.payloads = wholeGroupPayload(t, mutation.Subject(), desired)
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
	if exact, known := namespace.Observation().Exact(); known {
		value, err := ownership.NewDocumentBinding(input.Owner(), placement.Destination(), exact)
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
		binding, err = ownership.NewExactDocumentBindingIntent(input.Owner(), placement.Destination(), beforeBinding, afterBinding)
		if err != nil {
			t.Fatal(err)
		}
		if scope == target.ScopeGlobal {
			claim := documentActiveClaim(t, documentClaimFixture{owner: input.Owner(), exact: exact}, output.WholePathFootprint())
			var beforeClaims, afterClaims []ownership.Claim
			if action != "create" {
				beforeClaims = []ownership.Claim{claim}
			}
			if action != "remove" {
				afterClaims = []ownership.Claim{claim}
			}
			transition, err := ownershipmutation.NewDocumentClaimTransition(beforeClaims, afterClaims, input.OperationID())
			if err != nil {
				t.Fatal(err)
			}
			input.CaptureInput = reviseCaptureInput(input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) { spec.Claims = &transition })
		}
	} else {
		pending, _ := namespace.Observation().Provisional()
		binding, err = ownership.NewProvisionalDocumentBindingIntent(input.Owner(), placement.Destination(), pending)
		if err != nil {
			t.Fatal(err)
		}
		if scope == target.ScopeGlobal {
			intent, err := ownership.NewProvisionalAcquireIntent(placement.Destination(), output.WholePathFootprint(), pending, input.Owner(), input.OperationID())
			if err != nil {
				t.Fatal(err)
			}
			input.CaptureInput = reviseCaptureInput(input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) {
				spec.Provisional = []journaldocument.ProvisionalAcquire{journaldocument.NewProvisionalAcquire(address, intent)}
			})
		}
	}
	input.CaptureInput = reviseCaptureInput(input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) {
		spec.Bindings = []ownership.DocumentBindingIntent{binding}
	})
	return wholeGroupTestFixture{roots: roots, namespace: namespace, input: input, envelope: envelope, reader: reader, path: path, original: beforeImage, expected: expectedImage}
}

func wholeGroupPayload(t *testing.T, subject topology.SubjectID, content []byte) payload.PayloadSet {
	t.Helper()
	value, err := payload.NewFilePayload(subject, content, 0o600)
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

func (fixture wholeGroupTestFixture) stageAndReconstruct(t *testing.T, group journalrecord.SealedDocumentGroup, comparison journaldocument.Comparison) {
	t.Helper()
	journal := fixture.recordForGroups(t, []journalrecord.SealedDocumentGroup{group})
	session, err := journal.BeginLoadedDocuments(t.Context(), fixture.input.Limits(), fixture.envelope.read.codecs)
	if err != nil {
		t.Fatal(err)
	}
	demand, present, err := session.Next(t.Context())
	if err != nil || !present {
		t.Fatal("captured group has no recorded image demand", err)
	}
	content, err := journal.Encode(t.Context(), fixture.envelope.codec)
	if err != nil {
		t.Fatal(err)
	}
	destination, capability := imageStageDestination(t)
	defer capability.Close()
	prepared, err := journalTestFilesystem().PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
		original, expected, err := stageDocumentImages(t.Context(), comparison, fixture.input.GroupIndex(), writer, fixture.input.Budget())
		if err != nil {
			return err
		}
		if original != demand.Original() || expected != demand.Expected() {
			t.Fatal("capture and staging disagree on private image references")
		}
		path, err := mutationfs.NewTreeRelativePath(journalrecord.FileName)
		if err != nil {
			return err
		}
		return writer.WriteFile(path, journalrecord.Mode, bytes.NewReader(content))
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
	snapshot, err := readNativeCaptureRecord(t.Context(), root, journalTestFilesystem(), imageBudget(t))
	if err != nil {
		t.Fatal(err)
	}
	loadedRecord, err := journalrecord.DecodeNativeSnapshot(t.Context(), snapshot, journalrecord.FileName, fixture.envelope.codec)
	if err != nil {
		t.Fatal(err)
	}
	decoded, present := loadedRecord.Native()
	if !present {
		t.Fatal("whole capture decoded as another record family")
	}
	admitted, err := decoded.PrepareDocuments(t.Context(), []journaldocument.Comparison{comparison}, fixture.input.Limits(), fixture.envelope.read.codecs)
	if err != nil {
		t.Fatalf("prepared group does not pass full snapshot admission: %v", err)
	}
	comparisons, err := admitted.Comparisons()
	if err != nil || len(comparisons) != 1 {
		t.Fatal("prepared group lost its completed comparison", err)
	}
	fixture.envelope.read.artifacts, fixture.envelope.read.budget = root, imageBudget(t)
	loaded, err := loadDocuments(decoded, t.Context(), fixture.envelope.read)
	if err != nil || loaded.Len() != 1 {
		t.Fatalf("actual group cannot reconstruct: %v", err)
	}
	var expectedMode *recovery.PermissionMode
	if mode, present := comparison.ExpectedState().RegularFileMode(); present {
		expectedMode = &mode
	}
	restored, err := requireLoadedNativeDocument(t, loaded, 0).Group().Comparison().RestoreBefore(t.Context(), fixture.expected, expectedMode)
	if err != nil || !restored.Document().Equal(fixture.original) {
		t.Fatalf("actual artifact-backed recovery lost original bytes/presence: %v", err)
	}
}
