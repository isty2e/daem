package authoring

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	"github.com/isty2e/daem/internal/effect/mutation"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/output"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit/metadatatx"
)

func TestUnmanageExtensionRecoveryFencePrecedesEveryMetadataRead(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(*testing.T, daempaths.Paths)
		wantError string
	}{
		{
			name: "active",
			setup: func(t *testing.T, paths daempaths.Paths) {
				captureUnmanageActiveJournal(t, paths)
			},
			wantError: "interrupted apply operation found",
		},
		{
			name: "retained cleanup",
			setup: func(t *testing.T, paths daempaths.Paths) {
				prepareUnmanageRetirementState(t, paths, retirement.PhasePrepared, true)
			},
			wantError: "journal cleanup is incomplete",
		},
		{
			name: "finalizing cleanup",
			setup: func(t *testing.T, paths daempaths.Paths) {
				prepareUnmanageRetirementState(t, paths, retirement.PhaseFinalizing, false)
			},
			wantError: "journal cleanup is incomplete",
		},
		{
			name: "malformed reserved evidence",
			setup: func(t *testing.T, paths daempaths.Paths) {
				writeUnmanageDirectory(
					t,
					filepath.Join(paths.RecoveryDir, "retirement-v1-invalid"),
				)
			},
			wantError: "recovery inventory is blocked",
		},
		{
			name: "legacy unprovable evidence",
			setup: func(t *testing.T, paths daempaths.Paths) {
				writeUnmanageDirectory(
					t,
					filepath.Join(
						paths.RecoveryDir,
						".daem-tombstone-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					),
				)
			},
			wantError: "recovery inventory is blocked",
		},
	}
	for _, test := range tests {
		for _, mode := range []UnmanageMode{UnmanageModeDryRun, UnmanageModeWrite} {
			t.Run(test.name+"/"+string(mode), func(t *testing.T) {
				root := t.TempDir()
				configureUnmanageTestHomes(t, root)
				paths := unmanageTestPaths(t, root)
				hostPath := filepath.Join(root, "host", "extension")
				metadataPaths := []string{
					paths.ManifestPath,
					paths.LockfilePath,
					paths.StatefilePath,
					paths.CarrierClaimRegistryPath,
					hostPath,
				}
				for index, path := range metadataPaths {
					writeUnmanageFile(
						t,
						path,
						[]byte("unreadable-sentinel-"+string(rune('a'+index))+"\n"),
					)
				}
				test.setup(t, paths)
				metadataBefore := captureUnmanageFileImages(t, metadataPaths)
				recoveryBefore := captureUnmanageTree(t, paths.RecoveryDir)

				_, err := UnmanageExtension(t.Context(), UnmanageExtensionRequest{
					ManifestPath: paths.ManifestPath,
					ID:           "context7",
					Mode:         mode,
				})
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf(
						"UnmanageExtension error = %v, want %q",
						err,
						test.wantError,
					)
				}
				assertUnmanageFileImages(t, metadataPaths, metadataBefore)
				if after := captureUnmanageTree(t, paths.RecoveryDir); !reflect.DeepEqual(after, recoveryBefore) {
					t.Fatalf(
						"recovery evidence changed:\nbefore=%#v\nafter=%#v",
						recoveryBefore,
						after,
					)
				}
			})
		}
	}
}

func TestUnmanageDryRunReportsRecoverableJournalWithContinuingResidue(t *testing.T) {
	root := t.TempDir()
	configureUnmanageTestHomes(t, root)
	paths := unmanageTestPaths(t, root)
	writeUnmanageFile(
		t,
		paths.ManifestPath,
		[]byte(unmanageManifest("context7@official", target.ScopeProject)),
	)
	captureUnmanageActiveJournal(t, paths)
	residue := filepath.Join(paths.StateDir, ".daem-tmp-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := os.Mkdir(residue, 0o700); err != nil {
		t.Fatal(err)
	}

	_, err := UnmanageExtension(t.Context(), UnmanageExtensionRequest{
		ManifestPath: paths.ManifestPath,
		ID:           "context7",
		Mode:         UnmanageModeDryRun,
	})
	if err == nil {
		t.Fatal("expected joint journal and residue refusal")
	}
	if !strings.Contains(err.Error(), "interrupted apply operation found") {
		t.Fatalf("error = %v, want recoverable journal", err)
	}
	if !errors.Is(err, transaction.ErrAbandonedFileSetResidue) {
		t.Fatalf("error = %v, want continuing residue", err)
	}
	if !strings.Contains(err.Error(), "does not clear the continuing file-set fence") {
		t.Fatalf("error = %v, want continuing-fence diagnosis", err)
	}
	if _, statErr := os.Lstat(residue); statErr != nil {
		t.Fatalf("residue disappeared: %v", statErr)
	}
}

func TestUnmanageExtensionAllowsFinalizedGCResidue(t *testing.T) {
	root := t.TempDir()
	configureUnmanageTestHomes(t, root)
	paths := unmanageTestPaths(t, root)
	writeUnmanageFile(
		t,
		paths.ManifestPath,
		[]byte(unmanageManifest("context7@official", target.ScopeProject)),
	)
	captured, identity := captureUnmanageActiveJournal(t, paths)
	if err := os.RemoveAll(captured.Directory); err != nil {
		t.Fatal(err)
	}
	writeUnmanageDirectory(t, filepath.Join(paths.RecoveryDir, identity.GCName()))
	recoveryBefore := captureUnmanageTree(t, paths.RecoveryDir)

	result, err := UnmanageExtension(t.Context(), UnmanageExtensionRequest{
		ManifestPath: paths.ManifestPath,
		ID:           "context7",
		Mode:         UnmanageModeWrite,
	})
	if err != nil {
		t.Fatalf("UnmanageExtension returned error: %v", err)
	}
	if result.ManifestStatus != UnmanageManifestStatusRemoved ||
		result.LockfileStatus != LockfileStatusWritten {
		t.Fatalf("write result = %#v, want committed declaration removal", result)
	}
	if after := captureUnmanageTree(t, paths.RecoveryDir); !reflect.DeepEqual(after, recoveryBefore) {
		t.Fatalf(
			"finalized GC residue changed:\nbefore=%#v\nafter=%#v",
			recoveryBefore,
			after,
		)
	}
}

func TestCommitUnmanageCandidateRejectsJournalAppearingAfterOptimisticPlan(t *testing.T) {
	root := t.TempDir()
	configureUnmanageTestHomes(t, root)
	paths := unmanageTestPaths(t, root)
	writeUnmanageFile(
		t,
		paths.ManifestPath,
		[]byte(unmanageManifest("context7@official", target.ScopeProject)),
	)
	fixture := newUnmanageTestFixture(
		t,
		"context7",
		"context7@official",
		target.ScopeProject,
	)
	claim := unmanageTestClaim(t, fixture, unmanageTestOwner(t, paths))
	state, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedCarrierClaims: []durablecarrier.ManagedCarrierClaim{claim},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeUnmanageState(t, paths.StatefilePath, state)
	hostPath := filepath.Join(root, "host", "extension")
	writeUnmanageFile(t, hostPath, []byte("host-state\n"))

	request := UnmanageExtensionRequest{
		ManifestPath: paths.ManifestPath,
		ID:           "context7",
		Mode:         UnmanageModeWrite,
	}
	barrier, err := recoverygate.NewEffectAuthority(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	optimistic, err := buildUnmanageCandidate(t.Context(), request, paths, false, barrier, nil)
	if err != nil {
		t.Fatal(err)
	}
	captureUnmanageActiveJournal(t, paths)
	metadataPaths := []string{
		paths.ManifestPath,
		paths.LockfilePath,
		paths.StatefilePath,
		paths.CarrierClaimRegistryPath,
		hostPath,
	}
	metadataBefore := captureUnmanageFileImages(t, metadataPaths)
	recoveryBefore := captureUnmanageTree(t, paths.RecoveryDir)

	_, err = commitUnmanageCandidate(t.Context(), optimistic)
	if err == nil || !strings.Contains(err.Error(), "interrupted apply operation found") {
		t.Fatalf("commitUnmanageCandidate error = %v, want recovery fence", err)
	}
	assertUnmanageFileImages(t, metadataPaths, metadataBefore)
	if after := captureUnmanageTree(t, paths.RecoveryDir); !reflect.DeepEqual(after, recoveryBefore) {
		t.Fatalf("recovery evidence changed:\nbefore=%#v\nafter=%#v", recoveryBefore, after)
	}
	markerPath, err := transaction.FileSetAuthorityPath(paths.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("metadata transaction marker stat error = %v, want absent", err)
	}
}

func TestUnmanageMetadataRecoveryRefusesJournalBeforeRepairingFileSet(t *testing.T) {
	root := t.TempDir()
	configureUnmanageTestHomes(t, root)
	paths := unmanageTestPaths(t, root)
	writeUnmanageFile(
		t,
		paths.ManifestPath,
		[]byte(unmanageManifest("context7@official", target.ScopeProject)),
	)
	metadatatx.WriteInterruptedForAbsentTarget(
		t,
		paths.StateDir,
		paths.LockfilePath,
	)
	captureUnmanageActiveJournal(t, paths)
	targetPaths := []string{
		paths.ManifestPath,
		paths.LockfilePath,
		paths.StatefilePath,
		paths.CarrierClaimRegistryPath,
	}
	metadataBefore := captureUnmanageFileImages(t, targetPaths)
	stateDirBefore := captureUnmanageTree(t, paths.StateDir)

	barrier, err := recoverygate.NewEffectAuthority(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	err = recoverUnmanageFileSetBeforeRead(t.Context(), paths, targetPaths, barrier)
	if err == nil || !strings.Contains(err.Error(), "interrupted apply operation found") {
		t.Fatalf("recoverUnmanageFileSetBeforeRead error = %v, want recovery fence", err)
	}
	assertUnmanageFileImages(t, targetPaths, metadataBefore)
	if after := captureUnmanageTree(t, paths.StateDir); !reflect.DeepEqual(after, stateDirBefore) {
		t.Fatalf(
			"state directory changed:\nbefore=%#v\nafter=%#v",
			stateDirBefore,
			after,
		)
	}
}

func TestUnmanageMutationAuthorityConflictsWithRecoveryWriters(t *testing.T) {
	root := t.TempDir()
	configureUnmanageTestHomes(t, root)
	paths := unmanageTestPaths(t, root)
	markerPath, err := transaction.FileSetAuthorityPath(paths.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	domains, err := unmanageMutationDomains(
		[]string{
			paths.ManifestPath,
			paths.LockfilePath,
			paths.StatefilePath,
			paths.CarrierClaimRegistryPath,
		},
		markerPath,
		nil,
		paths.RecoveryDir,
	)
	if err != nil {
		t.Fatal(err)
	}
	store, err := mutation.NewStore(paths.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	holder, err := store.Acquire(t.Context(), domains...)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()

	for _, test := range []struct {
		name   string
		effect mutation.PathEffect
	}{
		{name: "directory entry", effect: mutation.PathEffectDirectoryEntry},
		{name: "referent", effect: mutation.PathEffectReferent},
	} {
		t.Run(test.name, func(t *testing.T) {
			writer, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{
				Path:   paths.RecoveryDir,
				Access: mutation.AccessExclusive,
				Effect: test.effect,
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			contended, err := store.Acquire(ctx, writer)
			if contended != nil {
				defer contended.Release()
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("exclusive recovery acquire error = %v, want deadline", err)
			}
		})
	}
}

func captureUnmanageActiveJournal(
	t *testing.T,
	paths daempaths.Paths,
) (journal.CaptureResult, retirement.Identity) {
	t.Helper()
	operationID := journal.OperationID(
		time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC),
	)
	fixture := newUnmanageTestFixture(
		t,
		"context7",
		"context7@official",
		target.ScopeProject,
	)
	after, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedCarrierClaims: []durablecarrier.ManagedCarrierClaim{
			unmanageTestClaim(t, fixture, unmanageTestOwner(t, paths)),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := journal.CaptureJournalWithOptions(
		t.Context(),
		unmanageJournalPaths(paths),
		operationID,
		time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC),
		durable.EmptySnapshot(),
		after,
		journal.CaptureOptions{
			Resolver: func(output.Destination) (string, error) {
				return "", nil
			},
			StateCodec: statefile.Codec{},
			Filesystem: storagecommit.Adapter{},
		},
	)
	if err != nil {
		t.Fatalf("CaptureJournalWithOptions: %v", err)
	}
	recoverable, err := journal.LoadRecoverablePlanWithOptions(
		t.Context(),
		unmanageJournalPaths(paths),
		journal.PlanLoadOptions{
			StateCodec: statefile.Codec{},
			StateReader: func(context.Context) (durable.Snapshot, error) {
				return durable.EmptySnapshot(), nil
			},
			Filesystem: storagecommit.Adapter{},
		},
	)
	if err != nil {
		t.Fatalf("LoadRecoverablePlanWithOptions: %v", err)
	}
	active, ok := journal.ActiveRecoveryPlan(recoverable)
	if !ok {
		t.Fatal("captured journal did not produce an active recovery plan")
	}
	fingerprint, err := active.JournalAuthorityFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := retirement.NewIdentity(operationID, fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	return result, identity
}

func prepareUnmanageRetirementState(
	t *testing.T,
	paths daempaths.Paths,
	phase retirement.Phase,
	residuePresent bool,
) {
	t.Helper()
	result, identity := captureUnmanageActiveJournal(t, paths)
	record, err := retirement.NewRecord(
		identity.OperationID(),
		identity.JournalAuthorityFingerprint(),
		phase,
	)
	if err != nil {
		t.Fatal(err)
	}
	content, err := retirement.Encode(record)
	if err != nil {
		t.Fatal(err)
	}
	controlPath := filepath.Join(paths.RecoveryDir, identity.ControlName())
	writeUnmanageDirectory(t, controlPath)
	writeUnmanageFile(
		t,
		filepath.Join(controlPath, retirement.RecordFileName),
		content,
	)
	residuePath := filepath.Join(paths.RecoveryDir, identity.ResidueName())
	if err := os.Rename(result.Directory, residuePath); err != nil {
		t.Fatal(err)
	}
	if !residuePresent {
		if err := os.RemoveAll(residuePath); err != nil {
			t.Fatal(err)
		}
	}
}

func unmanageJournalPaths(paths daempaths.Paths) journal.Paths {
	return journal.Paths{
		RecoveryDir:   paths.RecoveryDir,
		StatefilePath: paths.StatefilePath,
		ManifestRoot:  paths.ManifestRoot,
		DataDir:       paths.DataDir,
	}
}

type unmanageFileImage struct {
	exists  bool
	mode    os.FileMode
	content []byte
}

func captureUnmanageFileImages(
	t *testing.T,
	paths []string,
) map[string]unmanageFileImage {
	t.Helper()
	images := make(map[string]unmanageFileImage, len(paths))
	for _, path := range paths {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			images[path] = unmanageFileImage{}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		images[path] = unmanageFileImage{
			exists:  true,
			mode:    info.Mode(),
			content: content,
		}
	}
	return images
}

func assertUnmanageFileImages(
	t *testing.T,
	paths []string,
	want map[string]unmanageFileImage,
) {
	t.Helper()
	got := captureUnmanageFileImages(t, paths)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata changed:\nwant=%#v\ngot=%#v", want, got)
	}
}

func captureUnmanageTree(t *testing.T, root string) map[string]string {
	t.Helper()
	result := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		value := info.Mode().String()
		if info.Mode().IsRegular() {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			value += ":" + string(content)
		}
		result[relative] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func writeUnmanageDirectory(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
}
