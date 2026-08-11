//go:build darwin || linux

package journal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/supply/artifact"
)

func TestRecoveryBackupObservationChargesExactFileWork(t *testing.T) {
	root := t.TempDir()
	backupPath := filepath.Join(root, "backup")
	content := []byte("abc")
	if err := os.WriteFile(backupPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	budget := recoveryBackupBudgetForTest(t)
	before := budget.RemainingTreeWork()

	observation, err := observeRecoveryBackupPathForTest(
		t,
		t.Context(),
		"backups/file",
		backupPath,
		budget,
	)
	if err != nil {
		t.Fatalf("observe recovery file backup: %v", err)
	}
	if observation.Error != "" || observation.Kind != recovery.PathKindFile {
		t.Fatalf("file backup observation = %#v", observation)
	}
	if observation.ContentHash != string(artifact.HashFileContent(content)) {
		t.Fatalf("file backup hash = %q", observation.ContentHash)
	}
	if observation.Work.Entries() != 0 || observation.Work.Bytes() != int64(len(content)) {
		t.Fatalf(
			"file backup evidence work = entries:%d bytes:%d, want 0/%d",
			observation.Work.Entries(),
			observation.Work.Bytes(),
			len(content),
		)
	}
	after := budget.RemainingTreeWork()
	if before.Entries()-after.Entries() != 0 || before.Bytes()-after.Bytes() != int64(len(content)) {
		t.Fatalf(
			"file backup work = entries:%d bytes:%d, want 0/%d",
			before.Entries()-after.Entries(),
			before.Bytes()-after.Bytes(),
			len(content),
		)
	}
}

func TestRecoveryBackupObservationChargesExactDirectoryWork(t *testing.T) {
	root := t.TempDir()
	backupPath := filepath.Join(root, "backup")
	if err := os.MkdirAll(filepath.Join(backupPath, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupPath, "first"), []byte("ab"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupPath, "nested", "second"), []byte("cde"), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := recoveryBackupBudgetForTest(t)
	before := budget.RemainingTreeWork()

	observation, err := observeRecoveryBackupPathForTest(
		t,
		t.Context(),
		"backups/tree",
		backupPath,
		budget,
	)
	if err != nil {
		t.Fatalf("observe recovery directory backup: %v", err)
	}
	if observation.Error != "" || observation.Kind != recovery.PathKindDirectory {
		t.Fatalf("directory backup observation = %#v", observation)
	}
	if observation.Work.Entries() != 3 || observation.Work.Bytes() != 5 {
		t.Fatalf(
			"directory backup evidence work = entries:%d bytes:%d, want 3/5",
			observation.Work.Entries(),
			observation.Work.Bytes(),
		)
	}
	after := budget.RemainingTreeWork()
	if before.Entries()-after.Entries() != 3 || before.Bytes()-after.Bytes() != 5 {
		t.Fatalf(
			"directory backup work = entries:%d bytes:%d, want 3/5",
			before.Entries()-after.Entries(),
			before.Bytes()-after.Bytes(),
		)
	}
}

func TestRecoveryBackupObservationsDeduplicateSharedPayloadWork(t *testing.T) {
	root := t.TempDir()
	backupRelativePath := "backups/shared"
	backupPath := filepath.Join(root, filepath.FromSlash(backupRelativePath))
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupPath, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := recoveryEntry{Before: recoveryBeforePathDTO{
		Existed:    true,
		Kind:       recovery.PathKindFile,
		BackupPath: backupRelativePath,
	}}
	budget := recoveryBackupBudgetForTest(t)
	before := budget.RemainingTreeWork()

	observations, err := recoveryBackupObservationsForTest(
		t,
		t.Context(),
		root,
		[]recoveryEntry{entry, entry},
		budget,
	)
	if err != nil {
		t.Fatalf("observe deduplicated recovery backups: %v", err)
	}
	if len(observations) != 1 {
		t.Fatalf("backup observations = %d, want 1", len(observations))
	}
	after := budget.RemainingTreeWork()
	if before.Bytes()-after.Bytes() != 3 {
		t.Fatalf("deduplicated backup bytes = %d, want 3", before.Bytes()-after.Bytes())
	}
}

func TestRecoveryBackupObservationsBoundAggregateContentWork(t *testing.T) {
	root := t.TempDir()
	entries := make([]recoveryEntry, 0, 2)
	for _, name := range []string{"first", "second"} {
		relativePath := filepath.ToSlash(filepath.Join("backups", name))
		path := filepath.Join(root, filepath.FromSlash(relativePath))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("abc"), 0o600); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, recoveryEntry{Before: recoveryBeforePathDTO{
			Existed:    true,
			Kind:       recovery.PathKindFile,
			BackupPath: relativePath,
		}})
	}
	budget := recoveryBackupBudgetForTest(t)
	consumeRecoveryBackupBudgetLeaving(t, budget, budget.RemainingEntries(), 4)

	observations, err := recoveryBackupObservationsForTest(t, t.Context(), root, entries, budget)
	if err != nil {
		t.Fatalf("observe aggregate-bounded recovery backups: %v", err)
	}
	if len(observations) != 2 || observations[0].Error != "" {
		t.Fatalf("aggregate backup observations = %#v", observations)
	}
	if !strings.Contains(observations[1].Error, "exceeds 1 bytes") {
		t.Fatalf("second backup error = %q, want remaining-byte rejection", observations[1].Error)
	}
	if budget.RemainingBytes() != 0 {
		t.Fatalf("remaining backup bytes = %d, want 0", budget.RemainingBytes())
	}
}

func TestRecoveryBackupObservationChargesIndeterminateDirectoryMaximum(t *testing.T) {
	root := t.TempDir()
	backupPath := filepath.Join(root, "backup")
	if err := os.Mkdir(backupPath, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(backupPath, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	budget := recoveryBackupBudgetForTest(t)
	consumeRecoveryBackupBudgetLeaving(t, budget, 2, budget.RemainingBytes())

	observation, err := observeRecoveryBackupPathForTest(
		t,
		t.Context(),
		"backups/tree",
		backupPath,
		budget,
	)
	if err != nil {
		t.Fatalf("observe over-budget directory backup: %v", err)
	}
	if !strings.Contains(observation.Error, "exceeds 1 entries") {
		t.Fatalf("directory backup error = %q, want capacity-reserved overflow rejection", observation.Error)
	}
	if budget.RemainingEntries() != 0 {
		t.Fatalf("remaining backup entries = %d, want 0", budget.RemainingEntries())
	}
}

func TestRecoveryDirectoryObservationRequiresOverflowCapacityBeforeRead(t *testing.T) {
	budget, err := recovery.NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatalf("construct recovery budget: %v", err)
	}
	allEntries, err := recovery.NewArtifactWork(budget.RemainingEntries(), 0)
	if err != nil {
		t.Fatalf("construct aggregate entry work: %v", err)
	}
	if err := budget.AdmitTree(allEntries); err != nil {
		t.Fatalf("consume aggregate entry capacity: %v", err)
	}
	if _, _, err := recoveryArtifactWorkLimits(
		true,
		recovery.MaximumArtifactTreeBytes,
		budget,
	); err == nil || !strings.Contains(err.Error(), "remaining operation entry capacity") {
		t.Fatalf("directory limits error = %v, want pre-read overflow-capacity rejection", err)
	}
}

func TestRecoveryBackupObservationKeepsZeroWorkProofSeparate(t *testing.T) {
	root := t.TempDir()
	backupPath := filepath.Join(root, "backup")
	if err := os.WriteFile(backupPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	budget := recoveryBackupBudgetForTest(t)
	consumeRecoveryBackupBudgetLeaving(t, budget, budget.RemainingEntries(), 0)

	observation, err := observeRecoveryBackupPathForTest(
		t,
		t.Context(),
		"backups/empty",
		backupPath,
		budget,
	)
	if err != nil {
		t.Fatalf("observe empty recovery backup: %v", err)
	}
	if observation.Error != "" {
		t.Fatalf("empty backup observation error = %q", observation.Error)
	}
	if observation.Work.Entries() != 0 || observation.Work.Bytes() != 0 {
		t.Fatalf(
			"empty backup evidence work = entries:%d bytes:%d, want 0/0",
			observation.Work.Entries(),
			observation.Work.Bytes(),
		)
	}
	if budget.RemainingBytes() != 0 {
		t.Fatalf("empty backup changed semantic bytes to %d", budget.RemainingBytes())
	}
}

func TestRecoveryBackupObservationPreservesCancellation(t *testing.T) {
	root := t.TempDir()
	backupPath := filepath.Join(root, "backup")
	if err := os.WriteFile(backupPath, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := recoveryBackupBudgetForTest(t)
	before := budget.RemainingTreeWork()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := observeRecoveryBackupPathForTest(t, ctx, "backups/file", backupPath, budget)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled backup observation error = %v, want context.Canceled", err)
	}
	after := budget.RemainingTreeWork()
	if after != before {
		t.Fatalf("canceled backup changed work from %#v to %#v", before, after)
	}
}

func TestRecoveryBackupObservationRejectsExhaustedPathBudgetBeforePayloadWork(t *testing.T) {
	operationDir := canonicalRecoveryBackupOperationDir(t)
	backupRelativePath := "backups/file"
	backupPath := filepath.Join(operationDir, filepath.FromSlash(backupRelativePath))
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupPath, []byte("must-not-be-read"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := recoveryEntry{Before: recoveryBeforePathDTO{
		Existed:    true,
		Kind:       recovery.PathKindFile,
		BackupPath: backupRelativePath,
	}}
	budget := recoveryBackupBudgetForTest(t)
	for budget.AdmitPathComponents(recovery.MaximumPhysicalPathDepth) == nil {
	}
	before := budget.RemainingTreeWork()

	_, err := recoveryBackupObservations(
		t.Context(),
		operationDir,
		recoveryBackupActiveAuthorityForTest(t, operationDir),
		[]recoveryEntry{entry},
		storagecommit.Adapter{},
		budget,
	)
	if err == nil || !strings.Contains(err.Error(), "path-component work exceeds operation limit") {
		t.Fatalf("exhausted backup path error = %v, want pre-payload path refusal", err)
	}
	if after := budget.RemainingTreeWork(); after != before {
		t.Fatalf("path refusal changed payload work from %#v to %#v", before, after)
	}
}

func TestRecoveryBackupObservationRejectsReplacedOperationDirectory(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	operationDir := filepath.Join(parent, "operation")
	if err := os.Mkdir(operationDir, 0o700); err != nil {
		t.Fatal(err)
	}
	authority := recoveryBackupActiveAuthorityForTest(t, operationDir)
	if err := os.Rename(operationDir, filepath.Join(parent, "moved-operation")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(operationDir, "backups"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(operationDir, "backups", "file"), []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := recoveryEntry{Before: recoveryBeforePathDTO{
		Existed:    true,
		Kind:       recovery.PathKindFile,
		BackupPath: "backups/file",
	}}

	_, err = recoveryBackupObservations(
		t.Context(),
		operationDir,
		authority,
		[]recoveryEntry{entry},
		storagecommit.Adapter{},
		recoveryBackupBudgetForTest(t),
	)
	if err == nil || !strings.Contains(err.Error(), "active recovery journal identity changed") {
		t.Fatalf("replaced operation directory error = %v, want authority refusal", err)
	}
}

func TestRecoveryBackupObservationRejectsOperationDirectoryReplacementAfterIdentityCapture(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	operationDir := filepath.Join(parent, "operation")
	backupPath := filepath.Join(operationDir, "backups", "file")
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupPath, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	authority := recoveryBackupActiveAuthorityForTest(t, operationDir)
	entry := recoveryEntry{Before: recoveryBeforePathDTO{
		Existed:    true,
		Kind:       recovery.PathKindFile,
		BackupPath: "backups/file",
	}}
	filesystem := &replaceOperationDirectoryAfterIdentityReader{
		operationDir: operationDir,
		movedDir:     filepath.Join(parent, "moved-operation"),
	}

	_, err = recoveryBackupObservations(
		t.Context(),
		operationDir,
		authority,
		[]recoveryEntry{entry},
		filesystem,
		recoveryBackupBudgetForTest(t),
	)
	if err == nil {
		t.Fatal("operation-directory replacement after identity capture was accepted")
	}
	replacementContent, readErr := os.ReadFile(filepath.Join(operationDir, "backups", "file"))
	if readErr != nil {
		t.Fatalf("read replacement backup: %v", readErr)
	}
	if string(replacementContent) != "replacement" {
		t.Fatalf("replacement backup content = %q", replacementContent)
	}
}

func TestRecoveryBackupObservationReportsMissingBoundedEntry(t *testing.T) {
	operationDir := canonicalRecoveryBackupOperationDir(t)
	entry := recoveryEntry{Before: recoveryBeforePathDTO{
		Existed:    true,
		Kind:       recovery.PathKindFile,
		BackupPath: "backups/missing",
	}}

	observations, err := recoveryBackupObservations(
		t.Context(),
		operationDir,
		recoveryBackupActiveAuthorityForTest(t, operationDir),
		[]recoveryEntry{entry},
		storagecommit.Adapter{},
		recoveryBackupBudgetForTest(t),
	)
	if err != nil {
		t.Fatalf("observe missing recovery backup: %v", err)
	}
	if len(observations) != 1 || observations[0].Exists || observations[0].Error != "" {
		t.Fatalf("missing backup observation = %#v", observations)
	}
}

func TestRecoveryBackupObservationRejectsSymlink(t *testing.T) {
	operationDir := canonicalRecoveryBackupOperationDir(t)
	if err := os.Mkdir(filepath.Join(operationDir, "backups"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(operationDir, "backups", "link")); err != nil {
		t.Fatal(err)
	}
	entry := recoveryEntry{Before: recoveryBeforePathDTO{
		Existed:    true,
		Kind:       recovery.PathKindFile,
		BackupPath: "backups/link",
	}}

	observations, err := recoveryBackupObservations(
		t.Context(),
		operationDir,
		recoveryBackupActiveAuthorityForTest(t, operationDir),
		[]recoveryEntry{entry},
		storagecommit.Adapter{},
		recoveryBackupBudgetForTest(t),
	)
	if err != nil {
		t.Fatalf("observe symlink recovery backup: %v", err)
	}
	if len(observations) != 1 || !observations[0].Exists ||
		!strings.Contains(observations[0].Error, "unsupported backup kind") {
		t.Fatalf("symlink backup observation = %#v", observations)
	}
}

func TestRecoveryBackupObservationsNeedNoFilesystemWithoutBackupEvidence(t *testing.T) {
	observations, err := recoveryBackupObservations(
		t.Context(),
		"unused",
		ActiveJournalAuthority{},
		nil,
		nil,
		recoveryBackupBudgetForTest(t),
	)
	if err != nil {
		t.Fatalf("empty backup observation: %v", err)
	}
	if len(observations) != 0 {
		t.Fatalf("empty backup observations = %#v", observations)
	}
}

func TestRecoveryPathAndBackupShareAggregateContentBudget(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "current")
	backup := filepath.Join(root, "backup")
	for _, selected := range []string{path, backup} {
		if err := os.WriteFile(selected, []byte("abc"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	budget := recoveryBackupBudgetForTest(t)
	consumeRecoveryBackupBudgetLeaving(t, budget, budget.RemainingEntries(), 4)

	destination, err := output.Parse("~/.daem-test/current")
	if err != nil {
		t.Fatalf("parse recovery destination: %v", err)
	}
	pathObservation, err := observeGlobalRecoveryPath(
		t.Context(),
		destination.String(),
		"",
		nil,
		path,
		journalTestFilesystem(),
		nil,
		journalTestCodecs(),
		budget,
	)
	if err != nil {
		t.Fatalf("observe current recovery path: %v", err)
	}
	if pathObservation.Error != "" {
		t.Fatalf("current recovery path error = %q", pathObservation.Error)
	}
	backupObservation, err := observeRecoveryBackupPathForTest(
		t,
		t.Context(),
		"backups/file",
		backup,
		budget,
	)
	if err != nil {
		t.Fatalf("observe recovery backup after current path: %v", err)
	}
	if !strings.Contains(backupObservation.Error, "exceeds 1 bytes") {
		t.Fatalf("backup error = %q, want shared remaining-byte rejection", backupObservation.Error)
	}
	if budget.RemainingBytes() != 0 {
		t.Fatalf("remaining shared recovery bytes = %d, want 0", budget.RemainingBytes())
	}
}

func TestProjectRecoveryPathChargesRootedDirectoryWork(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "tree")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "entry"), []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	session := mustManifestAuthoritySession(t, root)
	defer session.root.Close()
	budget := recoveryBackupBudgetForTest(t)
	before := budget.RemainingTreeWork()

	observation, err := observeProjectRecoveryPath(
		t.Context(),
		"tree",
		"",
		nil,
		journalTestFilesystem(),
		session,
		journalTestCodecs(),
		budget,
	)
	if err != nil {
		t.Fatalf("observe rooted project recovery path: %v", err)
	}
	if observation.Error != "" || observation.Kind != recovery.PathKindDirectory {
		t.Fatalf("rooted project observation = %#v", observation)
	}
	after := budget.RemainingTreeWork()
	if before.Entries()-after.Entries() != 1 || before.Bytes()-after.Bytes() != 3 {
		t.Fatalf(
			"rooted project work = entries:%d bytes:%d, want 1/3",
			before.Entries()-after.Entries(),
			before.Bytes()-after.Bytes(),
		)
	}
}

func TestRemovalCleanupAssessmentContinuesConsumedPlanningBudget(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonicalize cleanup test root: %v", err)
	}
	parent := filepath.Join(root, "managed")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte("authorized")
	intent := removalPlanTestIntent(t, parent, content)
	cleanupPath := filepath.Join(parent, intent.Namespace().Names().Cleanup())
	if err := os.WriteFile(cleanupPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	authority, err := recovery.NewAuthority(
		"operation",
		"operation-directory",
		[]recovery.Entry{},
		durable.EmptySnapshot(),
		durable.EmptySnapshot(),
		nil,
		nil,
		removalPlanTestProvenance(t, root),
		strings.Repeat("a", 64),
		[]recovery.RemovalIntent{intent},
	)
	if err != nil {
		t.Fatalf("construct cleanup authority: %v", err)
	}
	selection, err := recovery.NewSelection(authority, nil)
	if err != nil {
		t.Fatalf("construct cleanup selection: %v", err)
	}
	plan, err := recovery.Classify(
		authority,
		selection,
		durable.EmptySnapshot(),
		nil,
		nil,
		ownership.EmptyRegistry(),
	)
	if err != nil {
		t.Fatalf("classify cleanup plan: %v", err)
	}
	budget := recoveryBackupBudgetForTest(t)
	consumeRecoveryBackupBudgetLeaving(t, budget, budget.RemainingEntries(), 0)

	_, err = assessRemovalCleanupPlan(
		t.Context(),
		plan,
		PlanLoadOptions{
			Filesystem: storagecommit.Adapter{},
			Resolver: func(output.Destination) (string, error) {
				return filepath.Join(parent, "config"), nil
			},
		},
		budget,
	)
	if err == nil || !strings.Contains(err.Error(), "reader probe exceeds reserved empty-proof capacity") {
		t.Fatalf("consumed planning budget error = %v, want no-reset rejection", err)
	}
}

func recoveryBackupBudgetForTest(t *testing.T) *recovery.PhysicalWorkBudget {
	t.Helper()
	budget, err := recovery.NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatalf("construct recovery backup work budget: %v", err)
	}
	return budget
}

func consumeRecoveryBackupBudgetLeaving(
	t *testing.T,
	budget *recovery.PhysicalWorkBudget,
	entries int,
	bytes int64,
) {
	t.Helper()
	remaining := budget.RemainingTreeWork()
	work, err := recovery.NewArtifactWork(
		remaining.Entries()-entries,
		remaining.Bytes()-bytes,
	)
	if err != nil {
		t.Fatalf("construct consumed recovery backup work: %v", err)
	}
	if err := budget.AdmitTree(work); err != nil {
		t.Fatalf("consume recovery backup work: %v", err)
	}
}

func observeRecoveryBackupPathForTest(
	t *testing.T,
	ctx context.Context,
	journalPath string,
	hostPath string,
	budget *recovery.PhysicalWorkBudget,
) (recoveryBackupObservation, error) {
	t.Helper()
	root, destination, err := rootedpath.CaptureDestinationBounded(
		hostPath,
		recovery.MaximumPhysicalPathDepth,
		budget,
	)
	if err != nil {
		t.Fatalf("bind recovery backup: %v", err)
	}
	capability, err := root.AcquireBounded(
		destination,
		recovery.MaximumPhysicalPathDepth,
		budget,
	)
	if err != nil {
		t.Fatalf("acquire recovery backup: %v", errors.Join(err, root.Close()))
	}
	observation, observeErr := observeRecoveryBackup(
		ctx,
		journalPath,
		storagecommit.Adapter{},
		capability,
		budget,
	)
	return observation, errors.Join(observeErr, capability.Close(), root.Close())
}

func recoveryBackupObservationsForTest(
	t *testing.T,
	ctx context.Context,
	operationDir string,
	entries []recoveryEntry,
	budget *recovery.PhysicalWorkBudget,
) ([]recoveryBackupObservation, error) {
	t.Helper()
	canonicalOperationDir, err := filepath.EvalSymlinks(operationDir)
	if err != nil {
		t.Fatalf("canonicalize recovery operation directory: %v", err)
	}
	return recoveryBackupObservations(
		ctx,
		canonicalOperationDir,
		recoveryBackupActiveAuthorityForTest(t, canonicalOperationDir),
		entries,
		storagecommit.Adapter{},
		budget,
	)
}

func recoveryBackupActiveAuthorityForTest(t *testing.T, operationDir string) ActiveJournalAuthority {
	t.Helper()
	identity, err := storagecommit.CaptureEntryIdentity(t.Context(), operationDir)
	if err != nil {
		t.Fatalf("capture recovery operation identity: %v", err)
	}
	authority, err := newActiveJournalAuthority(identity)
	if err != nil {
		t.Fatalf("construct recovery operation authority: %v", err)
	}
	return authority
}

func canonicalRecoveryBackupOperationDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonicalize recovery backup root: %v", err)
	}
	return root
}

type replaceOperationDirectoryAfterIdentityReader struct {
	storagecommit.Adapter
	operationDir string
	movedDir     string
}

func (reader *replaceOperationDirectoryAfterIdentityReader) CaptureWorkingDirectoryIdentity(
	ctx context.Context,
	capability rootedpath.WorkingDirectoryCapability,
	budget rootedpath.PhysicalTraversalBudget,
) (mutationfs.EntryIdentity, error) {
	identity, err := reader.Adapter.CaptureWorkingDirectoryIdentity(ctx, capability, budget)
	if err != nil {
		return nil, err
	}
	if err := os.Rename(reader.operationDir, reader.movedDir); err != nil {
		return nil, err
	}
	backupPath := filepath.Join(reader.operationDir, "backups", "file")
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(backupPath, []byte("replacement"), 0o600); err != nil {
		return nil, err
	}
	return identity, nil
}
