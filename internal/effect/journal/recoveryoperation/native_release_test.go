package recoveryoperation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/fileset"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"
	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/testkit/metadatatx"
	"github.com/isty2e/daem/test/testkit/stateimage"
)

type nativeReleaseFixture struct {
	nativeCaptureTestInput
	captured journalpublication.CapturedNativeRelease
	local    NativeReleasePaths
	load     PlanLoadOptions
	writes   []metadatatx.Write
}

func prepareNativeRelease(t *testing.T) nativeReleaseFixture {
	t.Helper()
	input := nativeCaptureInput(t, false, target.ScopeProject)
	before := input.batch.Evidence().BeforeState()
	id, _, present := topologyprojection.HostConfigKeyIdentity(before.ManagedAggregates()[0].Subject())
	if !present {
		t.Fatal("fixture has no host-config membership")
	}
	managements, err := journaldocument.HostConfigManagements(t.Context(), before, id)
	if err != nil || len(managements) != 1 {
		t.Fatal(managements, err)
	}
	after, err := managements[0].RemoveFrom(t.Context(), before)
	if err != nil {
		t.Fatal(err)
	}
	store, err := ownershipstore.New(input.registry)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := store.LoadProtectionRegistry(t.Context())
	if err != nil || len(registry.Claims()) != 1 {
		t.Fatal(err)
	}
	owner := registry.Claims()[0].Owner()
	binding, present := registry.DocumentBinding(owner.Key(), managements[0].Destination())
	if !present {
		t.Fatal("fixture has no document binding")
	}
	bindingValue, err := ownership.PresentDocumentBinding(binding)
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := journalrecord.NewNativeReleaseExactNamespace(binding.PathAuthority())
	if err != nil {
		t.Fatal(err)
	}
	local := NativeReleasePaths{Manifest: owner.ManifestPath(), DefaultLock: filepath.Join(input.paths.ManifestRoot, "daem.lock.json"), State: input.paths.StatefilePath}
	requireWholeGroupWrite(t, local.DefaultLock, []byte("lock-before"), 0o600)
	stateAfter, err := input.options.StateCodec.Encode(after)
	if err != nil {
		t.Fatal(err)
	}
	value := journalrecord.NativeReleaseInput{
		OperationID: "native-record-apply", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Owner: owner,
		Before: before, Management: managements[0], Namespace: namespace, OwnClaimsBefore: registry.Claims(), BindingBefore: bindingValue,
	}
	var writes []metadatatx.Write
	for index, path := range []string{local.Manifest, local.DefaultLock, local.State} {
		roles := []journalrecord.NativeReleaseFileRole{journalrecord.NativeReleaseManifest, journalrecord.NativeReleaseDefaultLock, journalrecord.NativeReleaseState}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		observation, err := mutation.ObserveDirectoryEntryAuthorityBounded(path, physicalwork.MaximumPhysicalPathDepth, input.budget)
		if err != nil {
			t.Fatal(err)
		}
		exact, present := observation.Exact()
		if !present {
			t.Fatal("fixture control is not exact")
		}
		reference, err := journalrecord.NewNativeReleaseExactNamespace(exact)
		if err != nil {
			t.Fatal(err)
		}
		next := []byte("version=1\n")
		if index == 1 {
			next = []byte("lock-after")
		}
		if index == 2 {
			next = stateAfter
		}
		file, err := journalrecord.NewNativeReleaseFileIntent(journalrecord.NativeReleaseFileInput{
			Role: roles[index], Target: reference, BeforeExists: true, BeforeSHA256: journalrecord.Fingerprint(content), BeforeMode: 0o600, AfterSHA256: journalrecord.Fingerprint(next), Write: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		value.Files = append(value.Files, file)
		writes = append(writes, metadatatx.Write{Path: path, Before: &metadatatx.Image{Content: content, Mode: 0o600}, After: next, CommitPoint: index == 2})
	}
	release, err := journalrecord.NewNativeRelease(t.Context(), value, input.options.StateCodec)
	if err != nil {
		t.Fatal(err)
	}
	captured, err := journalpublication.CaptureNativeRelease(t.Context(), input.paths, release, journalpublication.NativeReleasePublicationOptions{
		OperationAuthority: input.options.OperationAuthority, Filesystem: input.options.Filesystem, StateCodec: input.options.StateCodec, Budget: input.budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	load := PlanLoadOptions{
		Filesystem: input.options.Filesystem, StateCodec: statefile.Codec{}, PhysicalPathBudget: input.budget, NativeReleasePaths: &local,
		NativeDocuments: &Profile{StateCodec: input.options.StateCodec, RawRegistry: store.LoadProtectionRegistry, Budget: input.budget},
	}
	return nativeReleaseFixture{nativeCaptureTestInput: input, captured: captured, local: local, load: load, writes: writes}
}

func TestNativeReleasePlanningDoesNotInterpretLiveState(t *testing.T) {
	fixture := prepareNativeRelease(t)
	metadatatx.WritePrefix(t, filepath.Dir(fixture.local.State), fixture.writes, 1)
	reads := 0
	fixture.load.NativeDocuments.StateReader = func(context.Context) (durable.Snapshot, error) {
		reads++
		return durable.Snapshot{}, errors.New("live state is unavailable until FileSet settlement")
	}
	selection, err := LoadRecoverablePlanWithOptions(t.Context(), fixture.paths, fixture.load)
	if err != nil {
		t.Fatal(err)
	}
	plan, present := NativeReleaseActiveRecoveryPlan(selection)
	if !present || reads != 0 || plan.FileSetDisposition() != fileset.RecoveryRollback || !plan.SameExecutionAuthority(plan.Clone()) {
		t.Fatal("release planning interpreted state or lost its exact intent", reads, plan.FileSetDisposition())
	}
	wrong := fixture.local
	wrong.Manifest = filepath.Join(fixture.paths.ManifestRoot, "other-manifest.toml")
	fixture.load.NativeReleasePaths = &wrong
	if changed, err := LoadRecoverablePlanWithOptions(t.Context(), fixture.paths, fixture.load); err == nil || changed != nil || reads != 0 {
		t.Fatal("another manifest sharing the state root acquired recovery authority", err, reads)
	}
	fixture.requireUnchanged(t)
}

func TestNativeReleaseAssessmentRequiresClearFilesAndExactBindings(t *testing.T) {
	for _, phase := range []string{"before", "after", "pending", "wrong-binding", "third-local"} {
		t.Run(phase, func(t *testing.T) {
			fixture := prepareNativeRelease(t)
			if phase == "pending" {
				metadatatx.WritePrefix(t, filepath.Dir(fixture.local.State), fixture.writes, 3)
			}
			if phase == "after" {
				for _, write := range fixture.writes {
					requireWholeGroupWrite(t, write.Path, write.After, write.Before.Mode)
				}
			}
			if phase == "third-local" {
				requireWholeGroupWrite(t, fixture.local.Manifest, []byte("editor change"), 0o600)
			}
			var authorities []*rootedpath.EntryAuthority
			for index, path := range []string{fixture.local.Manifest, fixture.local.DefaultLock, fixture.local.State} {
				if phase == "wrong-binding" && index == 0 {
					path = filepath.Join(fixture.paths.ManifestRoot, "same-bytes-other-manifest.toml")
					requireWholeGroupWrite(t, path, fixture.writes[0].Before.Content, 0o600)
				}
				entry, err := rootedpath.BindCanonicalSelectedEntryAuthorityBounded(fixture.options.ManifestRoot, fixture.paths.ManifestRoot, path, physicalwork.MaximumPhysicalPathDepth, fixture.budget)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = entry.Close() })
				authorities = append(authorities, entry)
			}
			reads := 0
			assessment, err := journalqualification.AssessNativeRelease(t.Context(), fixture.captured.Source(), journalqualification.NativeReleaseReadOptions{
				Paths: fixture.local, OperationAuthority: fixture.options.OperationAuthority, LocalAuthorities: authorities, Filesystem: fixture.options.Filesystem,
				Budget: fixture.budget, Registry: fixture.load.NativeDocuments.RawRegistry,
				State: func(ctx context.Context) (durable.Snapshot, error) {
					reads++
					return statefile.LoadOptionalWithOptions(ctx, fixture.local.State, statefile.ReadOptions{Codec: stateimage.Physical(t, fixture.options.StateCodec), PhysicalPathBudget: fixture.budget})
				},
			})
			switch phase {
			case "before":
				if err != nil || !assessment.CleanBefore() || assessment.CleanAfter() {
					t.Fatal("complete Before was not admitted", err)
				}
			case "after":
				if err != nil || !assessment.NeedsOwnRelease() || assessment.CleanAfter() {
					t.Fatal("After silently omitted own release", err)
				}
			default:
				if err == nil || assessment.CleanBefore() || assessment.CleanAfter() || assessment.NeedsOwnRelease() {
					t.Fatal("incomplete evidence produced a usable phase", err)
				}
				if phase == "pending" && reads != 0 {
					t.Fatal("pending FileSet leaked provisional state to phase selection")
				}
			}
			if current, err := os.ReadFile(fixture.host); err != nil || !bytes.Equal(current, fixture.original.Content()) {
				t.Fatal("metadata assessment changed host bytes", err)
			}
		})
	}
}

func TestJournalReadinessClassifiesNativePurposesAsInterrupted(t *testing.T) {
	for _, purpose := range []string{"host-mutation", "release-only"} {
		t.Run(purpose, func(t *testing.T) {
			var root string
			if purpose == "release-only" {
				root = prepareNativeRelease(t).paths.RecoveryDir
			} else {
				_, root, _ = nativeInventoryFixture(t)
			}
			if err := RequireNoInterruptedApply(t.Context(), root); !errors.Is(err, ErrInterruptedApply) {
				t.Fatal("native interruption lost its recoverable classification", err)
			}
		})
	}
}

func TestNativeReleaseRetainsItsInterruptionWhenLocalFileSetCannotBeSettled(t *testing.T) {
	for _, kind := range []fileset.FileSetFenceKind{fileset.FileSetFenceAbandonedResidue, fileset.FileSetFenceCensusLimit} {
		t.Run(string(kind), func(t *testing.T) {
			fixture := prepareNativeRelease(t)
			root := filepath.Dir(fixture.local.State)
			if kind == fileset.FileSetFenceAbandonedResidue {
				if err := os.Mkdir(filepath.Join(root, ".daem-tmp-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), 0o700); err != nil {
					t.Fatal(err)
				}
			} else {
				for index := range 4097 {
					if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("ordinary-%05d", index)), nil, 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			selection, err := LoadRecoverablePlanWithOptions(t.Context(), fixture.paths, fixture.load)
			if selection != nil || !errors.Is(err, ErrInterruptedApply) || fileset.FileSetFenceKindOf(err) != kind {
				t.Fatal("release lost its active authority or admitted an unsettled local fence", err)
			}
			if _, err := os.Stat(fixture.captured.CaptureResult().JournalPath); err != nil {
				t.Fatal("refusal discarded active recovery evidence", err)
			}
		})
	}
}
