//go:build linux

package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/isty2e/daem/test/testkit"
)

func TestNFSSingleClientLifecycle(t *testing.T) {
	base := os.Getenv("DAEM_TEST_NFS_ROOT")
	if base == "" {
		t.Skip("set DAEM_TEST_NFS_ROOT to a writable NFS directory")
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(base, &stat); err != nil {
		t.Fatal(err)
	}
	if stat.Type != unix.NFS_SUPER_MAGIC {
		t.Fatal("DAEM_TEST_NFS_ROOT must be on NFS")
	}
	root, err := os.MkdirTemp(base, "daem-nfs-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))

	run := func(args ...string) string {
		t.Helper()
		exit, stdout, stderr := runOwnershipCLI(args...)
		if exit != 0 {
			t.Fatalf("%v: exit=%d stdout=%q stderr=%q", args, exit, stdout, stderr)
		}
		return stdout
	}
	workspace := filepath.Join(root, "workspace")
	manifest := filepath.Join(workspace, "daem.toml")
	testkit.WriteFile(t, workspace, "daem.toml", `version = 1
 targets = ["codex"]

 [[skill]]
 name = "probe"
 scope = "global"
 source = { path = "`+filepath.Join(workspace, "skills", "probe")+`", mode = "vendor" }
`)
	installed := filepath.Join(home, ".agents", "skills", "probe", "SKILL.md")
	for _, content := range []string{
		"---\nname: probe\ndescription: initial\n---\nfirst\n",
		"---\nname: probe\ndescription: updated\n---\nsecond\n",
	} {
		testkit.WriteFile(t, workspace, "skills/probe/SKILL.md", content)
		run("lock", "--manifest", manifest)
		run("apply", "--manifest", manifest, "--dry-run")
		run("apply", "--manifest", manifest, "--yes")
		testkit.AssertFileContent(t, installed, content)
		run("status", "--manifest", manifest, "--check")
		run("apply", "--manifest", manifest, "--yes")
	}
	doctor := run("doctor", "--manifest", manifest, "--json")
	var report struct {
		HasErrors bool `json:"has_errors"`
	}
	if err := json.Unmarshal([]byte(doctor), &report); err != nil {
		t.Fatal(err)
	}
	if report.HasErrors {
		t.Fatalf("doctor reported errors: %s", doctor)
	}

	testkit.WriteFile(t, workspace, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
	run("lock", "--manifest", manifest)
	run("apply", "--manifest", manifest, "--yes")
	if _, err := os.Lstat(filepath.Dir(installed)); !os.IsNotExist(err) {
		t.Fatalf("removed skill still exists: %v", err)
	}

	fixture := writeInterruptedRecoveryFixture(t, filepath.Join(root, "interrupted"), home)
	if exit, _, _ := runOwnershipCLI("doctor", "--manifest", fixture.manifestPath, "--json"); exit == 0 {
		t.Fatal("doctor ran through an active recovery journal")
	}
	run("recover", "--manifest", fixture.manifestPath, "--dry-run")
	testkit.AssertFileContent(t, fixture.hostPath, "after\n")
	recovery := startWorkspaceMutationHelper(t, []string{"recover", "--manifest", fixture.manifestPath, "--yes"})
	t.Cleanup(recovery.kill)
	recovery.start(t)
	if err := waitWorkspaceMutationHelper(t, recovery); err != nil {
		t.Fatalf("recover in a new process: %v; stderr=%s", err, recovery.stderr.String())
	}
	testkit.AssertFileContent(t, fixture.hostPath, "old\n")
	if _, err := os.Lstat(fixture.operationDir); !os.IsNotExist(err) {
		t.Fatalf("recovered journal still exists: %v", err)
	}
}
