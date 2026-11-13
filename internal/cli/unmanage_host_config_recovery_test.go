package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/statefile"
	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/fileset"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
	workflowrecover "github.com/isty2e/daem/internal/workflow/recover"
)

func TestCLIRecoversMetadataOnlyHostConfigReleaseAndPreservesFenceProjection(t *testing.T) {
	paths, _, host := prepareHostConfigUnmanageCLI(t)
	for _, args := range [][]string{{"lock", "--manifest", paths.ManifestPath}, {"apply", "--manifest", paths.ManifestPath, "--yes"}} {
		if code, _ := runHostConfigUnmanageCLI(t, args...); code != 0 {
			t.Fatal("fixture could not establish management")
		}
	}
	codecs, err := nativeconfig.Codecs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	codec, err := statefile.NewNativeCodec(codecs.Contributions())
	if err != nil {
		t.Fatal(err)
	}
	physical, err := statefile.NewPhysicalCodec(codec)
	if err != nil {
		t.Fatal(err)
	}
	state, err := statefile.LoadOptionalPhysicalWithOptions(t.Context(), paths.StatefilePath, statefile.ReadOptions{Codec: physical, PhysicalPathBudget: physicalwork.NewCaptureBudget(nil)})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := entity.New(entity.KindHostConfig, "preferences")
	managements, err := journaldocument.HostConfigManagements(t.Context(), state.Snapshot(), id)
	if err != nil || len(managements) != 1 {
		t.Fatal("fixture retained selection failed", err)
	}
	after, err := managements[0].RemoveFrom(t.Context(), state.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	stateAfter, err := state.Codec().EncodeState(after)
	if err != nil {
		t.Fatal(err)
	}
	stateBefore, err := os.ReadFile(paths.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := ownershipstore.New(paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := store.LoadProtectionRegistry(t.Context())
	if err != nil || len(registry.DocumentBindings()) != 1 {
		t.Fatal("fixture lacks its native binding", err)
	}
	binding := registry.DocumentBindings()[0]
	namespace, err := journalrecord.NewNativeReleaseExactNamespace(binding.PathAuthority())
	if err != nil {
		t.Fatal(err)
	}
	bound, err := ownership.PresentDocumentBinding(binding)
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	input := journalrecord.NativeReleaseInput{OperationID: journalpublication.OperationID(created), CreatedAt: created, Owner: binding.Owner(), Before: state.Snapshot(), Management: managements[0], Namespace: namespace, OwnClaimsBefore: registry.Claims(), BindingBefore: bound}

	// This is a post-local-commit interruption with absent declaration and lock.
	// Canonical record construction owns the fixture's persisted grammar.
	for index, path := range []string{paths.ManifestPath, paths.LockfilePath, paths.StatefilePath} {
		if index < 2 {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}
		observed, err := mutation.ObserveDirectoryEntryAuthorityBounded(path, physicalwork.MaximumPhysicalPathDepth, physicalwork.NewCaptureBudget(nil))
		if err != nil {
			t.Fatal(err)
		}
		exact, present := observed.Exact()
		if !present {
			t.Fatal("fixture control target is not exact")
		}
		target, err := journalrecord.NewNativeReleaseExactNamespace(exact)
		if err != nil {
			t.Fatal(err)
		}
		role := []journalrecord.NativeReleaseFileRole{journalrecord.NativeReleaseManifest, journalrecord.NativeReleaseDefaultLock, journalrecord.NativeReleaseState}[index]
		file := journalrecord.NativeReleaseFileInput{Role: role, Target: target}
		if index == 2 {
			file.BeforeExists, file.Write, file.BeforeMode = true, true, 0o600
			file.BeforeSHA256, file.AfterSHA256 = journalrecord.Fingerprint(stateBefore), journalrecord.Fingerprint(stateAfter)
		}
		intent, err := journalrecord.NewNativeReleaseFileIntent(file)
		if err != nil {
			t.Fatal(err)
		}
		input.Files = append(input.Files, intent)
	}
	release, err := journalrecord.NewNativeRelease(t.Context(), input, codec)
	if err != nil {
		t.Fatal(err)
	}
	content, err := release.Encode(t.Context(), codec)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(paths.RecoveryDir, release.OperationID())
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, journalrecord.FileName), content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.StatefilePath, stateAfter, 0o600); err != nil {
		t.Fatal(err)
	}
	hostBefore, err := os.ReadFile(host)
	if err != nil {
		t.Fatal(err)
	}

	prepared, err := workflowrecover.Plan(t.Context(), workflowrecover.PlanInput{ManifestPath: paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	var human, structured bytes.Buffer
	clipresent.PrintRecoverPlanWithFenceOptions(&human, prepared.Disclosure(), fileset.FileSetFenceAbandonedResidue, clipresent.HumanOptions{})
	if !strings.Contains(human.String(), "recover: release_only") || !strings.Contains(human.String(), "continuing file-set fence: abandoned_residue") {
		t.Fatal("release presenter dropped the supplied fence axis", human.String())
	}
	if err := clipresent.PrintRecoverResultJSONWithFence(&structured, "dry_run", prepared.Disclosure(), fileset.FileSetFenceAbandonedResidue, nil, nil); err != nil {
		t.Fatal(err)
	}
	var projection struct {
		Classification string `json:"classification"`
		Fence          string `json:"continuing_file_set_fence"`
	}
	if err := json.Unmarshal(structured.Bytes(), &projection); err != nil || projection.Classification != "release_only" || projection.Fence != "abandoned_residue" {
		t.Fatal("release JSON dropped the supplied fence axis", structured.String(), err)
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}

	if code, _ := runHostConfigUnmanageCLI(t, "recover", "--manifest", paths.ManifestPath, "--dry-run", "--json"); code != 0 {
		t.Fatal("CLI did not disclose release-only recovery")
	}
	if code, _ := runHostConfigUnmanageCLI(t, "recover", "--manifest", paths.ManifestPath, "--yes", "--json"); code != 0 {
		t.Fatal("CLI did not finish release-only recovery")
	}
	actual, err := store.LoadProtectionRegistry(t.Context())
	if err != nil || len(actual.Claims()) != 0 || len(actual.DocumentBindings()) != 0 {
		t.Fatal("CLI recovery retained own management", err)
	}
	if actual, err := os.ReadFile(host); err != nil || !bytes.Equal(actual, hostBefore) {
		t.Fatal("CLI recovery changed host content", err)
	}
}
