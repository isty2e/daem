//go:build darwin || linux

package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/metadatatx"
)

func TestApplyRetiresQualifiedRecoveryThenRunsUnchangedPlan(t *testing.T) {
	for _, test := range []struct {
		name         string
		journal      bool
		pair         bool
		noOp         bool
		wantKinds    []string
		journalPhase retirement.Phase
	}{
		{name: "selected pair", pair: true, wantKinds: []string{"declaration_pair"}},
		{name: "retained journal and no-op", journal: true, noOp: true, wantKinds: []string{"journal_retirement"}, journalPhase: retirement.PhasePrepared},
		{name: "joint, then apply", journal: true, pair: true, wantKinds: []string{"journal_retirement", "declaration_pair"}, journalPhase: retirement.PhaseFinalizing},
	} {
		t.Run(test.name, func(t *testing.T) {
			var manifest, host, before, desired string
			var journal recoverCleanupCLIFixture
			if test.journal {
				journal = writeRecoverCleanupCLIFixtureWithPermissions(t, test.journalPhase, true, realization.PathPermissionsExecutableClass)
				manifest, host = journal.manifestPath, journal.hostPath
				before, desired = string(journal.oldContent), string(journal.newContent)
				if test.noOp {
					desired = before
				}
				prepareRecoveryApplyDeclarations(t, manifest, desired)
				writeApplyConfirmationFile(t, filepath.Dir(host), filepath.Base(host), before)
			} else {
				root := t.TempDir()
				t.Setenv("XDG_DATA_HOME", filepath.Join(root, "xdg-data"))
				manifest, _, _ = writeApplyConfirmationFixture(t, root)
				host, desired = filepath.Join(root, "AGENTS.md"), "shared instructions\n"
			}
			paths, err := daempaths.Resolve(manifest)
			if err != nil {
				t.Fatal(err)
			}
			var marker string
			if test.pair {
				marker = stageApplyPair(t, paths, true)
			}
			assertPending := func() {
				t.Helper()
				if test.journal {
					journal.assertCleanupPresent(t)
					assertApplyConfirmationFileContent(t, host, before)
				} else {
					assertCLIPathMissing(t, host)
				}
				if test.pair {
					readCleanupFile(t, marker)
				}
			}
			preview := clijson.DecodePlan(t, runApplyCleanupJSON(t, "apply", "--dry-run", "--manifest", manifest))
			if preview.HasErrors || !slices.Equal(preview.RecoveryCleanup, cleanupRows(test.wantKinds, "")) {
				t.Fatalf("preview cleanup = %+v; has_errors = %t", preview.RecoveryCleanup, preview.HasErrors)
			}
			assertPending()
			if test.noOp {
				var stdout, stderr bytes.Buffer
				code := RunWithOptions([]string{"apply", "--manifest", manifest}, interactiveRunOptions(strings.NewReader("no\n"), &stdout, &stderr))
				if code != 1 || !strings.Contains(stdout.String(), "already-retired apply journal") || !strings.Contains(stderr.String(), "Proceed with apply?") {
					t.Fatalf("no-op decline: exit=%d, stdout=%s, stderr=%s", code, &stdout, &stderr)
				}
				assertPending()
			}
			writeResult := runApplyCleanupJSON(t, "apply", "--yes", "--manifest", manifest)
			result := clijson.DecodeApplyResult(t, writeResult)
			wantActions := 1
			if test.noOp {
				wantActions = 0
			}
			if result.HasErrors || result.ActionCount != wantActions || !slices.Equal(result.RecoveryCleanup, cleanupRows(test.wantKinds, "retired")) {
				t.Fatalf("apply actions = %d, want %d: %s", result.ActionCount, wantActions, writeResult)
			}
			if test.pair {
				assertCLIPathMissing(t, filepath.Dir(marker))
			}
			if test.journal {
				journal.assertCleanupAbsent(t)
			}
			assertApplyConfirmationFileContent(t, host, desired)
		})
	}
}

func TestApplyAndLockPreserveBothRecordsIfEitherOwnerCannotProceed(t *testing.T) {
	for _, name := range []string{"unfinished pair", "unrelated pair", "invalid pair evidence", "active journal", "unknown journal", "global fence", "overlapping local source"} {
		t.Run(name, func(t *testing.T) {
			var manifest string
			var journalPaths []string
			var host string
			if name == "active journal" {
				fixture := writeRecoverConfirmationFixture(t)
				manifest, host = fixture.manifestPath, fixture.hostPath
				journalPaths = append(journalPaths, fixture.operationDir)
			} else {
				fixture := writeRecoverCleanupCLIFixture(t, retirement.PhaseFinalizing, true)
				manifest, host = fixture.manifestPath, fixture.hostPath
				journalPaths = append(journalPaths, fixture.controlDir, fixture.residueDir)
			}
			prepareRecoveryApplyDeclarations(t, manifest, "intended host contents\n")
			paths, err := daempaths.Resolve(manifest)
			if err != nil {
				t.Fatal(err)
			}
			var marker string
			switch name {
			case "unknown journal":
				writeApplyConfirmationFile(t, paths.RecoveryDir, "foreign-entry", "not journal evidence")
			case "global fence":
				metadatatx.WriteInterrupted(t, paths.GlobalMetadataDir())
			case "overlapping local source":
				// The exact pair covers the source path even though no lock
				// generation should fetch or hash that source during cleanup.
				content := strings.Replace(readCleanupFile(t, manifest), "instructions.md", ".daem/metadata-transaction/target-000.before", 1)
				writeApplyConfirmationFile(t, paths.ManifestRoot, filepath.Base(manifest), content)
			case "unrelated pair":
				other := filepath.Join(paths.ManifestRoot, "other.toml")
				marker = metadatatx.WriteInterruptedPair(t, paths.StateDir, other, paths.LockfilePath,
					readCleanupFile(t, manifest), readCleanupFile(t, paths.LockfilePath), true)
			}
			if marker == "" {
				marker = stageApplyPair(t, paths, name != "unfinished pair")
			}
			if name == "invalid pair evidence" {
				writeApplyConfirmationFile(t, filepath.Dir(marker), "target-000.before", "backup no longer matches")
				preview, err := applyworkflow.PlanDryRun(t.Context(), applyworkflow.CommandInput{ManifestPath: manifest})
				failure := applyworkflow.ClassifyFailure(err, preview.CommandResult)
				if err == nil || failure.Reason() != applyworkflow.FailureReasonFileSetEvidenceInvalid || !failure.RecoveryBarrier().JournalKnown() {
					t.Fatalf("invalid evidence obscured its own reason or the journal axis: %v, code = %s", err, failure.Reason())
				}
			}
			wantMarker, wantHost, wantState := readCleanupFile(t, marker), readCleanupFile(t, host), readCleanupFile(t, paths.StatefilePath)
			for _, args := range [][]string{{"apply", "--dry-run"}, {"apply", "--yes"}, {"lock"}} {
				var stdout, stderr bytes.Buffer
				args = append(args, "--json", "--manifest", manifest)
				if code := RunWithOptions(args, RunOptions{Stdout: &stdout, Stderr: &stderr}); code != 1 || stderr.Len() != 0 {
					t.Fatalf("%v exit=%d, stdout=%s, stderr=%s", args, code, &stdout, &stderr)
				}
				if bytes.Contains(stdout.Bytes(), []byte(`"recovery_cleanup"`)) {
					t.Fatalf("unqualified cleanup was disclosed: %s", &stdout)
				}
				assertApplyConfirmationFileContent(t, marker, wantMarker)
				assertApplyConfirmationFileContent(t, host, wantHost)
				assertApplyConfirmationFileContent(t, paths.StatefilePath, wantState)
				for _, path := range journalPaths {
					if _, err := os.Stat(path); err != nil {
						t.Fatalf("journal evidence was lost: %v", err)
					}
				}
			}
		})
	}
}

func TestLockOnlyTouchesJournalToClearItsPair(t *testing.T) {
	fixture := writeRecoverCleanupCLIFixture(t, retirement.PhaseFinalizing, true)
	prepareRecoveryApplyDeclarations(t, fixture.manifestPath, "instructions\n")
	fixture.assertCleanupPresent(t)
	marker := stageApplyPair(t, fixture.paths, true)
	var stdout, stderr bytes.Buffer
	args := []string{"lock", "--json", "--dry-run", "--manifest", fixture.manifestPath}
	if code := RunWithOptions(args, RunOptions{Stdout: &stdout, Stderr: &stderr}); code != 1 || stderr.Len() != 0 {
		t.Fatalf("lock preview exit=%d, stdout=%s, stderr=%s", code, &stdout, &stderr)
	}
	fixture.assertCleanupPresent(t)
	readCleanupFile(t, marker)
	state, host := readCleanupFile(t, fixture.paths.StatefilePath), readCleanupFile(t, fixture.hostPath)
	clijson.DecodeLock(t, runApplyCleanupJSON(t, "lock", "--manifest", fixture.manifestPath))
	assertCLIPathMissing(t, filepath.Dir(marker))
	fixture.assertCleanupAbsent(t)
	assertApplyConfirmationFileContent(t, fixture.paths.StatefilePath, state)
	assertApplyConfirmationFileContent(t, fixture.hostPath, host)
}

func TestLockReportsBothRetirementsWhenSourceResolutionFails(t *testing.T) {
	fixture := writeRecoverCleanupCLIFixture(t, retirement.PhaseFinalizing, true)
	prepareRecoveryApplyDeclarations(t, fixture.manifestPath, "instructions\n")
	marker := stageApplyPair(t, fixture.paths, true)
	if err := os.Remove(filepath.Join(fixture.paths.ManifestRoot, "instructions.md")); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	args := []string{"lock", "--json", "--manifest", fixture.manifestPath}
	if code := RunWithOptions(args, RunOptions{Stdout: &stdout, Stderr: &stderr}); code != 1 || stderr.Len() != 0 {
		t.Fatalf("lock exit=%d, stdout=%s, stderr=%s", code, &stdout, &stderr)
	}
	failure := clijson.DecodeFailure(t, stdout.String(), "lock", "operation")
	for _, want := range []string{"manifest and lockfile recovery record was already cleared", "already-retired apply journal was cleared"} {
		if !slices.ContainsFunc(failure.Guidance, func(guidance clijson.FailureGuidance) bool { return strings.Contains(guidance.Message, want) }) {
			t.Fatalf("lock did not disclose %q before its source failure: %s", want, &stdout)
		}
	}
	assertCLIPathMissing(t, filepath.Dir(marker))
	fixture.assertCleanupAbsent(t)
}

func TestApplyRecoveryCleanupReportsPartialRetirement(t *testing.T) {
	for _, test := range []struct {
		name           string
		failCleanup    int
		changePair     bool
		newJournal     bool
		journalOutcome string
		pairOutcome    string
	}{
		{name: "first cleanup fails", failCleanup: 1, journalOutcome: "still_fenced", pairOutcome: "not_attempted"},
		{name: "final garbage collection fails", failCleanup: 2, journalOutcome: "retired", pairOutcome: "not_attempted"},
		{name: "pair changes after journal retirement", changePair: true, journalOutcome: "retired", pairOutcome: "still_fenced"},
		{name: "new barrier after journal retirement", newJournal: true, journalOutcome: "retired", pairOutcome: "not_attempted"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := writeRecoverCleanupCLIFixture(t, retirement.PhaseFinalizing, true)
			prepareRecoveryApplyDeclarations(t, fixture.manifestPath, string(fixture.newContent))
			writeApplyConfirmationFile(t, fixture.paths.ManifestRoot, "AGENTS.md", string(fixture.oldContent))
			marker := stageApplyPair(t, fixture.paths, true)
			state, evidence := readCleanupFile(t, fixture.paths.StatefilePath), readCleanupFile(t, marker)
			filesystem := mutationfs.Store(&failNthCleanupStore{Store: storagecommit.Adapter{}, failOn: test.failCleanup})
			if test.changePair || test.newJournal {
				filesystem = afterJournalCleanupStore{
					Store: storagecommit.Adapter{}, gcName: filepath.Base(fixture.garbageDir),
					afterGC: func() {
						if test.changePair {
							writeApplyConfirmationFile(t, filepath.Dir(marker), "target-000.before", "new unapproved evidence")
						} else {
							writeApplyConfirmationFile(t, fixture.paths.RecoveryDir, "unexpected-entry", "a later fence")
						}
					},
				}
			}
			var stdout, stderr bytes.Buffer
			args := []string{"apply", "--yes", "--json", "--manifest", fixture.manifestPath}
			code := RunWithOptions(args, RunOptions{
				Stdout: &stdout, Stderr: &stderr,
				ApplyExecuteOptions: applyworkflow.ExecuteOptions{RecoveryCleanupFilesystem: filesystem},
			})
			if code != 1 || stderr.Len() != 0 {
				t.Fatalf("apply exit=%d, stdout=%s, stderr=%s", code, &stdout, &stderr)
			}
			result := clijson.DecodeApplyResult(t, stdout.Bytes())
			want := []clijson.RecoveryCleanup{{Kind: "journal_retirement", Outcome: test.journalOutcome}, {Kind: "declaration_pair", Outcome: test.pairOutcome}}
			if !result.HasErrors || result.ActionCount != 0 || !slices.Equal(result.RecoveryCleanup, want) {
				t.Fatalf("partial apply result = %s, want %v", &stdout, want)
			}
			assertApplyConfirmationFileContent(t, fixture.hostPath, string(fixture.oldContent))
			assertApplyConfirmationFileContent(t, fixture.paths.StatefilePath, state)
			assertApplyConfirmationFileContent(t, marker, evidence)
			if test.journalOutcome == "still_fenced" {
				fixture.assertCleanupPresent(t)
			} else {
				assertCLIPathMissing(t, fixture.controlDir)
				assertCLIPathMissing(t, fixture.residueDir)
			}
			if !test.changePair && !test.newJournal {
				content := runApplyCleanupJSON(t, "apply", "--yes", "--manifest", fixture.manifestPath)
				retry := clijson.DecodeApplyResult(t, content)
				if retry.ActionCount != 1 || retry.HasErrors || len(retry.RecoveryCleanup) == 0 || retry.RecoveryCleanup[len(retry.RecoveryCleanup)-1].Kind != "declaration_pair" {
					t.Fatalf("retry did not complete: %s", content)
				}
				assertCLIPathMissing(t, filepath.Dir(marker))
			}
		})
	}
}

func TestApplyCleanupRefusesToRebindApprovedRecords(t *testing.T) {
	for _, name := range []string{"declaration marker", "journal record", "manifest", "state directory"} {
		t.Run(name, func(t *testing.T) {
			fixture := writeRecoverCleanupCLIFixture(t, retirement.PhaseFinalizing, true)
			prepareRecoveryApplyDeclarations(t, fixture.manifestPath, string(fixture.newContent))
			writeApplyConfirmationFile(t, fixture.paths.ManifestRoot, "AGENTS.md", string(fixture.oldContent))
			marker := stageApplyPair(t, fixture.paths, true)
			prepared, err := applyworkflow.PlanWrite(t.Context(), applyworkflow.CommandInput{ManifestPath: fixture.manifestPath})
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()

			state := readCleanupFile(t, fixture.paths.StatefilePath)
			journalControl := filepath.Join(fixture.controlDir, retirement.RecordFileName)
			swap := func(path string) {
				t.Helper()
				content := readCleanupFile(t, path)
				replacement := filepath.Join(filepath.Dir(path), ".same-content-replacement")
				writeApplyConfirmationFile(t, filepath.Dir(replacement), filepath.Base(replacement), content)
				if err := os.Rename(replacement, path); err != nil {
					t.Fatal(err)
				}
			}
			switch name {
			case "declaration marker":
				swap(marker)
			case "journal record":
				swap(journalControl)
			case "manifest":
				swap(fixture.manifestPath)
			case "state directory":
				// Put the same entries back under the selected name, but in
				// another incarnation of the StateDir.
				detached := fixture.paths.StateDir + "-detached"
				if err := os.Rename(fixture.paths.StateDir, detached); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(fixture.paths.StateDir, 0o700); err != nil {
					t.Fatal(err)
				}
				entries, err := os.ReadDir(detached)
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if err := os.Rename(filepath.Join(detached, entry.Name()), filepath.Join(fixture.paths.StateDir, entry.Name())); err != nil {
						t.Fatal(err)
					}
				}
			}
			result, err := applyworkflow.ExecuteWithOptions(t.Context(), prepared, applyworkflow.ExecuteOptions{PlanWasDisclosed: true})
			if err == nil || result.ExecutionAttempted || len(result.RecoveryCleanup) != 2 || result.RecoveryCleanup[0].Outcome != "not_attempted" || result.RecoveryCleanup[1].Outcome != "not_attempted" {
				t.Fatalf("approved cleanup was not refused before work: %+v, error = %v", result.RecoveryCleanup, err)
			}
			if name != "state directory" && applyworkflow.ClassifyFailure(err, result).Reason() != applyworkflow.FailureReasonStalePlan {
				t.Fatalf("same-content replacement was not stale: %v", err)
			}
			readCleanupFile(t, marker)
			readCleanupFile(t, journalControl)
			assertApplyConfirmationFileContent(t, fixture.hostPath, string(fixture.oldContent))
			assertApplyConfirmationFileContent(t, fixture.paths.StatefilePath, state)
		})
	}
}

func TestApplyCleanupDoesNotAdmitNativeReplacement(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "xdg-data"))
	manifest, _, _ := writeApplyConfirmationFixture(t, root)
	paths, err := daempaths.Resolve(manifest)
	if err != nil {
		t.Fatal(err)
	}
	marker := stageApplyPair(t, paths, true)
	_, err = applyworkflow.PlanNativeReplacement(t.Context(), applyworkflow.NativeReplacementInput{
		ManifestPath: manifest,
		ID:           "settings",
		Keys:         []string{"/example"},
	})
	if !errors.Is(err, fileset.ErrInterruptedFileSetTransaction) {
		t.Fatalf("native replacement did not retain its strict fence: %v", err)
	}
	readCleanupFile(t, marker)
}

type afterJournalCleanupStore struct {
	mutationfs.Store
	gcName  string
	afterGC func()
}

func (store afterJournalCleanupStore) CleanupRootedEntry(ctx context.Context, capability rootedpath.CommitCapability, identity mutationfs.EntryIdentity, limits mutationfs.TreeTraversalLimits) (mutationfs.CommitOutcome, error) {
	gc := capability.Destination().Relative().Path() == store.gcName
	outcome, err := store.Store.CleanupRootedEntry(ctx, capability, identity, limits)
	if gc && err == nil {
		store.afterGC()
	}
	return outcome, err
}

func TestApplyCleanupRetainsReceiptWhenNormalPlanChanges(t *testing.T) {
	fixture := writeRecoverCleanupCLIFixture(t, retirement.PhaseFinalizing, true)
	paths := fixture.paths
	prepareRecoveryApplyDeclarations(t, fixture.manifestPath, string(fixture.newContent))
	writeApplyConfirmationFile(t, paths.ManifestRoot, "AGENTS.md", string(fixture.oldContent))
	legacy := readCleanupFile(t, paths.StatefilePath)
	marker := stageApplyPair(t, paths, true)
	prepared, err := applyworkflow.PlanWrite(t.Context(), applyworkflow.CommandInput{ManifestPath: fixture.manifestPath})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()

	// The live output changes after consent. The cleanup is still authorized,
	// but the strict replan must not perform the approved update.
	writeApplyConfirmationFile(t, paths.ManifestRoot, "AGENTS.md", "new manual edit\n")
	result, err := applyworkflow.ExecuteWithOptions(t.Context(), prepared, applyworkflow.ExecuteOptions{PlanWasDisclosed: true})
	failure := applyworkflow.ClassifyFailure(err, result)
	if err == nil || failure.Reason() != applyworkflow.FailureReasonStalePlan || result.ExecutionAttempted || result.ActionCount != 0 || len(result.RecoveryCleanup) != 2 || result.RecoveryCleanup[0].Outcome != "retired" || result.RecoveryCleanup[1].Outcome != "retired" {
		t.Fatalf("changed plan = %v, failure=%s, result=%+v", err, failure.Reason(), result.RecoveryCleanup)
	}
	assertCLIPathMissing(t, filepath.Dir(marker))
	fixture.assertCleanupAbsent(t)
	assertApplyConfirmationFileContent(t, fixture.hostPath, "new manual edit\n")
	assertApplyConfirmationFileContent(t, paths.StatefilePath, legacy)
	assertCLIPathMissing(t, paths.OwnershipRegistryPath)
	assertCLIPathMissing(t, paths.CarrierClaimRegistryPath)
}

func prepareRecoveryApplyDeclarations(t *testing.T, manifest, source string) {
	t.Helper()
	root := filepath.Dir(manifest)
	writeApplyConfirmationFile(t, root, "instructions.md", source)
	writeApplyConfirmationFile(t, root, filepath.Base(manifest), `version = 1
targets = ["codex"]
[instructions.recovery-confirmation]
source = "instructions.md"
`)
	runApplyCleanupJSON(t, "lock", "--manifest", manifest)
}

func stageApplyPair(t *testing.T, paths daempaths.Paths, finished bool) string {
	t.Helper()
	return metadatatx.WriteInterruptedPair(t, paths.StateDir, paths.ManifestPath, paths.LockfilePath,
		readCleanupFile(t, paths.ManifestPath), readCleanupFile(t, paths.LockfilePath), finished)
}

func readCleanupFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func runApplyCleanupJSON(t *testing.T, args ...string) []byte {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := RunWithOptions(append(args, "--json"), RunOptions{Stdout: &stdout, Stderr: &stderr}); code != 0 || stderr.Len() != 0 {
		t.Fatalf("%v exit=%d stdout=%s stderr=%s", args, code, &stdout, &stderr)
	}
	return stdout.Bytes()
}

func cleanupRows(kinds []string, outcome string) []clijson.RecoveryCleanup {
	rows := make([]clijson.RecoveryCleanup, 0, len(kinds))
	for _, kind := range kinds {
		rows = append(rows, clijson.RecoveryCleanup{Kind: kind, Outcome: outcome})
	}
	return rows
}
