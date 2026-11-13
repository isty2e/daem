package publication

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func nativeReleasePublicationInput(t *testing.T) (nativeCaptureTestInput, journalrecord.NativeRelease, NativeReleasePublicationOptions) {
	t.Helper()
	input := nativeCaptureInput(t, false, target.ScopeProject)
	before := input.batch.Evidence().BeforeState()
	id, _, present := topologyprojection.HostConfigKeyIdentity(before.ManagedAggregates()[0].Subject())
	if !present {
		t.Fatal("fixture has no retained native identity")
	}
	identities, err := journaldocument.HostConfigManagements(t.Context(), before, id)
	if err != nil || len(identities) != 1 {
		t.Fatal(identities, err)
	}
	after, err := identities[0].RemoveFrom(t.Context(), before)
	if err != nil {
		t.Fatal(err)
	}
	store, err := ownershipstore.New(input.registry)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := store.LoadProtectionRegistry(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	owner := input.batch.owner
	binding, present := registry.DocumentBinding(owner.Key(), identities[0].Destination())
	if !present {
		t.Fatal("fixture has no own native binding")
	}
	bindingValue, err := ownership.PresentDocumentBinding(binding)
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := journalrecord.NewNativeReleaseExactNamespace(binding.PathAuthority())
	if err != nil {
		t.Fatal(err)
	}
	releaseInput := journalrecord.NativeReleaseInput{
		OperationID: "native-record-apply", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Owner: owner, Before: before, Management: identities[0], Namespace: namespace,
		OwnClaimsBefore: registry.Claims(), BindingBefore: bindingValue,
	}
	for _, role := range []journalrecord.NativeReleaseFileRole{journalrecord.NativeReleaseManifest, journalrecord.NativeReleaseDefaultLock, journalrecord.NativeReleaseState} {
		path := owner.ManifestPath()
		if role == journalrecord.NativeReleaseDefaultLock {
			path = filepath.Join(input.paths.ManifestRoot, "daem.lock.json")
		}
		if role == journalrecord.NativeReleaseState {
			path = input.paths.StatefilePath
		}
		observed, err := mutation.ObserveDirectoryEntryAuthorityBounded(path, physicalwork.MaximumPhysicalPathDepth, input.batch.budget)
		if err != nil {
			t.Fatal(err)
		}
		exact, present := observed.Exact()
		if !present {
			t.Fatal("fixture control path is not exact")
		}
		reference, err := journalrecord.NewNativeReleaseExactNamespace(exact)
		if err != nil {
			t.Fatal(err)
		}
		file := journalrecord.NativeReleaseFileInput{Role: role, Target: reference}
		content, err := os.ReadFile(path)
		if err == nil {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			file.BeforeExists, file.BeforeSHA256, file.BeforeMode = true, journalrecord.Fingerprint(content), info.Mode().Perm()
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if role == journalrecord.NativeReleaseState {
			content, err := input.options.StateCodec.Encode(after)
			if err != nil {
				t.Fatal(err)
			}
			file.Write, file.AfterSHA256 = true, journalrecord.Fingerprint(content)
		}
		intent, err := journalrecord.NewNativeReleaseFileIntent(file)
		if err != nil {
			t.Fatal(err)
		}
		releaseInput.Files = append(releaseInput.Files, intent)
	}
	release, err := journalrecord.NewNativeRelease(t.Context(), releaseInput, input.options.StateCodec)
	if err != nil {
		t.Fatal(err)
	}
	return input, release, NativeReleasePublicationOptions{
		OperationAuthority: input.options.OperationAuthority,
		Filesystem:         input.options.Filesystem, StateCodec: input.options.StateCodec, Budget: input.batch.budget,
	}
}

func TestNativeReleasePublicationWritesOnlyAQualifiedJournal(t *testing.T) {
	input, release, options := nativeReleasePublicationInput(t)
	captured, err := CaptureNativeRelease(t.Context(), input.paths, release, options)
	if err != nil {
		t.Fatal(err)
	}
	if err := captured.RequireAllocation(options.Budget); err != nil {
		t.Fatal(err)
	}
	if !captured.Release().Before().Equal(release.Before()) || !captured.Release().After().Equal(release.After()) {
		t.Fatal("publication changed complete state facts")
	}
	for path, want := range map[string][]byte{input.host: input.original.Content(), input.paths.StatefilePath: input.stateBytes, input.registry: input.registryBytes} {
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, want) {
			t.Fatal("journal publication changed a host/state/Registry file", err)
		}
	}
	entries, err := os.ReadDir(captured.CaptureResult().Directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != journalrecord.FileName {
		t.Fatal("metadata publication acquired host images or another artifact", err)
	}
	if next, err := CaptureNativeRelease(t.Context(), input.paths, release, options); !errors.Is(err, journalinventory.ErrInterruptedApply) || next.Validate() == nil {
		t.Fatal("second publication bypassed its active release fence", err)
	}
}

func TestNativeReleasePublicationRefusesAnUnownedExtraArtifact(t *testing.T) {
	input, release, options := nativeReleasePublicationInput(t)
	captured, err := CaptureNativeRelease(t.Context(), input.paths, release, options)
	if err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(captured.CaptureResult().Directory, "foreign.json")
	if err := os.WriteFile(extra, []byte("do not collect"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateNativeReleaseDirectory(t.Context(), options.OperationAuthority, captured.Source().PhysicalAuthority()); err == nil {
		t.Fatal("release evidence accepted an unowned extra artifact")
	}
	if content, err := os.ReadFile(extra); err != nil || string(content) != "do not collect" {
		t.Fatal("qualification failure removed an unowned artifact", err)
	}
}

func TestNativeReleasePublicationPreservesOrdinaryInterruptionClassification(t *testing.T) {
	for _, cleanup := range []bool{false, true} {
		input, release, options := nativeReleasePublicationInput(t)
		identity, ordinary := captureInventoryJournal(t, input.paths.RecoveryDir, "ordinary-before-release")
		want := journalinventory.ErrInterruptedApply
		if cleanup {
			record, err := retirement.NewRecord(identity.OperationID(), identity.JournalAuthorityFingerprint(), retirement.PhasePrepared)
			if err != nil {
				t.Fatal(err)
			}
			content, err := retirement.Encode(record)
			if err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(input.paths.RecoveryDir, identity.ControlName())
			if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, retirement.RecordFileName), content, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(ordinary.Directory, filepath.Join(input.paths.RecoveryDir, identity.ResidueName())); err != nil {
				t.Fatal(err)
			}
			want = journalinventory.ErrIncompleteJournalCleanup
		}
		captured, err := CaptureNativeRelease(t.Context(), input.paths, release, options)
		if !errors.Is(err, want) || captured.Validate() == nil {
			t.Fatal("ordinary recovery authority lost its canonical classification", cleanup, err)
		}
	}
}
