//go:build windows

package filesnapshot

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	for _, testCase := range []struct {
		name             string
		holdWriter       bool
		replaceAfterRead bool
	}{
		{name: "closed replacement"},
		{name: "writer-held replacement after read", holdWriter: true, replaceAfterRead: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "plugin.json")
			writeWindowsTestFile(t, path, "inside")
			dir := openWindowsTestDirectory(t, root)

			replaceEntry := func() {
				if renameErr := os.Rename(path, path+".moved"); renameErr != nil {
					t.Fatal(renameErr)
				}
				writeWindowsTestFile(t, path, "outside")
				if !testCase.holdWriter {
					return
				}
				writer, writerErr := openWindowsTestWriter(path)
				if writerErr != nil {
					t.Fatal(writerErr)
				}
				t.Cleanup(func() { _ = windows.CloseHandle(writer) })
			}
			hooks := readHooks{afterInspect: replaceEntry}
			if testCase.replaceAfterRead {
				hooks = readHooks{afterRead: replaceEntry}
			}

			counted, err := readRegularFileAtCountedWithHooks(t.Context(), dir, "plugin.json", 64, hooks)
			if !errors.Is(err, ErrChanged) {
				t.Fatalf("replacement snapshot = %+v, %v, want ErrChanged", counted, err)
			}
			if counted.Exists || counted.Attempted != 6 || len(counted.Content) != 0 {
				t.Fatalf("replacement snapshot = %+v, want attempted original bytes only", counted)
			}
		})
	}
}

func TestReadRegularFileAtCountedExcludesWindowsConcurrentSameSizeWriter(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "plugin.json")
	original := strings.Repeat("a", 64*1024)
	writeWindowsTestFile(t, path, original)
	dir := openWindowsTestDirectory(t, root)

	var writerErr error
	counted, err := readRegularFileAtCountedWithHooks(t.Context(), dir, "plugin.json", int64(len(original)), readHooks{
		afterInspect: func() {
			var writer windows.Handle
			writer, writerErr = openWindowsTestWriter(path)
			if writerErr != nil {
				return
			}
			defer windows.CloseHandle(writer)

			replacement := bytes.Repeat([]byte{'b'}, len(original))
			var written uint32
			writerErr = windows.WriteFile(writer, replacement, &written, nil)
			if writerErr == nil && int(written) != len(replacement) {
				writerErr = errors.New("short same-size Windows test write")
			}
		},
	})
	if !errors.Is(writerErr, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("concurrent writer error = %v, want ERROR_SHARING_VIOLATION", writerErr)
	}
	if err != nil || !counted.Exists || string(counted.Content) != original {
		t.Fatalf("snapshot with excluded writer = %+v, %v", counted, err)
	}
}

func TestReadRegularFileAtCountedRejectsExistingWindowsWriter(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "plugin.json")
	writeWindowsTestFile(t, path, "inside")
	dir := openWindowsTestDirectory(t, root)
	writer, err := openWindowsTestWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = windows.CloseHandle(writer) })

	counted, err := ReadRegularFileAtCounted(t.Context(), dir, "plugin.json", 64)
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("snapshot with existing writer = %+v, %v, want sharing violation", counted, err)
	}
	if counted.Exists || counted.Attempted != 0 || len(counted.Content) != 0 {
		t.Fatalf("snapshot with existing writer = %+v, want zero evidence", counted)
	}
}

func TestReadRegularFileContextCountedRejectsExistingWindowsWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plugin.json")
	writeWindowsTestFile(t, path, "inside")
	writer, err := openWindowsTestWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = windows.CloseHandle(writer) })

	counted, err := ReadRegularFileContextCounted(t.Context(), path, 64)
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("pathname snapshot with existing writer = %+v, %v, want sharing violation", counted, err)
	}
	if counted.Exists || counted.Attempted != 0 || len(counted.Content) != 0 {
		t.Fatalf("pathname snapshot with existing writer = %+v, want zero evidence", counted)
	}
}

func TestReadRegularFileAtCountedHonorsCancellationBeforeSuccessOnWindows(t *testing.T) {
	root := t.TempDir()
	writeWindowsTestFile(t, filepath.Join(root, "plugin.json"), "inside")
	dir := openWindowsTestDirectory(t, root)
	ctx, cancel := context.WithCancel(context.Background())

	counted, err := readRegularFileAtCountedWithHooks(ctx, dir, "plugin.json", 64, readHooks{
		beforeSuccess: cancel,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("final validation cancellation = %+v, %v, want context.Canceled", counted, err)
	}
	if counted.Exists || counted.Attempted != 6 || len(counted.Content) != 0 {
		t.Fatalf("final validation cancellation = %+v, want attempted bytes without content", counted)
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

func openWindowsTestWriter(path string) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	return windows.CreateFile(
		name,
		windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
}
