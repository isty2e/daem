package mutation

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type mutationLeaseHelper struct {
	command *exec.Cmd
	ready   string
}

func startMutationLeaseHelper(t *testing.T, dataDir string, access string, hold time.Duration, paths ...string) mutationLeaseHelper {
	t.Helper()
	ready := filepath.Join(t.TempDir(), "ready")
	args := []string{"-test.run=TestMutationLeaseHelperProcess", "--", dataDir, ready, access, hold.String()}
	args = append(args, paths...)
	command := exec.Command(os.Args[0], args...)
	command.Env = append(os.Environ(), "DAEM_MUTATION_LEASE_HELPER=1")
	if err := command.Start(); err != nil {
		t.Fatalf("start mutation lease helper: %v", err)
	}
	helper := mutationLeaseHelper{command: command, ready: ready}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(ready); err == nil {
			return helper
		}
		if command.ProcessState != nil && command.ProcessState.Exited() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = command.Process.Kill()
	_ = command.Wait()
	t.Fatal("mutation lease helper did not become ready")
	return mutationLeaseHelper{}
}

func stopMutationLeaseHelper(t *testing.T, helper mutationLeaseHelper) {
	t.Helper()
	if helper.command == nil || helper.command.Process == nil {
		return
	}
	_ = helper.command.Process.Kill()
	if err := helper.command.Wait(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("wait killed mutation lease helper: %v", err)
		}
	}
}

func waitMutationLeaseHelper(t *testing.T, helper mutationLeaseHelper, timeout time.Duration) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- helper.command.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("mutation lease helper failed: %v", err)
		}
	case <-time.After(timeout):
		_ = helper.command.Process.Kill()
		t.Fatal("mutation lease helper deadlocked")
	}
}

func argsAfterMutationTestSeparator(args []string) []string {
	for index, arg := range args {
		if arg == "--" {
			return args[index+1:]
		}
	}
	return nil
}

func mutationTestStore(t *testing.T) Store {
	t.Helper()
	return mutationTestStoreAt(t, t.TempDir())
}

func mutationTestStoreAt(t *testing.T, dataDir string) Store {
	t.Helper()
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func mutationTestLogicalDomain(t *testing.T, path string, access AccessMode) Domain {
	t.Helper()
	domain, err := NewLogicalPathDomain(LogicalPathRequest{Path: path, Access: access, Effect: PathEffectDirectoryEntry})
	if err != nil {
		t.Fatal(err)
	}
	return domain
}

func mutationTestRouteDomain(t *testing.T, target string, scope string, family string, containment RouteContainment) Domain {
	t.Helper()
	domain, err := NewHostRouteDomain(HostRouteRequest{Target: target, Scope: scope, Family: family, Containment: containment})
	if err != nil {
		t.Fatal(err)
	}
	return domain
}

func mutationTestPhysicalDomains(t *testing.T, path string, target string, scope string) []Domain {
	t.Helper()
	domains := make([]Domain, 0, 2)
	for _, effect := range []PathEffect{PathEffectDirectoryEntry, PathEffectReferent} {
		domain, err := NewPhysicalPathDomain(PhysicalPathRequest{
			Path: path, Access: AccessExclusive, Effect: effect,
			Target: target, Scope: scope,
		})
		if err != nil {
			t.Fatal(err)
		}
		domains = append(domains, domain)
	}
	return domains
}

func assertMutationTestDomainMode(t *testing.T, domains []normalizedDomain, key string, want AccessMode) {
	t.Helper()
	for _, domain := range domains {
		if domain.key == key {
			if domain.access != want {
				t.Fatalf("domain %q access = %d, want %d", key, domain.access, want)
			}
			return
		}
	}
	t.Fatalf("domain %q missing from %#v", key, domains)
}

func assertMutationTestDomainMissing(t *testing.T, domains []normalizedDomain, key string) {
	t.Helper()
	for _, domain := range domains {
		if domain.key == key {
			t.Fatalf("domain %q unexpectedly present", key)
		}
	}
}
