//go:build windows

package filesnapshot

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestReadRegularFileAtCountedStaysOnWindowsDirectoryHandle(t *testing.T) {
	root := t.TempDir()
	dirPath := filepath.Join(root, "plugin")
	writeWindowsTestFile(t, filepath.Join(dirPath, "plugin.json"), "inside")
	dir := openWindowsTestDirectory(t, dirPath)

	outside := filepath.Join(root, "outside")
	writeWindowsTestFile(t, filepath.Join(outside, "plugin.json"), "outside")
	if err := os.Rename(dirPath, dirPath+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(outside, dirPath); err != nil {
		t.Fatal(err)
	}

	counted, err := ReadRegularFileAtCounted(t.Context(), dir, "plugin.json", 64)
	if err != nil || !counted.Exists || string(counted.Content) != "inside" || counted.Attempted != 6 {
		t.Fatalf("ReadRegularFileAtCounted after path replacement = %+v, %v, want inside", counted, err)
	}
}

func TestReadRegularFileAtCountedRejectsWindowsEntryReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "plugin.json")
	writeWindowsTestFile(t, path, "inside")
	dir := openWindowsTestDirectory(t, root)

	_, err := readRegularFileAtCountedWithHooks(t.Context(), dir, "plugin.json", 64, readHooks{
		afterInspect: func() {
			if renameErr := os.Rename(path, path+".moved"); renameErr != nil {
				t.Fatal(renameErr)
			}
			writeWindowsTestFile(t, path, "outside")
		},
	})
	if !errors.Is(err, ErrChanged) {
		t.Fatalf("replacement error = %v, want ErrChanged", err)
	}
}

func TestWindowsFileIdentityRejectsUnprovableValues(t *testing.T) {
	for _, info := range []windowsFileIDInfo{
		{},
		{VolumeSerialNumber: 1},
		{FileID: [16]byte{1}},
	} {
		if _, err := windowsFileIDFromInfo(info); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("windowsFileIDFromInfo(%+v) error = %v, want ErrUnsupported", info, err)
		}
	}
}

func TestOpenEntryAtRejectsWindowsAlternateDataStreamSyntax(t *testing.T) {
	dir := openWindowsTestDirectory(t, t.TempDir())
	file, err := OpenEntryAt(dir, "plugin.json:secret")
	if file != nil || !errors.Is(err, ErrPathBlocked) {
		t.Fatalf("OpenEntryAt alternate data stream = %v, %v, want ErrPathBlocked", file, err)
	}
}

func openWindowsTestDirectory(t *testing.T, path string) *os.File {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(
		name,
		windows.FILE_GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		_ = windows.CloseHandle(handle)
		t.Fatal("wrap directory handle")
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func writeWindowsTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
