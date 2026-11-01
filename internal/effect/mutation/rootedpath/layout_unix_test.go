//go:build darwin || linux

package rootedpath_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

type layoutTestBudget struct {
	paths, entries        int
	bytes                 int64
	pathLimit, entryLimit int
	byteLimit             int64
}

func layoutBudget() *layoutTestBudget {
	return &layoutTestBudget{pathLimit: 1 << 26, entryLimit: 400_000, byteLimit: 16 << 30}
}

func (budget *layoutTestBudget) AdmitPathComponents(count int) error {
	if count < 0 || count > budget.pathLimit-budget.paths {
		return fmt.Errorf("test path allocation exhausted")
	}
	budget.paths += count
	return nil
}

func (budget *layoutTestBudget) AdmitPhysicalWork(paths, entries int, bytes int64) error {
	if entries < 0 || entries > budget.entryLimit-budget.entries || bytes < 0 || bytes > budget.byteLimit-budget.bytes {
		return fmt.Errorf("test artifact allocation exhausted")
	}
	if err := budget.AdmitPathComponents(paths); err != nil {
		return err
	}
	budget.entries += entries
	budget.bytes += bytes
	return nil
}

func TestPathLayoutProjectionBoundsRepeatedAliasObservations(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	alias := filepath.Join(root, "alias")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", alias); err != nil {
		t.Fatal(err)
	}
	outer := filepath.Join(root, "absolute")
	if err := os.Symlink(alias, outer); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(outer, "config")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	layout, err := rootedpath.CaptureReferentLayoutBounded(path, 256, layoutBudget())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	projection := layoutBudget()
	if err := layout.ReserveObservation(256, projection); err != nil {
		t.Fatal("pure projection consulted the removed alias", err)
	}
	if err := os.Symlink("real", alias); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		allocation := &layoutTestBudget{pathLimit: projection.paths, entryLimit: projection.entries, byteLimit: projection.bytes}
		parent, suffix, err := layout.ResolveBounded(256, allocation)
		if err != nil || filepath.Join(parent, filepath.Join(suffix...)) != layout.CanonicalPath() {
			t.Fatal("projected observation failed", err)
		}
		if allocation.paths != projection.paths || allocation.entries != projection.entries || allocation.bytes != projection.bytes {
			t.Fatalf("stable layout work differs: actual=%+v projected=%+v", allocation, projection)
		}
	}
	for _, dimension := range []string{"paths", "entries", "bytes"} {
		allocation := &layoutTestBudget{pathLimit: projection.paths, entryLimit: projection.entries, byteLimit: projection.bytes}
		switch dimension {
		case "paths":
			allocation.pathLimit--
		case "entries":
			allocation.entryLimit--
		case "bytes":
			allocation.byteLimit--
		}
		if _, _, err := layout.ResolveBounded(256, allocation); err == nil {
			t.Fatalf("short %s allocation was replenished", dimension)
		}
	}
	if err := os.WriteFile(filepath.Join(real, "replacement"), []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(real, "replacement"), filepath.Join(real, "config")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := layout.ResolveBounded(256, layoutBudget()); err != nil {
		t.Fatal("new regular-file inode was treated as changed layout", err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	if _, _, err := layout.ResolveBounded(256, layoutBudget()); err == nil {
		t.Fatal("changed alias representation bypassed prepared layout")
	}
}

func TestPathLayoutAllowsMissingSuffixCreationButNotNewAliases(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "new", "nested")
	path := filepath.Join(parent, "config")
	layout, err := rootedpath.CaptureDirectoryEntryLayoutBounded(path, 256, layoutBudget())
	if err != nil {
		t.Fatal(err)
	}
	projection := layoutBudget()
	if err := layout.ReserveObservation(256, projection); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	allocation := &layoutTestBudget{pathLimit: projection.paths, entryLimit: projection.entries, byteLimit: projection.bytes}
	if _, _, err := layout.ResolveBounded(256, allocation); err != nil {
		t.Fatal("ordinary planned parent creation invalidated layout", err)
	}
	if err := os.Rename(parent, parent+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(parent+"-old", parent); err != nil {
		t.Fatal(err)
	}
	if _, _, err := layout.ResolveBounded(256, layoutBudget()); err == nil {
		t.Fatal("new ancestor alias was followed during prepared observation")
	}
}

func TestPathLayoutDistinguishesLeafEntryFromNewReferentAlias(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	entry, err := rootedpath.CaptureDirectoryEntryLayoutBounded(path, 256, layoutBudget())
	if err != nil {
		t.Fatal(err)
	}
	referent, err := rootedpath.CaptureReferentLayoutBounded(path, 256, layoutBudget())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("config", path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := entry.ResolveBounded(256, layoutBudget()); err != nil {
		t.Fatal("entry observation followed the final alias", err)
	}
	if _, _, err := referent.ResolveBounded(256, layoutBudget()); err == nil {
		t.Fatal("referent observation followed a new alias loop")
	}
	if _, _, err := (rootedpath.PathLayout{}).ResolveBounded(256, layoutBudget()); err == nil {
		t.Fatal("zero layout was accepted")
	}
}

func TestPathLayoutRejectsAReplacedExistingParent(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	layout, err := rootedpath.CaptureDirectoryEntryLayoutBounded(filepath.Join(parent, "config"), 256, layoutBudget())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(parent, parent+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := layout.ResolveBounded(256, layoutBudget()); err == nil {
		t.Fatal("new parent inode was treated as the prepared root")
	}
}

func TestPathLayoutProjectsFreshDestinationCapture(t *testing.T) {
	root := t.TempDir()
	real, alias := filepath.Join(root, "real"), filepath.Join(root, "alias")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", alias); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(alias, "nested", "config")
	layout, err := rootedpath.CaptureDirectoryEntryLayoutBounded(path, 256, layoutBudget())
	if err != nil {
		t.Fatal(err)
	}
	projection := layoutBudget()
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := layout.ReserveDestinationCapture(256, projection); err != nil {
		t.Fatal("capture projection performed filesystem I/O", err)
	}
	if err := os.Symlink("real", alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(real, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	allocation := &layoutTestBudget{pathLimit: projection.paths, entryLimit: projection.entries, byteLimit: projection.bytes}
	captured, destination, err := layout.CaptureDestinationBounded(256, allocation)
	if err != nil {
		t.Fatal("fresh destination exceeded its projected work", err)
	}
	defer captured.Close()
	physical, err := destination.LexicalPath()
	if err != nil || physical != layout.CanonicalPath() {
		t.Fatal("captured destination escaped the prepared namespace", err)
	}
	capability, err := captured.AcquireBounded(destination, 256, layoutBudget())
	if err != nil {
		t.Fatal("fresh capture did not retain native authority", err)
	}
	if err := capability.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("alias", alias); err != nil {
		t.Fatal(err)
	}
	if unexpected, _, err := layout.CaptureDestinationBounded(256, layoutBudget()); err == nil {
		unexpected.Close()
		t.Fatal("fresh destination followed an unprepared alias loop")
	}
}
