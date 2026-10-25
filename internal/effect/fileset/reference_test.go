package fileset

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

func TestFileSet4StaticRoundTripAndOldReaderRefusal(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	path := filepath.Join(root, "static")
	writeFixture(t, path, "before", 0o600)
	target := mustWriteTarget(t, path, "after")

	var writes int
	assertPanics(t, func() {
		_ = commitWithOperationsVersion(t.Context(), FileSetInput{
			StateDir: stateDir,
			Targets:  []FileTarget{target},
		}, operations{writeFile: func(ctx context.Context, path string, content []byte, mode os.FileMode) error {
			writes++
			if err := commitFile(ctx, path, content, mode); err != nil {
				return err
			}
			panic("simulated interruption after static write")
		}}, contractversion.MetadataTransactionReference, false)
	})
	assertContent(t, path, "after")
	if writes != 1 {
		t.Fatalf("writer calls = %d, want one", writes)
	}

	if disposition, err := InspectFileSetRecovery4(t.Context(), stateDir, []string{path}); err != nil || disposition != RecoveryFinalize {
		t.Fatalf("InspectFileSetRecovery4 = %q, %v; want finalize", disposition, err)
	}
	if _, err := loadMarker(t.Context(), markerPath(stateDir)); err == nil || !strings.Contains(err.Error(), "newer daem") {
		t.Fatalf("v3 reader error = %v, want future-generation refusal", err)
	}
	if err := RecoverFileSet4(t.Context(), stateDir, []string{path}); err != nil {
		t.Fatalf("RecoverFileSet4: %v", err)
	}
	assertContent(t, path, "after")
	assertMissing(t, transactionDir(stateDir))
	if writes != 1 {
		t.Fatalf("recovery retried the operation: writer calls = %d", writes)
	}
}

func TestCommitFileSet4StaticWritesGenerationFour(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	path := filepath.Join(root, "value")
	writeFixture(t, path, "before", 0o644)
	if err := CommitFileSet4(t.Context(), FileSetInput{
		StateDir: stateDir,
		Targets:  []FileTarget{mustWriteTarget(t, path, "after")},
	}); err != nil {
		t.Fatalf("CommitFileSet4: %v", err)
	}
	assertContent(t, path, "after")
	assertMode(t, path, 0o644)
	assertMissing(t, transactionDir(stateDir))
}

func TestReferenceFileSet4ViewOwnsImagesAndMarkerRevision(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	source := filepath.Join(root, "source")
	manifest := filepath.Join(root, "manifest")
	writeFixture(t, source, "source-before", 0o600)
	writeFixture(t, manifest, "manifest-before", 0o600)

	proposed := []byte("manifest-proposed")
	commitPoint, err := NewFileCommitPointWrite(manifest, proposed)
	if err != nil {
		t.Fatal(err)
	}
	copy(proposed, []byte("caller-mutated!!!"))
	ordinary := mustWriteTarget(t, source, "source-proposed")
	var writes []string
	assertPanics(t, func() {
		_ = commitWithOperationsVersion(t.Context(), FileSetInput{
			StateDir: stateDir,
			Targets:  []FileTarget{commitPoint, ordinary},
		}, operations{writeFile: func(ctx context.Context, path string, content []byte, mode os.FileMode) error {
			writes = append(writes, path)
			if err := commitFile(ctx, path, content, mode); err != nil {
				return err
			}
			if path == ordinary.Path() {
				panic("simulated source-before-reference interruption")
			}
			return nil
		}}, contractversion.MetadataTransactionReference, true)
	})
	assertContent(t, source, "source-proposed")
	assertContent(t, manifest, "manifest-before")
	if len(writes) != 1 || writes[0] != ordinary.Path() {
		t.Fatalf("writes = %v, want only ordinary source target %s", writes, source)
	}

	view, err := ReadFileSetReferenceEvidence4(t.Context(), stateDir, manifest)
	if err != nil {
		t.Fatalf("ReadFileSetReferenceEvidence4: %v", err)
	}
	if view.ReferenceNamespace() != commitPoint.Path() {
		t.Fatalf("reference namespace = %q, want %q", view.ReferenceNamespace(), commitPoint.Path())
	}
	if view.MarkerRevision() != hashBytes(mustReadFile(t, markerPath(stateDir))) {
		t.Fatalf("marker revision %q does not identify the exact marker bytes", view.MarkerRevision())
	}
	before, exists := view.BeforeImage()
	if !exists || !bytes.Equal(before, []byte("manifest-before")) {
		t.Fatalf("before image = %q, exists=%t", before, exists)
	}
	after := view.AfterImage()
	if !bytes.Equal(after, []byte("manifest-proposed")) {
		t.Fatalf("after image = %q, want constructor-owned bytes", after)
	}
	if view.AfterHash() != hashBytes([]byte("manifest-proposed")) {
		t.Fatalf("after hash = %q, want proposed-image hash", view.AfterHash())
	}
	after[0] = 'X'
	before[0] = 'X'
	if !bytes.Equal(view.AfterImage(), []byte("manifest-proposed")) {
		t.Fatal("mutating returned after-image changed the evidence view")
	}
	if beforeAgain, existsAgain := view.BeforeImage(); !existsAgain || !bytes.Equal(beforeAgain, []byte("manifest-before")) {
		t.Fatalf("mutating returned before-image changed the view: %q, %t", beforeAgain, existsAgain)
	}

	if _, err := ReadFileSetReferenceEvidence4(t.Context(), stateDir, source); err == nil ||
		!errors.Is(err, ErrFileSetReferenceNamespaceMismatch) ||
		!strings.Contains(err.Error(), "does not match selected reference namespace") {
		t.Fatalf("retargeted evidence read error = %v, want namespace refusal", err)
	}
	artifact := filepath.Join(transactionDir(stateDir), referenceAfterImageName)
	assertMode(t, artifact, transactionEvidenceMode)
}

func TestReferenceFileSet4SupportsAbsentBeforeAndNormalCommit(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	manifest := filepath.Join(root, "manifest")
	target := mustCommitPointTarget(t, manifest, "proposed")

	assertPanics(t, func() {
		_ = commitWithOperationsVersion(t.Context(), FileSetInput{
			StateDir: stateDir,
			Targets:  []FileTarget{target},
		}, operations{writeFile: func(ctx context.Context, path string, content []byte, mode os.FileMode) error {
			if err := commitFile(ctx, path, content, mode); err != nil {
				return err
			}
			panic("simulated interruption after reference target")
		}}, contractversion.MetadataTransactionReference, true)
	})
	view, err := ReadFileSetReferenceEvidence4(t.Context(), stateDir, manifest)
	if err != nil {
		t.Fatalf("ReadFileSetReferenceEvidence4: %v", err)
	}
	if before, exists := view.BeforeImage(); exists || len(before) != 0 {
		t.Fatalf("absent reference before-image = %q, exists=%t", before, exists)
	}
	if err := RecoverFileSet4(t.Context(), stateDir, []string{manifest}); err != nil {
		t.Fatalf("RecoverFileSet4: %v", err)
	}
	assertContent(t, manifest, "proposed")

	second := filepath.Join(root, "second-manifest")
	if err := CommitFileSet4WithReference(t.Context(), FileSetInput{
		StateDir: filepath.Join(root, ".other-daem"),
		Targets:  []FileTarget{mustCommitPointTarget(t, second, "committed")},
	}); err != nil {
		t.Fatalf("CommitFileSet4WithReference: %v", err)
	}
	assertContent(t, second, "committed")
	assertMissing(t, transactionDir(filepath.Join(root, ".other-daem")))
}

func TestReferenceFileSet4RollbackUsesExistingFenceAndDoesNotRetry(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	source := filepath.Join(root, "source")
	manifest := filepath.Join(root, "manifest")
	writeFixture(t, source, "source-before", 0o600)
	writeFixture(t, manifest, "manifest-before", 0o600)
	var writes []string
	ordinary := mustWriteTarget(t, source, "source-after")
	commitPoint := mustCommitPointTarget(t, manifest, "manifest-after")
	assertPanics(t, func() {
		_ = commitWithOperationsVersion(t.Context(), FileSetInput{
			StateDir: stateDir,
			Targets:  []FileTarget{ordinary, commitPoint},
		}, operations{writeFile: func(ctx context.Context, path string, content []byte, mode os.FileMode) error {
			writes = append(writes, path)
			if err := commitFile(ctx, path, content, mode); err != nil {
				return err
			}
			if path == ordinary.Path() {
				panic("interrupted before reference commit point")
			}
			return nil
		}}, contractversion.MetadataTransactionReference, true)
	})

	work, err := MaximumFenceCensusWork4(stateDir, mutationfs.MaximumPhysicalPathDepth)
	if err != nil {
		t.Fatalf("MaximumFenceCensusWork4: %v", err)
	}
	if work.Bytes < maximumMarkerBytes+maximumStagedBeforeImageBytes || work.Entries < maximumFileSetTargets+1 {
		t.Fatalf("generation-4 fence work = %+v, does not cover evidence images", work)
	}
	physicalWorkBudget := &fenceObservationBudget{}
	fenceErr := ObserveClearFenceAt4(
		t.Context(),
		stateDir,
		mutationfs.MaximumPhysicalPathDepth,
		physicalWorkBudget,
	)
	if !errors.Is(fenceErr, ErrInterruptedFileSetTransaction) ||
		FileSetFenceKindOf(fenceErr) != FileSetFencePublishedTransaction {
		t.Fatalf("ObserveClearFenceAt4 = %v, want typed interrupted transaction", fenceErr)
	}
	if physicalWorkBudget.paths > work.PathComponents ||
		physicalWorkBudget.entries > work.Entries ||
		physicalWorkBudget.bytes > work.Bytes {
		t.Fatalf("generation-4 fence used more than its reservation: observed=%+v reserved=%+v", physicalWorkBudget, work)
	}
	if disposition, err := InspectFileSetRecovery4(t.Context(), stateDir, []string{source, manifest}); err != nil || disposition != RecoveryRollback {
		t.Fatalf("InspectFileSetRecovery4 = %q, %v; want rollback", disposition, err)
	}
	if err := RecoverFileSet4(t.Context(), stateDir, []string{source, manifest}); err != nil {
		t.Fatalf("RecoverFileSet4: %v", err)
	}
	assertContent(t, source, "source-before")
	assertContent(t, manifest, "manifest-before")
	assertMissing(t, transactionDir(stateDir))
	if len(writes) != 1 || writes[0] != ordinary.Path() {
		t.Fatalf("recovery retried original operation: writes = %v", writes)
	}
}

func TestFileSet4MarkerDepthChargeRejectsDeepRecordWithoutResolvingTarget(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	markerPathDepth, err := fileSetAbsolutePathWork(markerPath(stateDir), mutationfs.MaximumPhysicalPathDepth)
	if err != nil {
		t.Fatal(err)
	}
	deepTarget := root
	for range markerPathDepth + 1 {
		deepTarget = filepath.Join(deepTarget, "segment")
	}
	deepTarget = filepath.Join(deepTarget, "target")
	after := []byte("proposed")
	writeReferenceMarkerFixture(t, stateDir, []targetMarker{{
		Path:        deepTarget,
		Before:      fileState{},
		AfterHash:   hashBytes(after),
		Write:       true,
		CommitPoint: true,
	}}, deepTarget, after)

	budget := &fenceObservationBudget{}
	_, _, evidence, err := loadMarker4WithBudget(
		t.Context(),
		markerPath(stateDir),
		markerPathDepth,
		budget,
	)
	if err == nil || evidence != nil {
		t.Fatal("over-depth persisted target produced usable evidence")
	}
	if budget.paths != markerPathDepth || budget.entries != 1 || budget.bytes != maximumMarkerBytes {
		t.Fatalf("work after depth refusal = %+v; only marker work should have been admitted", budget)
	}
}

func TestFileSet4AllowsValidDeepExternalTargetWithShallowStateDir(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	deepParent := root
	for range 48 {
		deepParent = filepath.Join(deepParent, "d")
	}
	if err := os.MkdirAll(deepParent, 0o700); err != nil {
		t.Fatal(err)
	}
	deepTarget := filepath.Join(deepParent, "source")
	manifest := filepath.Join(root, "manifest")
	writeFixture(t, deepTarget, "source-before", 0o600)
	writeFixture(t, manifest, "manifest-before", 0o600)

	stateDepth, err := fileSetAbsolutePathWork(stateDir, mutationfs.MaximumPhysicalPathDepth)
	if err != nil {
		t.Fatal(err)
	}
	targetDepth, err := fileSetAbsolutePathWork(deepTarget, mutationfs.MaximumPhysicalPathDepth)
	if err != nil {
		t.Fatal(err)
	}
	if targetDepth <= stateDepth {
		t.Fatalf("external target depth %d is not deeper than StateDir depth %d", targetDepth, stateDepth)
	}

	if _, err := prepareMarkerVersion(t.Context(), stateDir, []FileTarget{
		mustWriteTarget(t, deepTarget, "source-after"),
		mustCommitPointTarget(t, manifest, "manifest-after"),
	}, contractversion.MetadataTransactionReference, true); err != nil {
		t.Fatalf("prepare deep external target marker: %v", err)
	}
	disposition, err := InspectFileSetRecovery4(t.Context(), stateDir, []string{deepTarget, manifest})
	if err != nil || disposition != RecoveryRollback {
		t.Fatalf("InspectFileSetRecovery4 = %q, %v; want rollback for valid deep external target", disposition, err)
	}
	assertContent(t, deepTarget, "source-before")
	assertContent(t, manifest, "manifest-before")
}

func TestFileSet4StaticAndReferenceWritersRejectOverdepthTargetsBeforePublication(t *testing.T) {
	for _, reference := range []bool{false, true} {
		name := "static"
		if reference {
			name = "reference"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
			targetPath := deepExternalTarget(t, root, mutationfs.MaximumPhysicalPathDepth)
			writeFixture(t, targetPath, "before", 0o600)
			target := mustWriteTarget(t, targetPath, "after")
			if reference {
				target = mustCommitPointTarget(t, targetPath, "after")
			}
			if _, err := fileSetAbsolutePathWork(target.Path(), mutationfs.MaximumPhysicalPathDepth); err == nil {
				t.Fatal("target fixture is within the selected physical path depth")
			}

			assertV4WriterRefusesBeforePublication(t, stateDir, target, reference)
		})
	}
}

func TestFileSet4WriterRejectsStateDirsOutsideMarkerDepthBeforePublication(t *testing.T) {
	for _, depth := range []int{
		mutationfs.MaximumPhysicalPathDepth - 1,
		mutationfs.MaximumPhysicalPathDepth + 1,
	} {
		t.Run(fmt.Sprintf("state-depth-%d", depth), func(t *testing.T) {
			root := t.TempDir()
			stateDir := deepDirectoryAtDepth(t, root, depth)
			targetPath := filepath.Join(root, "manifest")
			writeFixture(t, targetPath, "before", 0o600)
			target := mustCommitPointTarget(t, targetPath, "after")
			actualDepth, depthErr := fileSetAbsolutePathWork(stateDir, mutationfs.MaximumPhysicalPathDepth)
			if depth < mutationfs.MaximumPhysicalPathDepth && (depthErr != nil || actualDepth != depth) {
				t.Fatalf("StateDir fixture depth = %d, %v; want %d", actualDepth, depthErr, depth)
			} else if depth > mutationfs.MaximumPhysicalPathDepth && depthErr == nil {
				t.Fatal("StateDir fixture is within the selected physical path depth")
			}
			assertV4WriterRefusesBeforePublication(t, stateDir, target, true)
		})
	}
}

func TestFileSet4StaticAndReferenceWritersAdmitDeepPathsBelowLimit(t *testing.T) {
	for _, reference := range []bool{false, true} {
		name := "static"
		if reference {
			name = "reference"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
			targetPath := deepExternalTarget(t, root, 200)
			writeFixture(t, targetPath, "before", 0o600)
			target := mustWriteTarget(t, targetPath, "after")
			if reference {
				target = mustCommitPointTarget(t, targetPath, "after")
			}

			if _, err := fileSetAbsolutePathWork(target.Path(), mutationfs.MaximumPhysicalPathDepth); err != nil {
				t.Fatalf("control target exceeds the selected path depth: %v", err)
			}
			assertPanics(t, func() {
				_ = commitWithOperationsVersion(t.Context(), FileSetInput{
					StateDir: stateDir,
					Targets:  []FileTarget{target},
				}, operations{writeFile: func(context.Context, string, []byte, os.FileMode) error {
					panic("interrupt-after-marker")
				}}, contractversion.MetadataTransactionReference, reference)
			})
			if _, err := os.Lstat(markerPath(stateDir)); err != nil {
				t.Fatalf("bounded writer did not publish its transaction marker: %v", err)
			}
			if reference {
				view, err := ReadFileSetReferenceEvidence4(t.Context(), stateDir, target.Path())
				if err != nil || string(view.AfterImage()) != "after" {
					t.Fatalf("deep reference view = %q, %v", view.AfterImage(), err)
				}
			}
			disposition, err := InspectFileSetRecovery4(t.Context(), stateDir, []string{target.Path()})
			if err != nil || disposition != RecoveryRollback {
				t.Fatalf("deep target recovery preview = %q, %v; want rollback", disposition, err)
			}
			if err := RecoverFileSet4(t.Context(), stateDir, []string{target.Path()}); err != nil {
				t.Fatalf("recover bounded deep target: %v", err)
			}
			assertContent(t, targetPath, "before")
			assertMissing(t, transactionDir(stateDir))
		})
	}
}

func TestFileSet4FenceAndViewDoNotResolveUnselectedPersistedTargets(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	realDir := filepath.Join(root, "real")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	aliasDir := filepath.Join(root, "alias")
	if err := os.Symlink(realDir, aliasDir); err != nil {
		t.Fatal(err)
	}
	ordinaryPath := filepath.Join(aliasDir, "source")
	commitPoint := mustCommitPointTarget(t, filepath.Join(root, "manifest"), "manifest-proposed")
	after := []byte("manifest-proposed")
	writeReferenceMarkerFixture(t, stateDir, []targetMarker{
		{
			Path:      ordinaryPath,
			Before:    fileState{},
			AfterHash: hashBytes([]byte("source-proposed")),
			Write:     true,
		},
		{
			Path:        commitPoint.Path(),
			Before:      fileState{},
			AfterHash:   hashBytes(after),
			Write:       true,
			CommitPoint: true,
		},
	}, commitPoint.Path(), after)

	view, err := ReadFileSetReferenceEvidence4(t.Context(), stateDir, commitPoint.Path())
	if err != nil {
		t.Fatalf("read-only reference view resolved an unselected persisted path: %v", err)
	}
	if view.ReferenceNamespace() != commitPoint.Path() || !bytes.Equal(view.AfterImage(), after) {
		t.Fatal("read-only view did not retain the selected reference evidence")
	}

	fenceErr := ObserveClearFenceAt4(
		t.Context(),
		stateDir,
		mutationfs.MaximumPhysicalPathDepth,
		&fenceObservationBudget{},
	)
	if !errors.Is(fenceErr, ErrInterruptedFileSetTransaction) ||
		FileSetFenceKindOf(fenceErr) != FileSetFencePublishedTransaction {
		t.Fatalf("fence = %v, want a pending transaction without resolving its external targets", fenceErr)
	}
}

func TestFileSet4RecoveryRequiresBoundedIndependentExactTargetSet(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	realDir := filepath.Join(root, "real")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	aliasDir := filepath.Join(root, "alias")
	if err := os.Symlink(realDir, aliasDir); err != nil {
		t.Fatal(err)
	}
	ordinaryPath := filepath.Join(aliasDir, "source")
	resolvedSource := filepath.Join(realDir, "source")
	writeFixture(t, resolvedSource, "untouched-source", 0o600)
	commitPoint := mustCommitPointTarget(t, filepath.Join(root, "manifest"), "manifest-proposed")
	writeFixture(t, commitPoint.Path(), "untouched-manifest", 0o600)
	writeReferenceMarkerFixture(t, stateDir, []targetMarker{
		{
			Path:      ordinaryPath,
			Before:    fileState{},
			AfterHash: hashBytes([]byte("source-proposed")),
			Write:     true,
		},
		{
			Path:        commitPoint.Path(),
			Before:      fileState{},
			AfterHash:   hashBytes([]byte("manifest-proposed")),
			Write:       true,
			CommitPoint: true,
		},
	}, commitPoint.Path(), []byte("manifest-proposed"))

	wrongSet := []string{ordinaryPath, commitPoint.Path()}
	for _, inspect := range []bool{true, false} {
		if inspect {
			_, err := InspectFileSetRecovery4(t.Context(), stateDir, wrongSet)
			assertRecoveryAuthorityRefusal(t, err, ordinaryPath)
		} else {
			err := RecoverFileSet4(t.Context(), stateDir, wrongSet)
			assertRecoveryAuthorityRefusal(t, err, ordinaryPath)
		}
		assertContent(t, resolvedSource, "untouched-source")
		assertContent(t, commitPoint.Path(), "untouched-manifest")
		if _, err := os.Stat(markerPath(stateDir)); err != nil {
			t.Fatalf("authority refusal removed the active marker: %v", err)
		}
	}

	differentSet := []string{filepath.Join(root, "other-source"), commitPoint.Path()}
	assertRecoveryAuthorityRefusal(t, RecoverFileSet4(t.Context(), stateDir, differentSet), ordinaryPath)
	assertContent(t, resolvedSource, "untouched-source")
	assertContent(t, commitPoint.Path(), "untouched-manifest")
}

func TestFileSet4MaximumFenceReservationCoversTargetsAndImages(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	targets := make([]FileTarget, 0, maximumFileSetTargets)
	for index := range maximumFileSetTargets {
		path := filepath.Join(root, fmt.Sprintf("target-%02d", index))
		writeFixture(t, path, fmt.Sprintf("before-%d", index), 0o600)
		if index == maximumFileSetTargets-1 {
			targets = append(targets, mustCommitPointTarget(t, path, "proposed"))
		} else {
			targets = append(targets, mustWriteTarget(t, path, fmt.Sprintf("after-%d", index)))
		}
	}
	if _, err := prepareMarkerVersion(
		t.Context(),
		stateDir,
		targets,
		contractversion.MetadataTransactionReference,
		true,
	); err != nil {
		t.Fatalf("prepare maximum-target marker: %v", err)
	}
	work, err := MaximumFenceCensusWork4(stateDir, mutationfs.MaximumPhysicalPathDepth)
	if err != nil {
		t.Fatalf("MaximumFenceCensusWork4: %v", err)
	}
	budget := &limitedPhysicalWorkBudget{maximum: work}
	fenceErr := ObserveClearFenceAt4(t.Context(), stateDir, mutationfs.MaximumPhysicalPathDepth, budget)
	if !errors.Is(fenceErr, ErrInterruptedFileSetTransaction) ||
		FileSetFenceKindOf(fenceErr) != FileSetFencePublishedTransaction {
		t.Fatalf("maximum-target fence = %v, want interrupted transaction within its reservation", fenceErr)
	}
	if budget.paths > work.PathComponents || budget.entries > work.Entries || budget.bytes > work.Bytes {
		t.Fatalf("fence work exceeded exact reservation: observed=%+v reserved=%+v", budget, work)
	}
	for index := range maximumFileSetTargets {
		assertContent(t, filepath.Join(root, fmt.Sprintf("target-%02d", index)), fmt.Sprintf("before-%d", index))
	}
}

func TestReferenceFileSet4FinalizeDoesNotRepeatOperation(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	source := filepath.Join(root, "source")
	manifest := filepath.Join(root, "manifest")
	writeFixture(t, source, "source-before", 0o600)
	writeFixture(t, manifest, "manifest-before", 0o600)
	writes := 0
	ordinary := mustWriteTarget(t, source, "source-after")
	commitPoint := mustCommitPointTarget(t, manifest, "manifest-after")
	assertPanics(t, func() {
		_ = commitWithOperationsVersion(t.Context(), FileSetInput{
			StateDir: stateDir,
			Targets:  []FileTarget{ordinary, commitPoint},
		}, operations{writeFile: func(ctx context.Context, path string, content []byte, mode os.FileMode) error {
			writes++
			if err := commitFile(ctx, path, content, mode); err != nil {
				return err
			}
			if path == commitPoint.Path() {
				panic("interrupted after reference commit point")
			}
			return nil
		}}, contractversion.MetadataTransactionReference, true)
	})
	if disposition, err := InspectFileSetRecovery4(t.Context(), stateDir, []string{source, manifest}); err != nil || disposition != RecoveryFinalize {
		t.Fatalf("InspectFileSetRecovery4 = %q, %v; want finalize", disposition, err)
	}
	if err := RecoverFileSet4(t.Context(), stateDir, []string{source, manifest}); err != nil {
		t.Fatalf("RecoverFileSet4: %v", err)
	}
	assertContent(t, source, "source-after")
	assertContent(t, manifest, "manifest-after")
	assertMissing(t, transactionDir(stateDir))
	if writes != 2 {
		t.Fatalf("recovery repeated operation: write calls = %d, want 2", writes)
	}
}

func TestFileSet4VersionEnvelopeRefusesBeforeNestedDecode(t *testing.T) {
	for _, test := range []struct {
		version int
		want    string
	}{
		{version: contractversion.MetadataTransaction, want: "use the daem version that wrote it"},
		{version: contractversion.MetadataTransactionReference + 1, want: "newer daem"},
	} {
		t.Run(fmt.Sprintf("version-%d", test.version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), transactionMarkerFile)
			content := fmt.Sprintf(`{"version":%d,"targets":false,"unknown":true}`, test.version)
			writeFixture(t, path, content, transactionEvidenceMode)
			_, _, _, err := loadMarker4(t.Context(), path)
			if err == nil || !strings.Contains(err.Error(), test.want) || strings.Contains(err.Error(), "unknown field") {
				t.Fatalf("loadMarker4 error = %v, want envelope guidance before nested decode", err)
			}
		})
	}
}

func TestReferenceFileSet4RejectsMalformedMarkerFields(t *testing.T) {
	tests := []struct {
		name string
		edit func(t *testing.T, root map[string]any, encoded string) string
		want string
	}{
		{name: "null reference", edit: func(t *testing.T, root map[string]any, encoded string) string {
			root["reference"] = nil
			return marshalTestJSON(t, root)
		}, want: `field "reference" must not be null`},
		{name: "missing reference target", edit: func(t *testing.T, root map[string]any, encoded string) string {
			delete(root["reference"].(map[string]any), "target")
			return marshalTestJSON(t, root)
		}, want: `reference field "target" is required`},
		{name: "null reference target", edit: func(t *testing.T, root map[string]any, encoded string) string {
			root["reference"].(map[string]any)["target"] = nil
			return marshalTestJSON(t, root)
		}, want: `reference field "target" is required`},
		{name: "missing after image", edit: func(t *testing.T, root map[string]any, encoded string) string {
			delete(root["reference"].(map[string]any), "after_image")
			return marshalTestJSON(t, root)
		}, want: `reference field "after_image" is required`},
		{name: "null after image", edit: func(t *testing.T, root map[string]any, encoded string) string {
			root["reference"].(map[string]any)["after_image"] = nil
			return marshalTestJSON(t, root)
		}, want: `reference field "after_image" is required`},
		{name: "missing artifact path", edit: func(t *testing.T, root map[string]any, encoded string) string {
			delete(root["reference"].(map[string]any)["after_image"].(map[string]any), "path")
			return marshalTestJSON(t, root)
		}, want: `reference.after_image field "path" is required`},
		{name: "missing artifact size", edit: func(t *testing.T, root map[string]any, encoded string) string {
			delete(root["reference"].(map[string]any)["after_image"].(map[string]any), "size")
			return marshalTestJSON(t, root)
		}, want: `reference.after_image field "size" is required`},
		{name: "missing artifact digest", edit: func(t *testing.T, root map[string]any, encoded string) string {
			delete(root["reference"].(map[string]any)["after_image"].(map[string]any), "digest")
			return marshalTestJSON(t, root)
		}, want: `reference.after_image field "digest" is required`},
		{name: "null artifact size", edit: func(t *testing.T, root map[string]any, encoded string) string {
			root["reference"].(map[string]any)["after_image"].(map[string]any)["size"] = nil
			return marshalTestJSON(t, root)
		}, want: `reference.after_image field "size" is required`},
		{name: "digest mismatch", edit: func(t *testing.T, root map[string]any, encoded string) string {
			root["reference"].(map[string]any)["after_image"].(map[string]any)["digest"] = hashBytes([]byte("different"))
			return marshalTestJSON(t, root)
		}, want: "does not match target after hash"},
		{name: "unknown nested field", edit: func(t *testing.T, root map[string]any, encoded string) string {
			root["reference"].(map[string]any)["other"] = true
			return marshalTestJSON(t, root)
		}, want: "unknown field"},
		{name: "unknown root field", edit: func(t *testing.T, root map[string]any, encoded string) string {
			root["unexpected"] = true
			return marshalTestJSON(t, root)
		}, want: "unknown field"},
		{name: "duplicate target", edit: func(t *testing.T, root map[string]any, encoded string) string {
			targets := root["targets"].([]any)
			root["targets"] = append(targets, targets[0])
			return marshalTestJSON(t, root)
		}, want: "targets must be unique"},
		{name: "artifact traversal", edit: func(t *testing.T, root map[string]any, encoded string) string {
			root["reference"].(map[string]any)["after_image"].(map[string]any)["path"] = "../outside"
			return marshalTestJSON(t, root)
		}, want: "engine-owned artifact"},
		{name: "retarget to ordinary target", edit: func(t *testing.T, root map[string]any, encoded string) string {
			root["reference"].(map[string]any)["target"] = root["targets"].([]any)[0].(map[string]any)["path"]
			return marshalTestJSON(t, root)
		}},
		{name: "duplicate JSON key", edit: func(t *testing.T, root map[string]any, encoded string) string {
			last := strings.LastIndex(encoded, "}")
			return encoded[:last] + `,"reference":null}`
		}, want: "duplicate object key"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, stateDir, _, manifest := interruptedReferenceFixture(t, false)
			path := markerPath(stateDir)
			content := mustReadFile(t, path)
			var root map[string]any
			if err := json.Unmarshal(content, &root); err != nil {
				t.Fatal(err)
			}
			encoded := test.edit(t, root, string(content))
			if err := os.WriteFile(path, []byte(encoded), transactionEvidenceMode); err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := loadMarker4(t.Context(), path); err == nil ||
				(test.want != "" && !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("loadMarker4 error = %v, want %q", err, test.want)
			}
			if test.name == "duplicate JSON key" {
				if _, err := ReadFileSetReferenceEvidence4(t.Context(), stateDir, manifest); err == nil {
					t.Fatal("malformed marker returned a usable evidence view")
				}
			}
		})
	}
}

func TestReadFileSetReferenceEvidence4RejectsStaticMarker(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	path := filepath.Join(root, "static")
	writeFixture(t, path, "before", 0o600)
	assertPanics(t, func() {
		_ = commitWithOperationsVersion(t.Context(), FileSetInput{
			StateDir: stateDir,
			Targets:  []FileTarget{mustWriteTarget(t, path, "after")},
		}, operations{writeFile: func(ctx context.Context, path string, content []byte, mode os.FileMode) error {
			if err := commitFile(ctx, path, content, mode); err != nil {
				return err
			}
			panic("simulated static transaction interruption")
		}}, contractversion.MetadataTransactionReference, false)
	})
	if view, err := ReadFileSetReferenceEvidence4(t.Context(), stateDir, path); err == nil ||
		!errors.Is(err, ErrFileSetEvidenceInvalid) ||
		!strings.Contains(err.Error(), "has no reference evidence") {
		t.Fatalf("ReadFileSetReferenceEvidence4 = %+v, %v; want static-marker refusal", view, err)
	}
}

func TestReferenceFileSet4RejectsBrokenPrivateAfterImage(t *testing.T) {
	tests := []struct {
		name string
		edit func(t *testing.T, artifact string)
		want string
	}{
		{name: "missing", edit: func(t *testing.T, artifact string) {
			if err := os.Remove(artifact); err != nil {
				t.Fatal(err)
			}
		}, want: "inspect reference after-image"},
		{name: "corrupt", edit: func(t *testing.T, artifact string) {
			writeFixture(t, artifact, strings.Repeat("x", len("manifest-after")), transactionEvidenceMode)
		}, want: "digest"},
		{name: "oversized", edit: func(t *testing.T, artifact string) {
			file, err := os.OpenFile(artifact, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err := file.Truncate(maximumTargetBytes + 1); err != nil {
				_ = file.Close()
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
		}, want: "maximum"},
		{name: "wrong mode", edit: func(t *testing.T, artifact string) {
			if err := os.Chmod(artifact, 0o644); err != nil {
				t.Fatal(err)
			}
		}, want: "permissions"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, stateDir, _, manifest := interruptedReferenceFixture(t, false)
			artifact := filepath.Join(transactionDir(stateDir), referenceAfterImageName)
			test.edit(t, artifact)
			view, err := ReadFileSetReferenceEvidence4(t.Context(), stateDir, manifest)
			if err == nil || !errors.Is(err, ErrFileSetEvidenceInvalid) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ReadFileSetReferenceEvidence4 error = %v, want %q", err, test.want)
			}
			if view.ReferenceNamespace() != "" || len(view.AfterImage()) != 0 {
				t.Fatal("failed reconstruction returned a usable partial evidence view")
			}
		})
	}
}

func TestFileSet4RejectsEvidenceBudgetOverflowBeforePublication(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	targets := make([]FileTarget, 0, 4)
	for index := range 4 {
		path := filepath.Join(root, fmt.Sprintf("target-%d", index))
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(maximumTargetBytes); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		var target FileTarget
		if index == 3 {
			target = mustCommitPointTarget(t, path, "x")
		} else {
			target = mustWriteTarget(t, path, "x")
		}
		targets = append(targets, target)
	}
	err := CommitFileSet4WithReference(t.Context(), FileSetInput{StateDir: stateDir, Targets: targets})
	if err == nil || !strings.Contains(err.Error(), "maximum") {
		t.Fatalf("CommitFileSet4WithReference error = %v, want aggregate evidence budget refusal", err)
	}
	assertMissing(t, transactionDir(stateDir))
	for index := range 4 {
		path := filepath.Join(root, fmt.Sprintf("target-%d", index))
		info, err := os.Stat(path)
		if err != nil || info.Size() != maximumTargetBytes {
			t.Fatalf("target %s changed before evidence publication: info=%v err=%v", path, info, err)
		}
	}
}

func TestFileSet4RejectsEvidenceBudgetOverflowDuringReconstruction(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	evidenceDir := transactionDir(stateDir)
	if err := os.MkdirAll(evidenceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	targets := make([]targetMarker, 0, 4)
	for index := range 4 {
		path := mustWriteTarget(t, filepath.Join(root, fmt.Sprintf("target-%d", index)), "after").Path()
		backupPath := filepath.Join(evidenceDir, fmt.Sprintf("target-%03d.before", index))
		backup, err := os.Create(backupPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := backup.Truncate(maximumTargetBytes); err != nil {
			_ = backup.Close()
			t.Fatal(err)
		}
		if err := backup.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(backupPath, transactionEvidenceMode); err != nil {
			t.Fatal(err)
		}
		targets = append(targets, targetMarker{
			Path: path,
			Before: fileState{
				Exists:     true,
				Hash:       hashBytes([]byte("before")),
				BackupPath: backupPath,
				Mode:       0o600,
			},
			AfterHash:   hashBytes([]byte("after")),
			Write:       true,
			CommitPoint: index == 3,
		})
	}
	afterImage := []byte("after")
	writeFixture(t, filepath.Join(evidenceDir, referenceAfterImageName), string(afterImage), transactionEvidenceMode)
	marker := transactionMarker{
		Version: contractversion.MetadataTransactionReference,
		Targets: targets,
		Reference: &referenceEvidenceMarker{
			Target: targets[3].Path,
			AfterImage: referenceAfterImageMarker{
				Path:   referenceAfterImageName,
				Size:   int64(len(afterImage)),
				Digest: hashBytes(afterImage),
			},
		},
	}
	markerContent, err := marshalMarker(marker)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(markerPath(stateDir), markerContent, transactionEvidenceMode); err != nil {
		t.Fatal(err)
	}

	view, err := ReadFileSetReferenceEvidence4(t.Context(), stateDir, targets[3].Path)
	if err == nil || !strings.Contains(err.Error(), "maximum") {
		t.Fatalf("ReadFileSetReferenceEvidence4 error = %v, want aggregate evidence budget refusal", err)
	}
	if view.ReferenceNamespace() != "" || len(view.AfterImage()) != 0 {
		t.Fatal("over-budget reconstruction returned a usable partial evidence view")
	}
}

func TestFileSet4RejectsReconstructionBudgetOverflowBeforeReadingImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "one-byte-image")
	writeFixture(t, path, "x", transactionEvidenceMode)
	_, _, err := readEvidenceImageWithBudget(
		t.Context(),
		path,
		"reference after-image",
		maximumStagedBeforeImageBytes,
		hashBytes([]byte("x")),
		1,
		mutationfs.MaximumPhysicalPathDepth,
		&fenceObservationBudget{},
	)
	if err == nil || !strings.Contains(err.Error(), "maximum") {
		t.Fatalf("readEvidenceImageWithBudget error = %v, want aggregate evidence budget refusal", err)
	}
}

func TestFileSet4BoundsReadAfterEvidenceGrowsAtByteAdmission(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	referencePath := filepath.Join(root, "reference")
	target := mustCommitPointTarget(t, referencePath, "proposed")
	marker, err := prepareMarkerVersion(
		t.Context(),
		stateDir,
		[]FileTarget{target},
		contractversion.MetadataTransactionReference,
		true,
	)
	if err != nil {
		t.Fatalf("prepare reference evidence: %v", err)
	}
	afterImagePath := filepath.Join(transactionDir(stateDir), referenceAfterImageName)
	grown := []byte("proposed-after-growth")
	budget := &mutateOnByteAdmissionBudget{
		mutate: func() error {
			return os.WriteFile(afterImagePath, grown, transactionEvidenceMode)
		},
	}

	evidence, err := reconstructReferenceEvidenceWithBudget(
		t.Context(),
		markerPath(stateDir),
		marker,
		mutationfs.MaximumPhysicalPathDepth,
		budget,
	)
	if evidence != nil {
		t.Fatal("changed evidence returned a usable reconstruction")
	}
	var readLimitError *storagecommit.RegularFileReadLimitError
	if !errors.Is(err, storagecommit.ErrRegularFileReadLimitExceeded) ||
		!errors.As(err, &readLimitError) {
		t.Fatalf("reconstruction error = %v, want bounded read refusal", err)
	}
	if readLimitError.Limit() != int64(len("proposed")) ||
		readLimitError.Observed() != int64(len(grown)) {
		t.Fatalf(
			"read limit = %d, observed = %d; want %d and %d",
			readLimitError.Limit(),
			readLimitError.Observed(),
			len("proposed"),
			len(grown),
		)
	}
	if !budget.mutated {
		t.Fatal("physical-work budget did not grow the image after byte admission")
	}
}

func TestFileSet4RequiresReferenceCommitPoint(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	path := filepath.Join(root, "ordinary")
	err := CommitFileSet4WithReference(t.Context(), FileSetInput{
		StateDir: stateDir,
		Targets:  []FileTarget{mustWriteTarget(t, path, "after")},
	})
	if err == nil || !strings.Contains(err.Error(), "exactly one commit-point") {
		t.Fatalf("CommitFileSet4WithReference error = %v, want commit-point requirement", err)
	}
	assertMissing(t, path)
	assertMissing(t, transactionDir(stateDir))
}

func interruptedReferenceFixture(t *testing.T, referenceBeforeAbsent bool) (string, string, string, string) {
	t.Helper()
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	source := filepath.Join(root, "source")
	manifest := filepath.Join(root, "manifest")
	writeFixture(t, source, "source-before", 0o600)
	if !referenceBeforeAbsent {
		writeFixture(t, manifest, "manifest-before", 0o600)
	}
	ordinary := mustWriteTarget(t, source, "source-after")
	commitPoint := mustCommitPointTarget(t, manifest, "manifest-after")
	assertPanics(t, func() {
		_ = commitWithOperationsVersion(t.Context(), FileSetInput{
			StateDir: stateDir,
			Targets:  []FileTarget{ordinary, commitPoint},
		}, operations{writeFile: func(ctx context.Context, path string, content []byte, mode os.FileMode) error {
			if err := commitFile(ctx, path, content, mode); err != nil {
				return err
			}
			if path == ordinary.Path() {
				panic("simulated interruption before reference target")
			}
			return nil
		}}, contractversion.MetadataTransactionReference, true)
	})
	return root, stateDir, source, manifest
}

func writeReferenceMarkerFixture(
	t *testing.T,
	stateDir string,
	targets []targetMarker,
	referenceTarget string,
	afterImage []byte,
) {
	t.Helper()
	if err := os.MkdirAll(transactionDir(stateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(transactionDir(stateDir), referenceAfterImageName),
		afterImage,
		transactionEvidenceMode,
	); err != nil {
		t.Fatal(err)
	}
	marker := transactionMarker{
		Version: contractversion.MetadataTransactionReference,
		Targets: targets,
		Reference: &referenceEvidenceMarker{
			Target: referenceTarget,
			AfterImage: referenceAfterImageMarker{
				Path:   referenceAfterImageName,
				Size:   int64(len(afterImage)),
				Digest: hashBytes(afterImage),
			},
		},
	}
	content, err := marshalMarker(marker)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(markerPath(stateDir), content, transactionEvidenceMode); err != nil {
		t.Fatal(err)
	}
}

func deepExternalTarget(t *testing.T, root string, directoryComponents int) string {
	t.Helper()
	parent := root
	for range directoryComponents {
		parent = filepath.Join(parent, "d")
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(parent, "target")
}

func deepDirectoryAtDepth(t *testing.T, root string, targetDepth int) string {
	t.Helper()
	currentDepth, err := fileSetAbsolutePathWork(root, mutationfs.MaximumPhysicalPathDepth)
	if err != nil {
		t.Fatal(err)
	}
	if targetDepth < currentDepth {
		t.Fatalf("requested directory depth %d is shallower than root depth %d", targetDepth, currentDepth)
	}
	directory := root
	for currentDepth < targetDepth {
		directory = filepath.Join(directory, "d")
		currentDepth++
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	return directory
}

func assertV4WriterRefusesBeforePublication(t *testing.T, stateDir string, target FileTarget, reference bool) {
	t.Helper()
	var err error
	if reference {
		err = CommitFileSet4WithReference(t.Context(), FileSetInput{
			StateDir: stateDir,
			Targets:  []FileTarget{target},
		})
	} else {
		err = CommitFileSet4(t.Context(), FileSetInput{
			StateDir: stateDir,
			Targets:  []FileTarget{target},
		})
	}
	if err == nil {
		t.Fatal("writer admitted a path the generation-4 reader cannot admit")
	}
	assertMissing(t, markerPath(stateDir))
	assertMissing(t, transactionDir(stateDir))
	assertContent(t, target.Path(), "before")
}

func assertRecoveryAuthorityRefusal(t *testing.T, err error, path string) {
	t.Helper()
	var authorityError RecoveryAuthorityError
	if !errors.As(err, &authorityError) || authorityError.Path != path {
		t.Fatalf("recovery error = %v, want authority refusal for %q", err, path)
	}
}

func assertPanics(t *testing.T, run func()) {
	t.Helper()
	deferred := false
	func() {
		defer func() { deferred = recover() != nil }()
		run()
	}()
	if !deferred {
		t.Fatal("operation did not simulate an interruption")
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func marshalTestJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

type mutateOnByteAdmissionBudget struct {
	fenceObservationBudget
	mutate  func() error
	mutated bool
}

type limitedPhysicalWorkBudget struct {
	maximum FenceCensusWork
	paths   int
	entries int
	bytes   int64
}

func (budget *limitedPhysicalWorkBudget) AdmitPathComponents(count int) error {
	return budget.AdmitPhysicalWork(count, 0, 0)
}

func (budget *limitedPhysicalWorkBudget) AdmitPhysicalWork(
	pathComponents int,
	entries int,
	bytes int64,
) error {
	if pathComponents < 0 || entries < 0 || bytes < 0 ||
		pathComponents > budget.maximum.PathComponents-budget.paths ||
		entries > budget.maximum.Entries-budget.entries ||
		bytes > budget.maximum.Bytes-budget.bytes {
		return fmt.Errorf("reserved file-set work exhausted")
	}
	budget.paths += pathComponents
	budget.entries += entries
	budget.bytes += bytes
	return nil
}

func (budget *mutateOnByteAdmissionBudget) AdmitPathComponents(count int) error {
	return budget.AdmitPhysicalWork(count, 0, 0)
}

func (budget *mutateOnByteAdmissionBudget) AdmitPhysicalWork(
	pathComponents int,
	entries int,
	bytes int64,
) error {
	if err := budget.fenceObservationBudget.AdmitPhysicalWork(pathComponents, entries, bytes); err != nil {
		return err
	}
	if bytes > 0 && !budget.mutated {
		budget.mutated = true
		if budget.mutate != nil {
			return budget.mutate()
		}
	}
	return nil
}
