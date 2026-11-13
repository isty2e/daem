package authoring

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/execute/nativeoperation"
	"github.com/isty2e/daem/internal/effect/fileset"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	workflowrecover "github.com/isty2e/daem/internal/workflow/recover"
	"github.com/isty2e/daem/test/testkit/metadatatx"
)

func interruptHostRelease(t *testing.T, fixture hostReleaseFixture, boundary string) (hostConfigReleaseCandidate, journalrecord.NativeRelease) {
	t.Helper()
	budget := physicalwork.NewCaptureBudget(nil)
	candidate, err := buildHostConfigRelease(t.Context(), fixture.paths, fixture.request, fixture.contributions, budget)
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC)
	id := journalpublication.OperationID(created)
	release, err := candidate.release(t.Context(), id, created, budget)
	if err != nil {
		t.Fatal(err)
	}
	content, err := release.Encode(t.Context(), candidate.codec)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := retirement.NewIdentity(id, journalrecord.Fingerprint(content))
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(fixture.paths.RecoveryDir, id)
	marker, err := fileset.FileSetAuthorityPath(fixture.paths.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	var domains []mutation.Domain
	for _, path := range []string{candidate.files[0].path, candidate.files[1].path, candidate.files[2].path, directory, filepath.Join(directory, journalrecord.FileName), marker, fixture.paths.OwnershipRegistryPath} {
		for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
			domain, err := mutation.NewPhysicalPathDomain(mutation.PhysicalPathRequest{Path: path, Access: mutation.AccessExclusive, Effect: effect, Target: "metadata", Scope: "project"})
			if err != nil {
				t.Fatal(err)
			}
			domains = append(domains, domain)
		}
	}
	store, err := mutation.NewStore(fixture.paths.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	leases, err := store.Acquire(t.Context(), domains...)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := leases.Release(); err != nil {
			t.Error(err)
		}
	}()
	targets, err := candidate.fileTargets()
	if err != nil {
		t.Fatal(err)
	}
	codec, err := statefile.NewPhysicalCodec(candidate.codec)
	if err != nil {
		t.Fatal(err)
	}
	interruption := errors.New("selected metadata release boundary")
	seen := false
	options := nativeoperation.ReleaseOptions{
		Filesystem: storagecommit.Adapter{}, RegistryBinder: ownershipstore.BindOwnReleaseRooted, Budget: budget, StateCodec: candidate.codec,
		StateReader: func(ctx context.Context) (durable.Snapshot, error) {
			return statefile.LoadOptionalWithOptions(ctx, fixture.paths.StatefilePath, statefile.ReadOptions{Codec: codec, PhysicalPathBudget: budget})
		},
		ValidateBeforeEffects: func(_ context.Context, requested mutation.PhysicalAuthoritySet) error {
			covered, err := leases.CoversPhysicalAuthority(requested)
			if err != nil {
				return err
			}
			if !covered {
				return errors.New("release exceeded selected metadata leases")
			}
			return nil
		},
		AcceptVisibilityChanges: func(ctx context.Context) error {
			if accepted, err := leases.AcceptVisibilityChanges(ctx); err != nil || !accepted {
				return errors.Join(errors.New("release visibility changed"), err)
			}
			_, journalErr := os.Stat(filepath.Join(directory, journalrecord.FileName))
			state := readUnmanageFile(t, fixture.paths.StatefilePath)
			registryStore, err := ownershipstore.New(fixture.paths.OwnershipRegistryPath)
			if err != nil {
				return err
			}
			registry, err := registryStore.LoadProtectionRegistry(ctx)
			if err != nil {
				return err
			}
			_, preparedErr := os.Stat(filepath.Join(fixture.paths.RecoveryDir, identity.ControlName(), retirement.RecordFileName))
			before := bytes.Equal(state, candidate.files[2].content)
			after := bytes.Equal(state, candidate.files[2].after)
			ownBefore := release.Metadata().RequireBefore(registry) == nil
			_, changed, ownErr := release.Metadata().ApplyAfterCommit(registry)
			match := boundary == "journal" && journalErr == nil && before || boundary == "local-files" && after && ownBefore || boundary == "own-metadata" && after && ownErr == nil && !changed || boundary == "prepared-retirement" && preparedErr == nil
			if match {
				seen = true
				return interruption
			}
			return nil
		},
	}
	err = nativeoperation.ExecuteNativeRelease(t.Context(), release, journalrecord.NativeReleaseControlPaths{Manifest: candidate.files[0].path, DefaultLock: candidate.files[1].path, State: candidate.files[2].path}, targets, execute.Paths{
		ManifestRoot: fixture.paths.ManifestRoot, StateDir: fixture.paths.StateDir, StatefilePath: fixture.paths.StatefilePath, RecoveryDir: fixture.paths.RecoveryDir, DataDir: fixture.paths.DataDir, OwnershipRegistryPath: fixture.paths.OwnershipRegistryPath,
	}, options)
	if !seen || !errors.Is(err, interruption) {
		t.Fatal("release did not stop at the selected durable boundary", boundary, err)
	}
	return candidate, release
}

func TestHostConfigReleaseRecoveryUsesTheCompleteLocalCommit(t *testing.T) {
	for _, boundary := range []string{"journal", "partial-files", "local-files", "own-metadata", "prepared-retirement"} {
		t.Run(boundary, func(t *testing.T) {
			fixture := newHostReleaseFixture(t, false)
			hostBefore := readUnmanageFile(t, fixture.host)
			stop := boundary
			if boundary == "partial-files" {
				stop = "journal"
			}
			candidate, release := interruptHostRelease(t, fixture, stop)
			expectedRegistry := ownership.EmptyRegistry()
			if boundary == "partial-files" {
				var writes []metadatatx.Write
				for _, file := range candidate.files {
					write := metadatatx.Write{Path: file.path, After: file.after, CommitPoint: file.role == journalrecord.NativeReleaseState, Retain: !file.write}
					if file.exists {
						write.Before = &metadatatx.Image{Content: file.content, Mode: file.mode}
					}
					writes = append(writes, write)
				}
				metadatatx.WritePrefix(t, fixture.paths.StateDir, writes, len(writes))
				writeUnmanageFile(t, candidate.files[0].path, candidate.files[0].content)
			}
			if boundary == "own-metadata" {
				// Recovery must not inspect or rewrite a newly occupied host path.
				if err := os.Remove(fixture.host); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(fixture.host, 0o700); err != nil {
					t.Fatal(err)
				}
				foreignRoot := t.TempDir()
				observed, err := mutation.ObserveDirectoryEntryAuthorityBounded(filepath.Join(foreignRoot, "state.json"), physicalwork.MaximumPhysicalPathDepth, physicalwork.NewCaptureBudget(nil))
				if err != nil {
					t.Fatal(err)
				}
				exact, present := observed.Exact()
				if !present {
					t.Fatal("foreign fixture State has no exact namespace")
				}
				owner, err := stateauthority.New(exact, filepath.Join(foreignRoot, "daem.toml"))
				if err != nil {
					t.Fatal(err)
				}
				claim, err := ownership.NewReservedClaim(release.Metadata().RemovedClaims()[0].Address(), owner, "foreign-after-release")
				if err != nil {
					t.Fatal(err)
				}
				expectedRegistry, err = ownership.NewRegistry([]ownership.Claim{claim})
				if err != nil {
					t.Fatal(err)
				}
				content, err := ownershipstore.Marshal(expectedRegistry)
				if err != nil {
					t.Fatal(err)
				}
				writeUnmanageFile(t, fixture.paths.OwnershipRegistryPath, content)
			}
			prepared, err := workflowrecover.Plan(t.Context(), workflowrecover.PlanInput{ManifestPath: fixture.paths.ManifestPath})
			if err != nil {
				t.Fatal("release recovery could not reopen retained authority", err)
			}
			defer prepared.Close()
			if _, continuing := prepared.ContinuingFileSetFence(); continuing {
				t.Fatal("intent-owned FileSet was disclosed as an unrelated continuing fence")
			}
			if boundary == "partial-files" {
				plan, present := journal.NativeReleaseActiveRecoveryPlan(prepared.Disclosure())
				if !present || plan.FileSetDisposition() != fileset.RecoveryRollback {
					t.Fatal("provisional State after-image was treated as committed")
				}
			}
			result, err := workflowrecover.Execute(t.Context(), prepared, workflowrecover.ExecuteOptions{})
			if err != nil || result.Phase() != workflowrecover.ExecutionPhaseCompleted {
				t.Fatal("release recovery did not settle and retire", err, result.Phase())
			}
			state, err := statefile.LoadOptional(t.Context(), fixture.paths.StatefilePath)
			if err != nil {
				t.Fatal(err)
			}
			rollback := boundary == "journal" || boundary == "partial-files"
			if rollback && !state.Equal(release.Before()) || !rollback && !state.Equal(release.After()) {
				t.Fatal("release chose a Snapshot phase before complete FileSet settlement")
			}
			registryStore, err := ownershipstore.New(fixture.paths.OwnershipRegistryPath)
			if err != nil {
				t.Fatal(err)
			}
			registry, err := registryStore.LoadProtectionRegistry(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if rollback {
				expectedRegistry = fixture.registry
			}
			if !registry.Equal(expectedRegistry) {
				t.Fatal("release did not retain or finish only the scoped own phase")
			}
			if boundary == "own-metadata" {
				if info, err := os.Stat(fixture.host); err != nil || !info.IsDir() {
					t.Fatal("recovery changed the foreign host incarnation", err)
				}
			} else if actual := readUnmanageFile(t, fixture.host); !bytes.Equal(actual, hostBefore) {
				t.Fatal("release recovery changed host bytes")
			}
			if _, err := workflowrecover.Plan(t.Context(), workflowrecover.PlanInput{ManifestPath: fixture.paths.ManifestPath}); !errors.Is(err, journal.ErrNoRecoverableJournal) {
				t.Fatal("completed release left actionable journal authority", err)
			}
		})
	}
}
