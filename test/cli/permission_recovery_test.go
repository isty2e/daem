//go:build darwin || linux

package cli_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/isty2e/daem/test/ownershiptest"
)

func TestFreshCLIProcessRecoversPersistedPermissionsWithoutSourceOrJournal(t *testing.T) {
	fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml", ".pi/settings.json")
	beforeRegistry, _ := os.ReadFile(fixture.Store.Path())
	beforeState, _ := os.ReadFile(fixture.Paths.StatefilePath)
	beforePayload, _ := os.ReadFile(fixture.Documents[0])
	plan := startWorkspaceMutationHelper(t, []string{"recover", "--dry-run", "--manifest", fixture.Paths.ManifestPath})
	t.Cleanup(plan.kill)
	plan.start(t)
	if err := waitWorkspaceMutationHelper(t, plan); err != nil {
		t.Fatalf("fresh planning process failed: %v stderr=%s", err, plan.stderr.String())
	}
	afterRegistry, _ := os.ReadFile(fixture.Store.Path())
	info, err := os.Stat(fixture.Documents[0])
	if err != nil || !bytes.Equal(beforeRegistry, afterRegistry) || info.Mode().Perm() != 0o644 {
		t.Fatal("fresh dry-run changed the protection or host mode", err)
	}
	write := startWorkspaceMutationHelper(t, []string{"recover", "--yes", "--manifest", fixture.Paths.ManifestPath})
	t.Cleanup(write.kill)
	write.start(t)
	if err := waitWorkspaceMutationHelper(t, write); err != nil {
		t.Fatalf("fresh permission recovery failed: %v stderr=%s", err, write.stderr.String())
	}
	for _, path := range fixture.Documents {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatal("fresh process did not contain the document", err)
		}
	}
	afterState, _ := os.ReadFile(fixture.Paths.StatefilePath)
	afterPayload, _ := os.ReadFile(fixture.Documents[0])
	if !bytes.Equal(beforeState, afterState) || !bytes.Equal(beforePayload, afterPayload) {
		t.Fatal("fresh permission recovery rewrote payloads")
	}
	rows, err := fixture.Store.LoadPendingProtections(t.Context())
	if err != nil || len(rows) != 0 {
		t.Fatal("fresh process retained confirmed-settled authority", err)
	}
}
