package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/statefile"
	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/hostpath"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
	"github.com/isty2e/daem/test/testkit"
)

func TestConcurrentApplyAcrossManifestsSerializesSharedGlobalDestination(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	leftManifest := writeLockedInstructionWorkspace(t, filepath.Join(root, "left"), "left\n", true)
	rightManifest := writeLockedInstructionWorkspace(t, filepath.Join(root, "right"), "right\n", true)
	destination := filepath.Join(home, ".codex", "AGENTS.md")

	left, right, leftErr, rightErr := runBlockedPhysicalMutationPair(
		t, destination, "codex", "global",
		[]string{"apply", "--manifest", leftManifest, "--yes"},
		[]string{"apply", "--manifest", rightManifest, "--yes"},
	)
	if (leftErr == nil) == (rightErr == nil) {
		t.Fatalf("concurrent apply errors = %v/%v; stderr=%q/%q; want one winner and one stale", leftErr, rightErr, left.stderr.String(), right.stderr.String())
	}
	stale := left
	if rightErr != nil {
		stale = right
	}
	if !strings.Contains(stale.stderr.String(), "stale_snapshot") {
		t.Fatalf("losing apply stderr = %q, want stale_snapshot", stale.stderr.String())
	}
	content, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(content); got != "left\n" && got != "right\n" {
		t.Fatalf("shared destination content = %q", got)
	}
}

func TestDisjointApplyDestinationsProgressWhileSiblingLeaseIsHeld(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	leftRoot := filepath.Join(root, "left")
	rightRoot := filepath.Join(root, "right")
	leftManifest := writeLockedInstructionWorkspace(t, leftRoot, "left\n", false)
	rightManifest := writeLockedInstructionWorkspace(t, rightRoot, "right\n", false)
	leftDestination := filepath.Join(leftRoot, "AGENTS.md")

	paths, err := daempaths.Resolve(leftManifest)
	if err != nil {
		t.Fatal(err)
	}
	store, err := mutation.NewStore(paths.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	domain, err := mutation.NewPhysicalPathDomain(mutation.PhysicalPathRequest{
		Path: leftDestination, Access: mutation.AccessExclusive, Effect: mutation.PathEffectDirectoryEntry,
		Target: "codex", Scope: "project",
	})
	if err != nil {
		t.Fatal(err)
	}
	holder, err := store.Acquire(context.Background(), domain)
	if err != nil {
		t.Fatal(err)
	}
	holderReleased := false
	defer func() {
		if !holderReleased {
			_ = holder.Release()
		}
	}()

	left := startWorkspaceMutationHelper(t, []string{"apply", "--manifest", leftManifest, "--yes"})
	right := startWorkspaceMutationHelper(t, []string{"apply", "--manifest", rightManifest, "--yes"})
	t.Cleanup(left.kill)
	t.Cleanup(right.kill)
	left.start(t)
	right.start(t)
	select {
	case err := <-left.done:
		t.Fatalf("held-destination apply completed early: %v; stderr=%s", err, left.stderr.String())
	case err := <-right.done:
		if err != nil {
			t.Fatalf("disjoint apply failed while sibling was held: %v; stderr=%s", err, right.stderr.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("disjoint apply did not progress independently")
	}
	if err := holder.Release(); err != nil {
		t.Fatal(err)
	}
	holderReleased = true
	if err := waitWorkspaceMutationHelper(t, left); err != nil {
		t.Fatalf("held-destination apply failed after release: %v; stderr=%s", err, left.stderr.String())
	}
	for path, want := range map[string]string{
		leftDestination:                       "left\n",
		filepath.Join(rightRoot, "AGENTS.md"): "right\n",
	} {
		content, err := os.ReadFile(path)
		if err != nil || string(content) != want {
			t.Fatalf("%s content = %q, %v; want %q", path, content, err, want)
		}
	}
}

func TestForeignApplyCannotTakeOwnershipFromRecoverableManifest(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	fixture := writeInterruptedRecoveryFixture(t, filepath.Join(root, "recovery"), home)
	applyManifest := writeLockedInstructionWorkspace(t, filepath.Join(root, "apply"), "third\n", true)
	applyPaths, err := daempaths.Resolve(applyManifest)
	if err != nil {
		t.Fatal(err)
	}
	writeWorkspaceMutationStatefile(
		t,
		applyPaths.StatefilePath,
		testkit.Snapshot(
			t,
			testkit.InstructionPathState(t, "shared", []string{"codex"}, "global", "~/.codex/AGENTS.md", fixture.afterHash),
		),
	)

	var applyStdout bytes.Buffer
	var applyStderr bytes.Buffer
	if exitCode := clipkg.RunWithOptions([]string{"apply", "--manifest", applyManifest, "--yes"}, clipkg.RunOptions{
		Context: context.Background(), Stdout: &applyStdout, Stderr: &applyStderr,
	}); exitCode == 0 || !strings.Contains(applyStderr.String(), "ownership_conflict") {
		t.Fatalf("foreign apply exit=%d stderr=%q, want ownership_conflict", exitCode, applyStderr.String())
	}
	var recoverStdout bytes.Buffer
	var recoverStderr bytes.Buffer
	if exitCode := clipkg.RunWithOptions([]string{"recover", "--manifest", fixture.manifestPath, "--yes"}, clipkg.RunOptions{
		Context: context.Background(), Stdout: &recoverStdout, Stderr: &recoverStderr,
	}); exitCode != 0 {
		t.Fatalf("recover exit=%d stderr=%q", exitCode, recoverStderr.String())
	}
	content, err := os.ReadFile(fixture.hostPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(content); got != "old\n" {
		t.Fatalf("shared destination content = %q, want recovered owner content", got)
	}
}

func TestConcurrentRecoverCommandsReplanOneActiveJournal(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	fixture := writeInterruptedRecoveryFixture(t, filepath.Join(root, "recovery"), home)

	left, right, leftErr, rightErr := runBlockedPhysicalMutationPair(
		t, fixture.hostPath, "codex", "global",
		[]string{"recover", "--manifest", fixture.manifestPath, "--yes"},
		[]string{"recover", "--manifest", fixture.manifestPath, "--yes"},
	)
	if (leftErr == nil) == (rightErr == nil) {
		t.Fatalf("recover errors = %v/%v; stderr=%q/%q; want one winner and one stale", leftErr, rightErr, left.stderr.String(), right.stderr.String())
	}
	loser := left
	if rightErr != nil {
		loser = right
	}
	if !strings.Contains(loser.stderr.String(), "stale_snapshot") {
		t.Fatalf("losing recover stderr = %q, want stale_snapshot", loser.stderr.String())
	}
	content, err := os.ReadFile(fixture.hostPath)
	if err != nil || string(content) != "old\n" {
		t.Fatalf("recovered content = %q, %v", content, err)
	}
	if _, err := os.Stat(fixture.operationDir); !os.IsNotExist(err) {
		t.Fatalf("operation journal stat error = %v, want absent", err)
	}
}

type interruptedRecoveryFixture struct {
	manifestPath string
	hostPath     string
	operationDir string
	afterHash    string
}

func writeInterruptedRecoveryFixture(t *testing.T, root string, home string) interruptedRecoveryFixture {
	t.Helper()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "daem.toml")
	if err := os.WriteFile(manifestPath, []byte("version = 1\ntargets = [\"codex\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	hostPath := filepath.Join(home, ".codex", "AGENTS.md")
	oldContent := []byte("old\n")
	afterContent := []byte("after\n")
	oldHash := string(artifact.HashFileContent(oldContent))
	afterHash := string(artifact.HashFileContent(afterContent))
	if err := os.MkdirAll(filepath.Dir(hostPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hostPath, oldContent, 0o600); err != nil {
		t.Fatal(err)
	}
	currentState := testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "shared", []string{"codex"}, "global", "~/.codex/AGENTS.md", oldHash),
	)
	nextState := testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "shared", []string{"codex"}, "global", "~/.codex/AGENTS.md", afterHash),
	)
	previous := singleCLIManagedPath(t, currentState)
	managedMutation, err := journal.NewManagedPathReplaceMutation(
		previous.Subject(),
		[]target.Target{target.TargetCodex},
		target.ScopeGlobal,
		outputtest.Parse(t, "~/.codex/AGENTS.md"),
		artifact.ContentHash(afterHash),
		artifact.ContentHash(oldHash),
		realization.PathProjectionFile,
		0o600,
		previous,
	)
	if err != nil {
		t.Fatal(err)
	}
	managedEvidence, err := observe.NewManagedPathEvidence(
		previous.Subject(),
		outputtest.Parse(t, "~/.codex/AGENTS.md"),
		true,
		artifact.ContentHash(oldHash),
		0o600,
	)
	if err != nil {
		t.Fatal(err)
	}
	operationID := journal.OperationID(time.Date(2026, 7, 10, 1, 0, 0, 0, time.UTC))
	claim := testkit.WriteActiveOwnershipClaim(t, manifestPath, "~/.codex/AGENTS.md", "")
	claimTransition, err := ownershipmutation.NewRetainTransition(claim)
	if err != nil {
		t.Fatal(err)
	}
	captured, err := journal.CaptureJournalWithOptions(
		context.Background(),
		journal.Paths{
			RecoveryDir:   paths.RecoveryDir,
			StatefilePath: paths.StatefilePath,
			ManifestRoot:  paths.ManifestRoot,
			DataDir:       paths.DataDir,
		},
		operationID,
		time.Date(2026, 7, 10, 1, 0, 0, 0, time.UTC),
		currentState,
		nextState,
		journal.CaptureOptions{
			Filesystem:           testFilesystem(),
			ClaimTransitions:     []ownershipmutation.ClaimTransition{claimTransition},
			ManagedPathMutations: []journal.ManagedPathMutation{managedMutation},
			ManagedPathEvidence:  []observe.ManagedPathEvidence{managedEvidence},
			Resolver:             hostpath.NewResolver(root).Resolve,
			StateCodec:           statefile.Codec{},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	writeWorkspaceMutationStatefile(t, paths.StatefilePath, currentState)
	if err := os.WriteFile(hostPath, afterContent, 0o600); err != nil {
		t.Fatal(err)
	}
	return interruptedRecoveryFixture{
		manifestPath: manifestPath, hostPath: hostPath, operationDir: captured.Directory, afterHash: afterHash,
	}
}

func writeWorkspaceMutationStatefile(t *testing.T, path string, file durable.Snapshot) {
	t.Helper()
	content, err := statefile.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeLockedInstructionWorkspace(t *testing.T, root string, content string, global bool) string {
	t.Helper()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(root, "source.md")
	if err := os.WriteFile(sourcePath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "daem.toml")
	scope := ""
	source := "source.md"
	if global {
		scope = "scope = \"global\"\n"
		source = filepath.ToSlash(sourcePath)
	}
	manifest := fmt.Sprintf("version = 1\ntargets = [\"codex\"]\n\n[instructions.shared]\nsource = %q\n%stargets = [\"codex\"]\n", source, scope)
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := clipkg.RunWithOptions([]string{"lock", "--manifest", manifestPath}, clipkg.RunOptions{
		Context: context.Background(), Stdout: &stdout, Stderr: &stderr,
	}); exitCode != 0 {
		t.Fatalf("lock %s exitCode=%d stderr=%s", manifestPath, exitCode, stderr.String())
	}
	return manifestPath
}

func runBlockedPhysicalMutationPair(
	t *testing.T,
	path string,
	target string,
	scope string,
	leftArgs []string,
	rightArgs []string,
) (*workspaceMutationHelper, *workspaceMutationHelper, error, error) {
	t.Helper()
	paths, err := daempaths.Resolve(argumentValue(leftArgs, "--manifest"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := mutation.NewStore(paths.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	domain, err := mutation.NewPhysicalPathDomain(mutation.PhysicalPathRequest{
		Path: path, Access: mutation.AccessExclusive, Effect: mutation.PathEffectDirectoryEntry,
		Target: target, Scope: scope,
	})
	if err != nil {
		t.Fatal(err)
	}
	holder, err := store.Acquire(context.Background(), domain)
	if err != nil {
		t.Fatal(err)
	}
	holderReleased := false
	defer func() {
		if !holderReleased {
			_ = holder.Release()
		}
	}()

	left := startWorkspaceMutationHelper(t, leftArgs)
	right := startWorkspaceMutationHelper(t, rightArgs)
	t.Cleanup(left.kill)
	t.Cleanup(right.kill)
	left.start(t)
	right.start(t)
	select {
	case err := <-left.done:
		t.Fatalf("left helper completed while destination holder was active: %v; stderr=%s", err, left.stderr.String())
	case err := <-right.done:
		t.Fatalf("right helper completed while destination holder was active: %v; stderr=%s", err, right.stderr.String())
	case <-time.After(150 * time.Millisecond):
	}
	if err := holder.Release(); err != nil {
		t.Fatal(err)
	}
	holderReleased = true
	return left, right, waitWorkspaceMutationHelper(t, left), waitWorkspaceMutationHelper(t, right)
}

func argumentValue(args []string, name string) string {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == name {
			return args[index+1]
		}
	}
	return ""
}
