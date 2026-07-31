//go:build darwin || linux

package journal

import (
	"fmt"
	"os"
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

func TestRecoveryRootInventoryBlocksSpecialChildInLegacyTombstone(t *testing.T) {
	recoveryRoot := filepath.Join(t.TempDir(), "recovery")
	_, result := captureInventoryJournal(t, recoveryRoot, "legacy-special-child")
	legacy := renameInventoryJournalToLegacy(t, result, "7")
	special := filepath.Join(legacy, "foreign-fifo")
	if err := unix.Mkfifo(special, uint32(retirement.RecordMode)); err != nil {
		t.Fatalf("create legacy tombstone FIFO: %v", err)
	}

	assertRecoveryInventoryBlocked(t, recoveryRoot, "unsupported entry")
	if info, err := os.Lstat(special); err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("legacy special child changed: info=%v err=%v", info, err)
	}
}

func TestRecoveryRootInventoryBlocksOverdeepLegacyTombstone(t *testing.T) {
	recoveryRoot := filepath.Join(t.TempDir(), "recovery")
	_, result := captureInventoryJournal(t, recoveryRoot, "legacy-overdeep-tree")
	deepest := renameInventoryJournalToLegacy(t, result, "8")
	for index := 0; index <= maximumRecoveryTreeDepth; index++ {
		deepest = filepath.Join(deepest, fmt.Sprintf("depth-%02d", index))
		if err := os.Mkdir(deepest, retirement.DirectoryMode); err != nil {
			t.Fatalf("create depth %d: %v", index, err)
		}
	}

	assertRecoveryInventoryBlocked(t, recoveryRoot, "maximum depth")
	if _, err := os.Lstat(deepest); err != nil {
		t.Fatalf("legacy deep tree changed: %v", err)
	}
}
