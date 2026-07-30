//go:build darwin || linux

package journal

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/retirement"
	"golang.org/x/sys/unix"
)

func TestRecoveryRootInventoryBlocksReservedSpecialFiles(t *testing.T) {
	identity := inventoryTestIdentity(t, "reserved-special", "5")
	tests := []struct {
		name string
		path func(retirement.Identity) string
	}{
		{name: "control", path: retirement.Identity.ControlName},
		{name: "residue", path: retirement.Identity.ResidueName},
		{name: "GC", path: retirement.Identity.GCName},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recoveryRoot := filepath.Join(t.TempDir(), "recovery")
			mkdirPrivate(t, recoveryRoot)
			if err := unix.Mkfifo(
				filepath.Join(recoveryRoot, test.path(identity)),
				uint32(retirement.RecordMode),
			); err != nil {
				t.Fatalf("create reserved FIFO: %v", err)
			}
			assertRecoveryInventoryBlocked(t, recoveryRoot, "must be a no-follow directory")
		})
	}
}

func TestRecoveryRootInventoryBlocksSpecialControlTemporary(t *testing.T) {
	recoveryRoot := filepath.Join(t.TempDir(), "recovery")
	identity := inventoryTestIdentity(t, "special-control-temporary", "6")
	control := writeInventoryControl(t, recoveryRoot, identity, retirement.PhaseFinalizing)
	if err := unix.Mkfifo(
		filepath.Join(control, ".daem-tmp-special"),
		uint32(retirement.RecordMode),
	); err != nil {
		t.Fatalf("create retirement temporary FIFO: %v", err)
	}

	assertRecoveryInventoryBlocked(t, recoveryRoot, "must be a no-follow regular file")
}
