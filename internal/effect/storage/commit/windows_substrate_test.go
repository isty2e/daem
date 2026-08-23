//go:build windows

package commit

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsComponentValidation(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		valid bool
	}{
		{name: "payload.bin", valid: true},
		{name: "ユニコード.txt", valid: true},
		{name: "", valid: false},
		{name: ".", valid: false},
		{name: "..", valid: false},
		{name: "payload:stream", valid: false},
		{name: "payload.", valid: false},
		{name: "payload ", valid: false},
		{name: "CON.txt", valid: false},
		{name: "com9", valid: false},
		{name: "LPT³.log", valid: false},
		{name: "nested\\name", valid: false},
		{name: "bad\x00name", valid: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateWindowsComponentName(testCase.name)
			if testCase.valid && err != nil {
				t.Fatalf("validateWindowsComponentName(%q) = %v", testCase.name, err)
			}
			if !testCase.valid && err == nil {
				t.Fatalf("validateWindowsComponentName(%q) unexpectedly succeeded", testCase.name)
			}
		})
	}
	long := strings.Repeat("a", maximumWindowsComponentUTF16+1)
	if err := validateWindowsComponentName(long); err == nil {
		t.Fatal("component exceeding UTF-16 limit unexpectedly succeeded")
	}
	component, err := parseWindowsComponent("abcd")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateWindowsComponentForVolume(component, 3); err == nil {
		t.Fatal("component exceeding the admitted volume limit unexpectedly succeeded")
	}
	if err := validateWindowsComponentForVolume(component, 4); err != nil {
		t.Fatalf("component at the admitted volume limit: %v", err)
	}
}

func TestWindowsEntryIdentitySeparatesExactStateFromLiveObjectContinuity(t *testing.T) {
	first := windowsEntryIdentityNative{
		volumeSerial: 1,
		fileID:       [16]byte{1},
		creationTime: 2,
		changeTime:   3,
	}
	changed := first
	changed.changeTime++
	if first.equal(changed) {
		t.Fatal("change-time drift preserved exact Windows entry identity")
	}
	if !first.sameObject(changed) {
		t.Fatal("change-time drift lost live Windows object continuity")
	}
	reused := changed
	reused.creationTime++
	if first.sameObject(reused) {
		t.Fatal("different creation time preserved Windows object continuity")
	}
}

func TestWindowsNativeErrorNormalization(t *testing.T) {
	cases := []struct {
		name  string
		cause error
		class windowsNativeErrorClass
	}{
		{name: "not found", cause: windows.STATUS_OBJECT_NAME_NOT_FOUND, class: windowsNativeErrorNotFound},
		{name: "collision", cause: windows.STATUS_OBJECT_NAME_COLLISION, class: windowsNativeErrorCollision},
		{name: "sharing", cause: windows.STATUS_SHARING_VIOLATION, class: windowsNativeErrorSharing},
		{name: "unsupported", cause: windows.STATUS_INVALID_INFO_CLASS, class: windowsNativeErrorUnsupported},
		{name: "EA unsupported", cause: windows.STATUS_EAS_NOT_SUPPORTED, class: windowsNativeErrorUnsupported},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := normalizeWindowsNativeError(windowsNativePhaseRename, testCase.cause, false)
			if got := windowsNativeErrorClassOf(err); got != testCase.class {
				t.Fatalf("normalized class = %v, want %v", got, testCase.class)
			}
		})
	}
	indeterminate := normalizeWindowsNativeError(windowsNativePhaseRename, errors.New("unknown"), true)
	if windowsNativeErrorClassOf(indeterminate) != windowsNativeErrorIndeterminate {
		t.Fatalf("post-visibility unknown class = %v, want indeterminate", windowsNativeErrorClassOf(indeterminate))
	}
	if !errors.Is(windowsNativeUnsupported(windowsNativePhaseIdentity, "missing identity", nil), errWindowsNativeUnsupported) {
		t.Fatal("unsupported result did not retain typed unsupported marker")
	}
}

func TestWindowsRenameInformationBuffers(t *testing.T) {
	if windowsWriteThroughOption(false) != 0 || windowsWriteThroughOption(true) != windows.FILE_WRITE_THROUGH {
		t.Fatal("write-through policy option is not exact")
	}
	component, err := parseWindowsComponent("next.txt")
	if err != nil {
		t.Fatal(err)
	}
	buffer := windowsRenameInformationBuffer(component, windows.FILE_RENAME_REPLACE_IF_EXISTS, windows.Handle(0x1234))
	pointerSize := int(unsafeSizeofWindowsHandle())
	rootOffset := alignWindowsOffset(4, pointerSize)
	lengthOffset := rootOffset + pointerSize
	nameOffset := lengthOffset + 4
	if binary.LittleEndian.Uint32(buffer[:4]) != windows.FILE_RENAME_REPLACE_IF_EXISTS {
		t.Fatalf("rename flags = 0x%x", binary.LittleEndian.Uint32(buffer[:4]))
	}
	if binary.LittleEndian.Uint32(buffer[lengthOffset:lengthOffset+4]) != uint32(len(component.units)*2) {
		t.Fatalf("rename name length = %d", binary.LittleEndian.Uint32(buffer[lengthOffset:lengthOffset+4]))
	}
	if got := buffer[nameOffset:]; !bytes.Equal(got, utf16Bytes(component.units)) {
		t.Fatalf("rename UTF-16 payload = %x, want %x", got, utf16Bytes(component.units))
	}
}

func TestWindowsStreamAndEAParsers(t *testing.T) {
	defaultStream := windowsStreamRecord("::$DATA", 0)
	namedStream := windowsStreamRecord(":secret:$DATA", 0)
	binary.LittleEndian.PutUint32(defaultStream[:4], uint32(len(defaultStream)))
	streams, err := parseWindowsStreamInformation(append(defaultStream, namedStream...))
	if err != nil {
		t.Fatal(err)
	}
	if !streams.namedStreams || len(streams.streams) != 2 {
		t.Fatalf("stream facts = %+v", streams)
	}
	if _, err := parseWindowsStreamInformation([]byte{0, 0, 0}); err == nil {
		t.Fatal("truncated stream record unexpectedly parsed")
	}
	ea, err := parseWindowsEAInformation([]byte{3, 0, 0, 0})
	if err != nil || ea.size != 3 {
		t.Fatalf("EA facts = %+v, %v", ea, err)
	}
	if _, err := parseWindowsEAInformation([]byte{1, 2}); err == nil {
		t.Fatal("truncated EA record unexpectedly parsed")
	}
}

func TestWindowsDirectoryEnumerationHonorsPreCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := enumerateWindowsDirectoryOnce(ctx, windows.InvalidHandle, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled enumeration error = %v, want context.Canceled", err)
	}
}

func TestWindowsDirectoryInformationParser(t *testing.T) {
	first := windowsDirectoryRecord("first.txt", 0)
	second := windowsDirectoryRecord("second.txt", 0)
	binary.LittleEndian.PutUint32(first[:4], uint32(len(first)))
	entries, err := parseWindowsExtendedDirectoryInformation(append(first, second...))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].name != "first.txt" || entries[1].name != "second.txt" {
		t.Fatalf("directory entries = %+v", entries)
	}
	malformed := windowsDirectoryRecord("bad", 0)
	binary.LittleEndian.PutUint32(malformed[60:64], 3)
	if _, err := parseWindowsExtendedDirectoryInformation(malformed); err == nil {
		t.Fatal("malformed directory name unexpectedly parsed")
	}
}

func TestWindowsOwnedHandleCloseIsDeterministic(t *testing.T) {
	owner, err := newWindowsOwnedHandle(windows.InvalidHandle)
	if err == nil || owner != nil {
		t.Fatalf("invalid handle owner = %#v, %v", owner, err)
	}
	root := t.TempDir()
	directory := openWindowsNativeTestDirectory(t, root)
	owner, err = duplicateWindowsOwnedHandle(directory.Handle())
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if !windowsHandleIsClosed(owner) {
		t.Fatal("closed owner did not clear its handle")
	}
}

func TestWindowsNativeIdentityPayloadAndFlush(t *testing.T) {
	root := t.TempDir()
	parent := openWindowsNativeTestDirectory(t, root)
	if _, err := queryWindowsVolumeFactsNative(parent.Handle()); err != nil {
		skipWindowsNativeCapability(t, err)
	}

	opened, err := openWindowsRelativeFile(
		parent.Handle(),
		"payload.bin",
		windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.DELETE,
		windowsPublicationShareMode,
		windows.FILE_CREATE,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeWindowsPayload(t.Context(), opened.handle.Handle(), []byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := flushWindowsHandle(opened.handle.Handle(), windowsFlushPolicy{}); err != nil {
		t.Fatal(err)
	}
	before, err := queryWindowsEntryFacts(opened.handle.Handle())
	if err != nil {
		t.Fatal(err)
	}
	if before.standard.directory || !before.identity.valid() {
		t.Fatalf("entry facts = %+v", before)
	}
	content, err := readWindowsPayloadUpTo(t.Context(), opened.handle.Handle(), 64)
	if err != nil || string(content) != "payload" {
		t.Fatalf("bounded payload = %q, %v", content, err)
	}
	if err := opened.handle.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := enumerateWindowsDirectoryOnce(t.Context(), parent.Handle(), 100)
	if err != nil {
		if errors.Is(err, errWindowsNativeUnsupported) {
			t.Skipf("extended directory enumeration unavailable: %v", err)
		}
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if entry.name == "payload.bin" {
			found = true
			if !entry.identity.valid() {
				t.Fatalf("enumerated identity = %+v", entry.identity)
			}
		}
	}
	if !found {
		t.Fatalf("directory enumeration omitted payload.bin: %+v", entries)
	}

	reopened, err := openWindowsRelativeFile(
		parent.Handle(),
		"payload.bin",
		windows.FILE_GENERIC_READ|windows.DELETE,
		windowsPublicationShareMode,
		windows.FILE_OPEN,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	after, err := queryWindowsEntryFacts(reopened.handle.Handle())
	if err != nil {
		t.Fatal(err)
	}
	if !before.identity.equal(after.identity) {
		t.Fatalf("identity changed across close/reopen: before=%+v after=%+v", before.identity, after.identity)
	}
	if err := flushWindowsHandle(parent.Handle(), windowsFlushPolicy{directory: true}); err != nil {
		t.Fatal(err)
	}
	_ = reopened.handle.Close()
}

func TestWindowsNativeDirectoryEnumerationContinuesAcrossBatches(t *testing.T) {
	root := t.TempDir()
	parent := openWindowsNativeTestDirectory(t, root)
	if _, err := queryWindowsVolumeFactsNative(parent.Handle()); err != nil {
		skipWindowsNativeCapability(t, err)
	}
	const count = 1_200
	for index := range count {
		name := fmt.Sprintf("entry-%04d", index)
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := enumerateWindowsDirectoryOnce(t.Context(), parent.Handle(), count+1)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != count {
		t.Fatalf("enumerated entries = %d, want %d", len(entries), count)
	}
}

func TestWindowsNativeNoFollowReparse(t *testing.T) {
	root := t.TempDir()
	parent := openWindowsNativeTestDirectory(t, root)
	target := filepath.Join(root, "target.txt")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink("target.txt", link); err != nil {
		if windowsNativeFeatureUnavailable(err) {
			t.Skipf("symbolic links unavailable: %v", err)
		}
		t.Fatal(err)
	}
	opened, err := openWindowsRelativeFile(
		parent.Handle(),
		"link.txt",
		windows.FILE_GENERIC_READ,
		windowsParentShareMode,
		windows.FILE_OPEN,
		false,
	)
	if err != nil {
		var status windows.NTStatus
		if errors.As(err, &status) && status == windows.STATUS_REPARSE_POINT_ENCOUNTERED {
			return
		}
		t.Fatal(err)
	}
	facts, err := queryWindowsBasicFacts(opened.handle.Handle())
	_ = opened.handle.Close()
	if err != nil {
		t.Fatal(err)
	}
	if facts.attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		t.Fatalf("no-follow open did not return the reparse entry: %#x", facts.attributes)
	}

	targetDirectory := filepath.Join(root, "target-directory")
	if err := os.Mkdir(targetDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	directoryLink := filepath.Join(root, "directory-link")
	if err := os.Symlink("target-directory", directoryLink); err != nil {
		if windowsNativeFeatureUnavailable(err) {
			t.Skipf("directory symbolic links unavailable: %v", err)
		}
		t.Fatal(err)
	}
	neutral, err := openWindowsRelativeEntry(
		parent.Handle(),
		"directory-link",
		windows.FILE_READ_ATTRIBUTES|windows.DELETE,
		windowsParentShareMode,
		windows.FILE_OPEN,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	neutralFacts, err := queryWindowsBasicFacts(neutral.handle.Handle())
	if err != nil {
		_ = neutral.handle.Close()
		t.Fatal(err)
	}
	if neutralFacts.attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		_ = neutral.handle.Close()
		t.Fatalf("neutral no-follow open did not return the directory reparse entry: %#x", neutralFacts.attributes)
	}
	if child, childErr := openWindowsRelativeEntry(
		neutral.handle.Handle(),
		"child",
		windows.FILE_READ_ATTRIBUTES,
		windowsParentShareMode,
		windows.FILE_OPEN,
		false,
	); childErr == nil {
		_ = child.handle.Close()
		_ = neutral.handle.Close()
		t.Fatal("relative open accepted a reparse parent")
	}
	if err := neutral.handle.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsNativeRenameDispositionAndSharing(t *testing.T) {
	root := t.TempDir()
	parent := openWindowsNativeTestDirectory(t, root)
	if _, err := queryWindowsVolumeFactsNative(parent.Handle()); err != nil {
		skipWindowsNativeCapability(t, err)
	}

	createTestFile := func(name, content string) {
		t.Helper()
		opened, err := openWindowsRelativeFile(
			parent.Handle(),
			name,
			windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.DELETE,
			windowsPublicationShareMode,
			windows.FILE_CREATE,
			true,
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := writeWindowsPayload(t.Context(), opened.handle.Handle(), []byte(content)); err != nil {
			t.Fatal(err)
		}
		if err := opened.handle.Close(); err != nil {
			t.Fatal(err)
		}
	}
	createTestFile("source.txt", "source")
	createTestFile("destination.txt", "destination")

	source, err := openWindowsRelativeFile(
		parent.Handle(),
		"source.txt",
		windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.DELETE,
		windowsPublicationShareMode,
		windows.FILE_OPEN,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := renameWindowsByHandle(source.handle.Handle(), parent.Handle(), "destination.txt", windowsRenameNoReplace); windowsNativeErrorClassOf(err) != windowsNativeErrorCollision {
		t.Fatalf("no-replace rename error = %v, class %v", err, windowsNativeErrorClassOf(err))
	}
	method, err := renameWindowsByHandle(source.handle.Handle(), parent.Handle(), "destination.txt", windowsRenameReplace)
	if err != nil {
		t.Fatal(err)
	}
	if method != windowsRenameMethodEx {
		t.Fatalf("replace rename method = %v, want FILE_RENAME_INFO_EX", method)
	}
	if err := source.handle.Close(); err != nil {
		t.Fatal(err)
	}
	if opened, err := openWindowsRelativeFile(parent.Handle(), "source.txt", windows.FILE_GENERIC_READ, windowsParentShareMode, windows.FILE_OPEN, false); err == nil {
		_ = opened.handle.Close()
		t.Fatal("renamed source remained visible")
	} else if windowsNativeErrorClassOf(err) != windowsNativeErrorNotFound {
		t.Fatalf("renamed source error = %v, class %v", err, windowsNativeErrorClassOf(err))
	}
	destination, err := openWindowsRelativeFile(parent.Handle(), "destination.txt", windows.FILE_GENERIC_READ|windows.DELETE, windowsParentShareMode, windows.FILE_OPEN, false)
	if err != nil {
		t.Fatal(err)
	}
	content, err := readWindowsPayloadUpTo(t.Context(), destination.handle.Handle(), 64)
	_ = destination.handle.Close()
	if err != nil || string(content) != "source" {
		t.Fatalf("replaced destination = %q, %v", content, err)
	}

	createTestFile("remove.txt", "remove")
	removable, err := openWindowsRelativeFile(parent.Handle(), "remove.txt", windows.FILE_GENERIC_READ|windows.DELETE, windowsPublicationShareMode, windows.FILE_OPEN, true)
	if err != nil {
		t.Fatal(err)
	}
	method, err = disposeWindowsByHandle(removable.handle.Handle(), false)
	if err != nil {
		t.Fatal(err)
	}
	if method != windowsRenameMethodEx {
		t.Fatalf("disposition method = %v, want FILE_DISPOSITION_INFO_EX", method)
	}
	if err := removable.handle.Close(); err != nil {
		t.Fatal(err)
	}
	if opened, err := openWindowsRelativeFile(parent.Handle(), "remove.txt", windows.FILE_GENERIC_READ, windowsParentShareMode, windows.FILE_OPEN, false); err == nil {
		_ = opened.handle.Close()
		t.Fatal("disposition did not remove the entry")
	} else if windowsNativeErrorClassOf(err) != windowsNativeErrorNotFound {
		t.Fatalf("removed entry error = %v, class %v", err, windowsNativeErrorClassOf(err))
	}

	createTestFile("shared.txt", "shared")
	writer, err := openWindowsRelativeFile(parent.Handle(), "shared.txt", windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.DELETE, windowsPublicationShareMode, windows.FILE_OPEN, false)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.handle.Close()
	if opened, err := openWindowsRelativeFile(parent.Handle(), "shared.txt", windows.FILE_GENERIC_WRITE, windowsPublicationShareMode, windows.FILE_OPEN, false); opened != nil || windowsNativeErrorClassOf(err) != windowsNativeErrorSharing {
		if opened != nil {
			_ = opened.handle.Close()
		}
		t.Fatalf("sharing probe = %v, class %v", err, windowsNativeErrorClassOf(err))
	}
}

func TestWindowsNativeSecurityAndStreamComparison(t *testing.T) {
	root := t.TempDir()
	parent := openWindowsNativeTestDirectory(t, root)
	if _, err := queryWindowsVolumeFactsNative(parent.Handle()); err != nil {
		skipWindowsNativeCapability(t, err)
	}
	filePath := filepath.Join(root, "metadata.txt")
	if err := os.WriteFile(filePath, []byte("metadata"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := openWindowsRelativeFile(parent.Handle(), "metadata.txt", windows.FILE_GENERIC_READ|windows.DELETE, windowsParentShareMode, windows.FILE_OPEN, false)
	if err != nil {
		t.Fatal(err)
	}
	first, err := queryWindowsSecurityFacts(file.handle.Handle())
	if err != nil {
		t.Fatal(err)
	}
	second, err := queryWindowsSecurityFacts(file.handle.Handle())
	if err != nil {
		t.Fatal(err)
	}
	if !compareWindowsSecurityFacts(first, second) {
		t.Fatalf("equal security captures did not compare equal: %+v %+v", first, second)
	}
	if err := file.handle.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath+":secret", []byte("secret"), 0o600); err != nil {
		if windowsNativeFeatureUnavailable(err) {
			t.Skipf("alternate data streams unavailable: %v", err)
		}
		t.Fatal(err)
	}
	file, err = openWindowsRelativeFile(parent.Handle(), "metadata.txt", windows.FILE_GENERIC_READ|windows.FILE_WRITE_EA|windows.DELETE, windowsParentShareMode, windows.FILE_OPEN, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := setWindowsTestEA(file.handle.Handle()); err != nil {
		_ = file.handle.Close()
		if windowsNativeFeatureUnavailable(err) {
			t.Skipf("extended attributes unavailable: %v", err)
		}
		t.Fatal(err)
	}
	ea, err := queryWindowsExtendedAttributeFacts(file.handle.Handle())
	if err != nil {
		_ = file.handle.Close()
		t.Fatal(err)
	}
	streams, err := queryWindowsStreamFacts(file.handle.Handle())
	_ = file.handle.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !streams.namedStreams || ea.size == 0 {
		t.Fatalf("metadata facts = streams=%+v ea=%+v", streams, ea)
	}
	if err := ensureWindowsMetadataSupported(windowsMetadataFacts{streams: streams, ea: ea}); !errors.Is(err, errWindowsNativeUnsupported) {
		t.Fatalf("unsupported metadata check = %v", err)
	}
}

func openWindowsNativeTestDirectory(t *testing.T, path string) *windowsOwnedHandle {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(
		name,
		windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.DELETE,
		windowsParentShareMode,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := newWindowsOwnedHandle(handle)
	if err != nil {
		_ = windows.CloseHandle(handle)
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	return owner
}

func skipWindowsNativeCapability(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if errors.Is(err, errWindowsNativeUnsupported) || windowsNativeFeatureUnavailable(err) {
		t.Skipf("Windows storage capability unavailable: %v", err)
	}
	t.Fatalf("Windows storage capability probe: %v", err)
}

func windowsNativeFeatureUnavailable(err error) bool {
	if errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD) || errors.Is(err, windows.ERROR_ACCESS_DENIED) ||
		errors.Is(err, windows.ERROR_NOT_SUPPORTED) || errors.Is(err, windows.ERROR_INVALID_FUNCTION) ||
		errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return true
	}
	var status windows.NTStatus
	if errors.As(err, &status) {
		return status == windows.STATUS_EAS_NOT_SUPPORTED || status == windows.STATUS_NOT_SUPPORTED ||
			status == windows.STATUS_ACCESS_DENIED || status == windows.STATUS_PRIVILEGE_NOT_HELD
	}
	return false
}

var windowsNtSetEaFile = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtSetEaFile")

func setWindowsTestEA(handle windows.Handle) error {
	name := []byte("DAEM")
	value := []byte("1")
	buffer := make([]byte, 8+len(name)+1+len(value))
	buffer[5] = byte(len(name))
	binary.LittleEndian.PutUint16(buffer[6:8], uint16(len(value)))
	copy(buffer[8:], name)
	copy(buffer[8+len(name)+1:], value)
	var statusBlock windows.IO_STATUS_BLOCK
	result, _, _ := windowsNtSetEaFile.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&statusBlock)),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
	)
	if result != 0 {
		return windows.NTStatus(result)
	}
	return nil
}

func unsafeSizeofWindowsHandle() uintptr {
	return unsafe.Sizeof(windows.Handle(0))
}

func utf16Bytes(units []uint16) []byte {
	result := make([]byte, len(units)*2)
	for index, unit := range units {
		binary.LittleEndian.PutUint16(result[index*2:], unit)
	}
	return result
}

func windowsStreamRecord(name string, next uint32) []byte {
	units := []uint16{}
	for _, unit := range []rune(name) {
		units = append(units, uint16(unit))
	}
	buffer := make([]byte, 24+len(units)*2)
	binary.LittleEndian.PutUint32(buffer[:4], next)
	binary.LittleEndian.PutUint32(buffer[4:8], uint32(len(units)*2))
	copy(buffer[24:], utf16Bytes(units))
	return buffer
}

func windowsDirectoryRecord(name string, next uint32) []byte {
	units := []uint16{}
	for _, unit := range []rune(name) {
		units = append(units, uint16(unit))
	}
	buffer := make([]byte, windowsExtendedDirectoryInfoHeaderSize+len(units)*2)
	binary.LittleEndian.PutUint32(buffer[:4], next)
	binary.LittleEndian.PutUint64(buffer[8:16], 1)
	binary.LittleEndian.PutUint64(buffer[40:48], 2)
	binary.LittleEndian.PutUint32(buffer[60:64], uint32(len(units)*2))
	for index := range buffer[72:88] {
		buffer[72+index] = byte(index + 1)
	}
	copy(buffer[windowsExtendedDirectoryInfoHeaderSize:], utf16Bytes(units))
	return buffer
}
