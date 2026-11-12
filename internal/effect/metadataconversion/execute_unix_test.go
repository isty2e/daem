//go:build darwin || linux

package metadataconversion

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
)

func TestExecutePublishesWholeRegistryWithoutHostEffects(t *testing.T) {
	fixture := compilerFixture(t, 9)
	plan := prepareFixture(t, fixture.paths)
	fixture.assertUnchanged(t)
	if err := plan.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertLocatedFixture(t, fixture)
	if err := recoverygate.RequireGlobalMetadataClear(t.Context(), fixture.paths); err != nil {
		t.Fatal("successful conversion retained a global fence", err)
	}
	noop := prepareFixture(t, fixture.paths)
	if noop.Changed() {
		t.Fatal("already-located full registry did not become a no-op")
	}
	if err := noop.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestExecuteRejectsSourceChangeWithoutPublishingEvidence(t *testing.T) {
	fixture := compilerFixture(t, 2)
	plan := prepareFixture(t, fixture.paths)
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{NativeDocumentCommit: 99})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := statefile.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	writeMetadata(t, fixture.states[1], changed)
	fixture.before[fixture.states[1]] = changed
	err = plan.Execute(t.Context())
	var stale mutation.StaleSnapshotError
	if !errors.As(err, &stale) {
		t.Fatal("changed source did not stale the compiled whole-cohort intent", err)
	}
	fixture.assertUnchanged(t)
	if _, err := os.Stat(fixture.paths.GlobalMetadataDir()); !os.IsNotExist(err) {
		t.Fatal("stale preparation published evidence", err)
	}
}

func TestMaterializationFailureRestoresWholeBeforeSet(t *testing.T) {
	fixture := compilerFixture(t, 3)
	plan := prepareFixture(t, fixture.paths)
	targets := plan.intent.Targets()
	cause := errors.New("metadata semantic materialization failed")
	plan.materialize[targets[1].Path()] = func(context.Context, []byte) ([]byte, error) { return nil, cause }
	if err := plan.Execute(t.Context()); !errors.Is(err, cause) {
		t.Fatal("materialization cause was lost", err)
	}
	fixture.assertUnchanged(t)
	if err := recoverygate.RequireGlobalMetadataClear(t.Context(), fixture.paths); err != nil {
		t.Fatal("complete Before rollback did not clear its fence", err)
	}
}

func TestRecoveryUsesOriginalSelectionAndBeforeRegistries(t *testing.T) {
	for _, allAfter := range []bool{false, true} {
		t.Run(strconv.FormatBool(allAfter), func(t *testing.T) {
			fixture := compilerFixture(t, 9)
			original := prepareFixture(t, fixture.paths)
			crashMetadataPublisher(t, fixture.paths)
			firstPath := original.intent.Targets()[0].Path()
			first, err := os.ReadFile(firstPath)
			if err != nil || bytes.Equal(first, fixture.before[firstPath]) {
				t.Fatal("crash fixture did not leave a real partially published set", err)
			}
			if allAfter {
				// Model the terminal all-After point using the same compiled outputs;
				// the actual process-loss case above exercises the mixed point.
				for path, materialize := range original.materialize {
					after, err := materialize(t.Context(), fixture.before[path])
					if err != nil {
						t.Fatal(err)
					}
					writeMetadata(t, path, after)
				}
			}
			unrelated, err := daempaths.Resolve(filepath.Join(filepath.Dir(fixture.paths.ManifestRoot), "different-project", "daem.toml"))
			if err != nil {
				t.Fatal(err)
			}
			unrelated, err = unrelated.WithDataDir(fixture.paths.DataDir)
			if err != nil {
				t.Fatal(err)
			}
			budget, err := physicalwork.NewBudget(0)
			if err != nil {
				t.Fatal(err)
			}
			recovery, err := InspectRecovery(t.Context(), unrelated, budget)
			if err != nil {
				t.Fatal(err)
			}
			wantAction := fileset.RecoveryRollback
			if allAfter {
				wantAction = fileset.RecoveryFinalize
			}
			if recovery.Action() != wantAction || recovery.OriginalManifestPath() != fixture.paths.ManifestPath || recovery.ParticipantCount() != 9 {
				t.Fatal("recovery changed original selection or disposition", recovery.Action())
			}
			if err := recovery.Execute(t.Context()); err != nil {
				t.Fatal(err)
			}
			if allAfter {
				assertLocatedFixture(t, fixture)
			} else {
				fixture.assertUnchanged(t)
			}
			if err := recoverygate.RequireGlobalMetadataClear(t.Context(), fixture.paths); err != nil {
				t.Fatal("recovery did not retire its own global fence", err)
			}
		})
	}
}

func TestMetadataPublisherCrashHelper(t *testing.T) {
	manifest := os.Getenv("DAEM_METADATA_CRASH_MANIFEST")
	if manifest == "" {
		return
	}
	paths, err := daempaths.Resolve(manifest)
	if err != nil {
		t.Fatal(err)
	}
	paths, err = paths.WithDataDir(os.Getenv("DAEM_METADATA_CRASH_DATA"))
	if err != nil {
		t.Fatal(err)
	}
	plan := prepareFixture(t, paths)
	targets := plan.intent.Targets()
	plan.materialize[targets[1].Path()] = func(context.Context, []byte) ([]byte, error) {
		os.Exit(86)
		return nil, nil
	}
	if err := plan.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Fatal("publisher did not reach the intended post-marker crash point")
}

func TestRecoveryRequiresItsRevisionAndPreservesConflictingLocalEvidence(t *testing.T) {
	fixture := compilerFixture(t, 2)
	crashMetadataPublisher(t, fixture.paths)
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	view, err := fileset.ReadPublicationIntentEvidence5(t.Context(), fixture.paths.GlobalMetadataDir(), mutationfs.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	revision := view.MarkerRevision()
	wrong := "0" + revision[1:]
	if revision[0] == '0' {
		wrong = "1" + revision[1:]
	}
	if _, err := recoverygate.CaptureMetadataRecoveryBarrier(t.Context(), fixture.paths, []string{fixture.paths.StateDir}, wrong, budget); !errors.Is(err, fileset.ErrRecoveryIntentMismatch) {
		t.Fatal("global observation barrier accepted a different revision", err)
	}
	local, err := fileset.FileSetAuthorityPath(fixture.paths.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(local, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectRecovery(t.Context(), fixture.paths, budget); !errors.Is(err, recoverygate.ErrGlobalMetadataFence) {
		t.Fatal("coexisting local evidence was not reported as a global recovery conflict", err)
	}
	for _, path := range []string{local, fixture.paths.GlobalMetadataDir()} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("joint refusal removed recovery evidence", path, err)
		}
	}
	retained, err := fileset.ReadPublicationIntentEvidence5(t.Context(), fixture.paths.GlobalMetadataDir(), mutationfs.MaximumPhysicalPathDepth, budget)
	if err != nil || retained.MarkerRevision() != revision {
		t.Fatal("joint refusal rewrote the owning global revision", err)
	}
}

func crashMetadataPublisher(t *testing.T, paths daempaths.Paths) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), executable, "-test.run=^TestMetadataPublisherCrashHelper$")
	command.Env = append(os.Environ(), "DAEM_METADATA_CRASH_MANIFEST="+paths.ManifestPath, "DAEM_METADATA_CRASH_DATA="+paths.DataDir)
	output, err := command.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 86 {
		t.Fatal("metadata crash fixture failed outside its expected point", err, string(output))
	}
}

func prepareFixture(t *testing.T, paths daempaths.Paths) Plan {
	t.Helper()
	authority, err := recoverygate.NewEffectAuthority(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := Prepare(t.Context(), authority, budget, nil)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func assertLocatedFixture(t *testing.T, fixture compilationFixture) {
	t.Helper()
	for path, snapshot := range fixture.snapshots {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		image, err := statefile.UnmarshalImage(content)
		if err != nil || !image.Snapshot().Equal(snapshot) {
			t.Fatal("converted State lost semantic facts", path, err)
		}
	}
	content, err := os.ReadFile(fixture.paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	outputs, err := ownershipstore.UnmarshalImage(content)
	if err != nil || !outputs.Registry().Equal(fixture.registry) {
		t.Fatal("converted output registry lost semantic facts", err)
	}
	content, err = os.ReadFile(fixture.paths.CarrierClaimRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	carriers, err := carrierclaim.UnmarshalImage(content)
	if err != nil || len(carriers.Registry().Claims()) != 0 {
		t.Fatal("converted carrier registry changed empty facts", err)
	}
	for path, before := range fixture.hosts {
		after, err := os.ReadFile(path)
		if err != nil || !slices.Equal(before, after) {
			t.Fatal("metadata operation changed a host document", path, err)
		}
	}
}
