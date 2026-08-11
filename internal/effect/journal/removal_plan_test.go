//go:build darwin || linux

package journal

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

type failingRemovalRegularFileReader struct {
	mutationfs.RootedReader
}

func (reader failingRemovalRegularFileReader) ReadRootedRegularFileUpTo(
	context.Context,
	rootedpath.CommitCapability,
	int64,
) ([]byte, fs.FileMode, mutationfs.EntryIdentity, error) {
	return nil, 0, nil, errors.New("injected bounded removal read failure")
}

func TestReservedRemovalReobservationReadsEmptyFileAndDirectory(t *testing.T) {
	for _, test := range []struct {
		name      string
		make      func(string) error
		kind      string
		directory bool
	}{
		{
			name: "file",
			make: func(path string) error { return os.WriteFile(path, nil, 0o600) },
			kind: recovery.PathKindFile,
		},
		{
			name:      "directory",
			make:      func(path string) error { return os.Mkdir(path, 0o700) },
			kind:      recovery.PathKindDirectory,
			directory: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatalf("canonicalize test root: %v", err)
			}
			selected := filepath.Join(root, "entry")
			if err := test.make(selected); err != nil {
				t.Fatalf("create empty entry: %v", err)
			}
			captured, err := rootedpath.CaptureRootNoFollow(root)
			if err != nil {
				t.Fatalf("capture test root: %v", err)
			}
			defer captured.Close()
			budget, err := recovery.NewPhysicalWorkBudget(1)
			if err != nil {
				t.Fatalf("construct removal budget: %v", err)
			}
			capability, err := acquireRemovalObservationSlot(captured, selected, budget)
			if err != nil {
				t.Fatalf("acquire empty entry: %v", err)
			}
			if err := budget.ReserveExecutionObservations(1, 1, 1, mutationfs.RootedAbsencePathObservationCount); err != nil {
				t.Fatalf("reserve execution observations: %v", err)
			}
			empty, err := recovery.NewArtifactWork(0, 0)
			if err != nil {
				t.Fatalf("construct empty work: %v", err)
			}
			var reserveErr error
			if test.directory {
				reserveErr = budget.ReserveDirectoryReobservation(empty)
			} else {
				reserveErr = budget.ReserveReobservation(empty)
			}
			if reserveErr != nil {
				t.Fatalf("reserve empty work: %v", reserveErr)
			}
			execution, err := budget.BeginReservedExecution()
			if err != nil {
				t.Fatalf("begin reserved execution: %v", err)
			}
			entry, _, work, err := ObserveRootedRemovalEntry(
				t.Context(),
				storagecommit.Adapter{},
				capability,
				execution,
				empty,
			)
			if err != nil {
				t.Fatalf("observe empty entry: %v", err)
			}
			if entry.Kind() != test.kind || work.Entries() != 0 || work.Bytes() != 0 {
				t.Fatalf("empty entry observation = kind:%q entries:%d bytes:%d", entry.Kind(), work.Entries(), work.Bytes())
			}
		})
	}
}

func TestReservedEmptyRemovalReobservationRejectsPositiveGrowth(t *testing.T) {
	for _, test := range []struct {
		name      string
		make      func(string) error
		change    func(string) error
		directory bool
	}{
		{
			name: "file grows",
			make: func(path string) error { return os.WriteFile(path, nil, 0o600) },
			change: func(path string) error {
				return os.WriteFile(path, []byte("x"), 0o600)
			},
		},
		{
			name:      "directory gains entry",
			directory: true,
			make:      func(path string) error { return os.Mkdir(path, 0o700) },
			change: func(path string) error {
				return os.WriteFile(filepath.Join(path, "child"), nil, 0o600)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			selected := filepath.Join(root, "entry")
			if err := test.make(selected); err != nil {
				t.Fatal(err)
			}
			captured, err := rootedpath.CaptureRootNoFollow(root)
			if err != nil {
				t.Fatal(err)
			}
			defer captured.Close()
			budget, err := recovery.NewPhysicalWorkBudget(1)
			if err != nil {
				t.Fatal(err)
			}
			capability, err := acquireRemovalObservationSlot(captured, selected, budget)
			if err != nil {
				t.Fatal(err)
			}
			defer capability.Close()
			if err := budget.ReserveExecutionObservations(1, 1, 1, mutationfs.RootedAbsencePathObservationCount); err != nil {
				t.Fatal(err)
			}
			empty, err := recovery.NewArtifactWork(0, 0)
			if err != nil {
				t.Fatal(err)
			}
			var reserveErr error
			if test.directory {
				reserveErr = budget.ReserveDirectoryReobservation(empty)
			} else {
				reserveErr = budget.ReserveReobservation(empty)
			}
			if reserveErr != nil {
				t.Fatal(reserveErr)
			}
			execution, err := budget.BeginReservedExecution()
			if err != nil {
				t.Fatal(err)
			}
			if err := test.change(selected); err != nil {
				t.Fatal(err)
			}

			observation, _, _, err := ObserveRootedRemovalEntry(
				t.Context(),
				storagecommit.Adapter{},
				capability,
				execution,
				empty,
			)
			if test.directory {
				if err != nil || observation.Status() != recovery.RemovalResidueEntryUnavailable {
					t.Fatalf("grown empty directory observation = %#v, err=%v, want unavailable", observation, err)
				}
				if execution.RemainingEntries() != 0 {
					t.Fatalf(
						"grown empty directory left %d overflow entries, want 0",
						execution.RemainingEntries(),
					)
				}
			} else if err == nil || !strings.Contains(err.Error(), "preflight maximum") {
				t.Fatalf("grown empty file error = %v, want preflight-work rejection", err)
			}
		})
	}
}

func TestRemovalObservationPreservesCancellation(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonicalize canceled observation root: %v", err)
	}
	selected := filepath.Join(root, "entry")
	if err := os.WriteFile(selected, nil, 0o600); err != nil {
		t.Fatalf("create canceled observation entry: %v", err)
	}
	captured, err := rootedpath.CaptureRootNoFollow(root)
	if err != nil {
		t.Fatalf("capture canceled observation root: %v", err)
	}
	defer captured.Close()
	budget, err := recovery.NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatalf("construct canceled observation budget: %v", err)
	}
	capability, err := acquireRemovalObservationSlot(captured, selected, budget)
	if err != nil {
		t.Fatalf("acquire canceled observation entry: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, _, err = ObserveRootedRemovalEntry(
		ctx,
		storagecommit.Adapter{},
		capability,
		budget,
		budget.RemainingTreeWork(),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled removal observation error = %v, want context.Canceled", err)
	}
}

func TestRemovalObservationFailureConsumesReservedMaximumWork(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(root, "entry")
	if err := os.WriteFile(selected, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	captured, err := rootedpath.CaptureRootNoFollow(root)
	if err != nil {
		t.Fatal(err)
	}
	defer captured.Close()
	maximum, err := recovery.NewArtifactWork(0, 5)
	if err != nil {
		t.Fatal(err)
	}
	budget, err := recovery.NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := acquireRemovalObservationSlot(captured, selected, budget)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveExecutionObservations(1, 1, 1, mutationfs.RootedAbsencePathObservationCount); err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveReobservation(maximum); err != nil {
		t.Fatal(err)
	}
	execution, err := budget.BeginReservedExecution()
	if err != nil {
		t.Fatal(err)
	}
	reader := failingRemovalRegularFileReader{RootedReader: storagecommit.Adapter{}}

	observation, _, work, err := ObserveRootedRemovalEntry(
		t.Context(),
		reader,
		capability,
		execution,
		maximum,
	)
	if err != nil {
		t.Fatalf("ObserveRootedRemovalEntry() error = %v", err)
	}
	if observation.Status() != recovery.RemovalResidueEntryUnavailable ||
		work.Entries() != 0 || work.Bytes() != 0 {
		t.Fatalf("failed observation = %#v work=%d/%d", observation, work.Entries(), work.Bytes())
	}
	if execution.RemainingBytes() != 0 {
		t.Fatalf("failed observation remaining bytes = %d, want 0", execution.RemainingBytes())
	}
}

func TestRemovalObservationCancellationPreservesBoundaryCause(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		wrapped := fmt.Errorf("storage observation interrupted: %w", cause)
		if got := removalObservationCancellation(wrapped); got != wrapped {
			t.Fatalf("cancellation translation = %v, want original %v", got, wrapped)
		}
	}
}

func TestRemovalNamespaceCancellationIsNotAuthorityDrift(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		wrapped := fmt.Errorf("namespace provenance interrupted: %w", cause)
		if got := removalNamespaceCancellation(t.Context(), wrapped); got != wrapped {
			t.Fatalf("namespace cancellation = %v, want original %v", got, wrapped)
		}
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got := removalNamespaceCancellation(ctx, nil); !errors.Is(got, context.Canceled) {
		t.Fatalf("post-provenance cancellation = %v, want context.Canceled", got)
	}
}

func TestAssessRemovalCleanupIntentDisclosesPhysicalContinuation(t *testing.T) {
	for _, test := range []struct {
		name       string
		place      func(string, mutationfs.LogicalRemovalNames, []byte) error
		wantAction recovery.RemovalCleanupActionKind
	}{
		{
			name: "residue promotion",
			place: func(parent string, names mutationfs.LogicalRemovalNames, content []byte) error {
				return os.WriteFile(filepath.Join(parent, names.Residue()), content, 0o600)
			},
			wantAction: recovery.RemovalCleanupActionPromoteResidue,
		},
		{
			name: "partial cleanup continuation",
			place: func(parent string, names mutationfs.LogicalRemovalNames, content []byte) error {
				return os.WriteFile(filepath.Join(parent, names.Cleanup()), content, 0o600)
			},
			wantAction: recovery.RemovalCleanupActionCleanupProgress,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatalf("canonicalize test root: %v", err)
			}
			parent := filepath.Join(root, "managed")
			if err := os.Mkdir(parent, 0o700); err != nil {
				t.Fatalf("create managed parent: %v", err)
			}
			content := []byte("authorized")
			intent := removalPlanTestIntent(t, parent, content)
			if err := test.place(parent, intent.Namespace().Names(), content); err != nil {
				t.Fatalf("place removal slot: %v", err)
			}
			budget, err := recovery.NewPhysicalWorkBudget(1)
			if err != nil {
				t.Fatalf("construct removal budget: %v", err)
			}
			obligation, err := assessRemovalCleanupIntent(
				t.Context(),
				intent,
				PlanLoadOptions{
					Filesystem: storagecommit.Adapter{},
					Resolver: func(output.Destination) (string, error) {
						return filepath.Join(parent, "config"), nil
					},
				},
				budget,
			)
			if err != nil {
				t.Fatalf("assess removal cleanup: %v", err)
			}
			if obligation.Readiness() != recovery.RemovalCleanupReady ||
				obligation.Action() != test.wantAction {
				t.Fatalf("cleanup obligation = %#v, want ready %s", obligation, test.wantAction)
			}
		})
	}
}

func removalPlanTestIntent(
	t *testing.T,
	parent string,
	content []byte,
) recovery.RemovalIntent {
	t.Helper()
	parentProvenance := removalPlanTestProvenance(t, parent)
	names, err := mutationfs.NewLogicalRemovalNames(
		".daem-tombstone-0123456789abcdef0123456789abcdef",
		".daem-cleanup-0123456789abcdef0123456789abcdef",
	)
	if err != nil {
		t.Fatalf("construct removal names: %v", err)
	}
	namespace, err := recovery.NewExistingParentAuthority(
		parentProvenance,
		names,
	)
	if err != nil {
		t.Fatalf("construct removal namespace: %v", err)
	}
	state, err := recovery.NewExpectedRemovalState(recovery.ExpectedPathState{
		Existed:     true,
		Kind:        recovery.PathKindFile,
		ContentHash: string(artifact.HashFileContentWithExecutable(content, false)),
		PathMode:    recovery.NewPermissionMode(0o600),
	})
	if err != nil {
		t.Fatalf("construct removal state: %v", err)
	}
	destination, err := output.Parse("managed/config")
	if err != nil {
		t.Fatalf("construct removal destination: %v", err)
	}
	demand, err := recovery.NewRemovalDemand(target.ScopeProject, destination, []recovery.RemovalState{state})
	if err != nil {
		t.Fatalf("construct removal demand: %v", err)
	}
	intent, err := recovery.NewRemovalIntent(demand, namespace)
	if err != nil {
		t.Fatalf("construct removal intent: %v", err)
	}
	return intent
}

func removalPlanTestProvenance(
	t *testing.T,
	path string,
) recovery.RootProvenance {
	t.Helper()
	root, err := rootedpath.CaptureRootNoFollow(path)
	if err != nil {
		t.Fatalf("capture removal root: %v", err)
	}
	authority, authorityErr := root.Authority()
	if authorityErr != nil {
		_ = root.Close()
		t.Fatalf("read removal root authority: %v", authorityErr)
	}
	provenance, provenanceErr := authority.Provenance()
	closeErr := root.Close()
	if provenanceErr != nil || closeErr != nil {
		t.Fatalf("read removal root provenance: provenance=%v close=%v", provenanceErr, closeErr)
	}
	result, err := recovery.NewRootProvenance(
		provenance.PhysicalRoot(),
		provenance.ObjectFingerprint(),
		provenance.MountFingerprint(),
	)
	if err != nil {
		t.Fatalf("construct removal root provenance: %v", err)
	}
	return result
}
