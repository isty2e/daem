//go:build darwin

package execute

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
)

func TestRecoveryRejectsNewDescendantMountBeforeEffects(t *testing.T) {
	if _, err := exec.LookPath("hdiutil"); err != nil {
		t.Skipf("hdiutil is unavailable: %v", err)
	}
	fixture := newGlobalFileRecoveryFixture(t, output.Destination{}, false)
	mountPoint := filepath.Dir(fixture.admittedPath)
	if err := os.Remove(fixture.admittedPath); err != nil {
		t.Fatalf("remove pre-mount expected-after file: %v", err)
	}
	if err := os.Remove(mountPoint); err != nil {
		t.Fatalf("remove pre-mount destination parent: %v", err)
	}
	if err := os.Mkdir(mountPoint, 0o700); err != nil {
		t.Fatalf("create mount point: %v", err)
	}

	imagePath := filepath.Join(t.TempDir(), "descendant-mount.dmg")
	runHDIUtil(
		t,
		"create",
		"-size", "8m",
		"-fs", "HFS+",
		"-volname", "daem-recovery-test",
		"-type", "UDIF",
		"-quiet",
		imagePath,
	)
	attached := false
	t.Cleanup(func() {
		if !attached {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, "hdiutil", "detach", mountPoint)
		if output, err := command.CombinedOutput(); err != nil {
			t.Errorf("detach temporary recovery mount: %v: %s", err, output)
		}
	})
	runHDIUtil(
		t,
		"attach",
		"-nobrowse",
		"-owners", "off",
		"-mountpoint", mountPoint,
		imagePath,
	)
	attached = true
	writeRecoveryTestFile(t, fixture.admittedPath, fixture.after)

	hostActions := 0
	for attempt := 1; attempt <= 2; attempt++ {
		err := executeRecoveryPlanWithOptionsForTest(
			context.Background(),
			fixture.plan,
			fixture.paths,
			RecoveryOptions{
				Resolver:                destinationResolver(fixture.paths),
				OwnershipRegistryBinder: testOwnershipRegistryBinder(),
				StateCodec:              testStateCodec(),
				StateReader:             testStateReader(fixture.paths.StatefilePath),
				Filesystem:              testFilesystem(),
				beforeHostAction: func(int) error {
					hostActions++
					return nil
				},
			},
		)
		if !hasRootedPathFailureKind(err, rootedpath.FailureMountChanged) {
			t.Fatalf(
				"recovery attempt %d error = %v, want %s",
				attempt,
				err,
				rootedpath.FailureMountChanged,
			)
		}
		if hostActions != 0 {
			t.Fatalf("recovery host actions after attempt %d = %d, want none", attempt, hostActions)
		}
		assertRecoveryTestContent(t, fixture.admittedPath, fixture.after)
		if _, err := os.Stat(fixture.plan.OperationDir()); err != nil {
			t.Fatalf("retained recovery journal after attempt %d: %v", attempt, err)
		}
	}
}

func runHDIUtil(t *testing.T, arguments ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "hdiutil", arguments...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("hdiutil %v: %v: %s", arguments, err, output)
	}
}
