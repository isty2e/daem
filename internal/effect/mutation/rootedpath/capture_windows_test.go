//go:build windows

package rootedpath

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsCaptureRootResolvesAliasButNoFollowRejectsIt(t *testing.T) {
	parent := t.TempDir()
	physical := filepath.Join(parent, "physical")
	if err := os.Mkdir(physical, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(physical, alias); err != nil {
		t.Skipf("directory symlinks are unavailable: %v", err)
	}

	captured := captureWindowsContractRoot(t, alias)
	defer captured.Close()
	resolved := mustWindowsCapturedAuthority(t, captured)
	direct := captureWindowsContractRoot(t, physical)
	defer direct.Close()
	if !resolved.Equal(mustWindowsCapturedAuthority(t, direct)) {
		t.Fatalf("alias authority %#v did not resolve to physical authority %#v", resolved, mustWindowsCapturedAuthority(t, direct))
	}
	if _, err := CaptureRootNoFollow(alias); !hasFailureKind(err, FailureRootReplaced) {
		t.Fatalf("CaptureRootNoFollow(alias) error = %v, want %s", err, FailureRootReplaced)
	}
}

func TestWindowsCaptureDestinationBoundedRetainsPhysicalAncestorAndMissingSuffix(t *testing.T) {
	rootPath := filepath.Join(t.TempDir(), "root")
	if err := os.Mkdir(rootPath, 0o700); err != nil {
		t.Fatal(err)
	}
	budget := &windowsTestBudget{}
	root, destination, err := CaptureDestinationBounded(
		filepath.Join(rootPath, "missing", "entry"),
		256,
		budget,
	)
	if err != nil {
		if hasFailureKind(err, FailureUnsupportedPlatform) {
			t.Skipf("Windows rootedpath capability unavailable: %v", err)
		}
		t.Fatal(err)
	}
	defer root.Close()
	if destination.Relative().Path() != filepath.ToSlash(filepath.Join("missing", "entry")) {
		t.Fatalf("destination relative path = %q, want missing/entry", destination.Relative().Path())
	}
	if destination.Root().PhysicalRoot() != mustWindowsCapturedAuthority(t, root).PhysicalRoot() {
		t.Fatalf("destination root = %q, captured root = %q", destination.Root().PhysicalRoot(), mustWindowsCapturedAuthority(t, root).PhysicalRoot())
	}
	if budget.calls == 0 {
		t.Fatal("bounded capture did not charge any physical path components")
	}
}

func TestWindowsCaptureDestinationBoundedChargesResolvedAliasChain(t *testing.T) {
	base := t.TempDir()
	baseDepth, err := absolutePathDepth(base)
	if err != nil {
		t.Fatal(err)
	}
	deepParent := base
	for range 5 {
		deepParent = filepath.Join(deepParent, "d")
	}
	if err := os.MkdirAll(deepParent, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(deepParent, alias); err != nil {
		t.Skipf("directory symlinks are unavailable: %v", err)
	}
	selected := filepath.Join(alias, "entry")
	_, initialComponents, err := prepareWindowsAbsolutePath(alias, nil)
	if err != nil {
		t.Fatal(err)
	}
	budget := &windowsTestBudget{limit: len(initialComponents)}
	_, _, err = CaptureDestinationBounded(selected, baseDepth+20, budget)
	if err == nil || !strings.Contains(err.Error(), "injected physical traversal budget exhausted") {
		t.Fatalf("CaptureDestinationBounded error = %v, want target-component budget rejection", err)
	}
	if budget.calls != budget.limit {
		t.Fatalf("target-component budget calls = %d, want lexical initial limit %d", budget.calls, budget.limit)
	}
	_, _, err = CaptureDestinationBounded(
		selected,
		baseDepth+3,
		&windowsTestBudget{limit: 1_000},
	)
	if err == nil || !strings.Contains(err.Error(), "physical path depth") {
		t.Fatalf("CaptureDestinationBounded error = %v, want physical-depth rejection", err)
	}
}

func TestWindowsResolvedAliasAuthorityRejectsPhysicalTargetReplacement(t *testing.T) {
	parent := t.TempDir()
	physical := filepath.Join(parent, "physical")
	if err := os.Mkdir(physical, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(physical, alias); err != nil {
		t.Skipf("directory symlinks are unavailable: %v", err)
	}
	captured := captureWindowsContractRoot(t, alias)
	defer captured.Close()
	moved := filepath.Join(parent, "moved")
	if err := os.Rename(physical, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(physical, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := captured.Authority(); !hasFailureKind(err, FailureRootReplaced) {
		t.Fatalf("resolved alias target replacement error = %v, want %s", err, FailureRootReplaced)
	}
}

func TestWindowsCapturedRootRejectsPathReplacement(t *testing.T) {
	parent := t.TempDir()
	path := filepath.Join(parent, "root")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	captured := captureWindowsContractRoot(t, path)
	defer captured.Close()

	moved := filepath.Join(parent, "moved")
	if err := os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := captured.Authority(); err != nil && !hasFailureKind(err, FailureRootReplaced) {
		t.Fatalf("captured root replacement error = %v, want %s", err, FailureRootReplaced)
	} else if err == nil {
		t.Fatal("captured root accepted a replaced path")
	}
}

func TestWindowsCapabilityCloneAndCloseIndependence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "root")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	root := captureWindowsContractRoot(t, path)
	defer root.Close()
	relative, err := NewRelativeDestination("child")
	if err != nil {
		t.Fatal(err)
	}
	destination, err := mustWindowsCapturedAuthority(t, root).Bind(relative)
	if err != nil {
		t.Fatal(err)
	}
	first, err := root.Acquire(destination)
	if err != nil {
		t.Fatal(err)
	}
	second, err := root.Acquire(destination)
	if err != nil {
		first.Close()
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := second.Validate(); err != nil {
		t.Fatalf("closing first clone invalidated second clone: %v", err)
	}
	opened, err := second.OpenRootDirectory()
	if err != nil {
		second.Close()
		t.Fatal(err)
	}
	if err := second.ValidateDirectoryHandle(opened.Fd()); err != nil {
		opened.Close()
		second.Close()
		t.Fatal(err)
	}
	if err := opened.Close(); err != nil {
		second.Close()
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if !hasFailureKind(second.Validate(), FailureRootUnavailable) {
		t.Fatalf("closed clone Validate error = %v, want %s", second.Validate(), FailureRootUnavailable)
	}
}

func TestWindowsChildProbesAreHandleRelativeAndNoFollow(t *testing.T) {
	rootPath := filepath.Join(t.TempDir(), "root")
	if err := os.Mkdir(rootPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootPath, "present"), []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	dangling := filepath.Join(rootPath, "dangling")
	if err := os.Symlink(filepath.Join(rootPath, "missing-target"), dangling); err != nil {
		t.Skipf("directory symlinks are unavailable: %v", err)
	}
	root := captureWindowsContractRoot(t, rootPath)
	defer root.Close()

	observed, err := root.ChildrenExistNoFollow(
		context.Background(),
		[2]string{"present", "dangling"},
		&windowsTestBudget{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if observed != [2]bool{true, true} {
		t.Fatalf("no-follow child probes = %v, want both entries present", observed)
	}
}

func TestWindowsAuthorityProvenanceMatchesAndRejectsReplacement(t *testing.T) {
	parent := t.TempDir()
	path := filepath.Join(parent, "root")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	first := captureWindowsContractRoot(t, path)
	provenance, err := mustWindowsCapturedAuthority(t, first).Provenance()
	if err != nil {
		first.Close()
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second := captureWindowsContractRoot(t, path)
	if err := provenance.Match(mustWindowsCapturedAuthority(t, second)); err != nil {
		second.Close()
		t.Fatalf("independent recapture did not match provenance: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}

	moved := filepath.Join(parent, "moved")
	if err := os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	replacement := captureWindowsContractRoot(t, path)
	defer replacement.Close()
	if err := provenance.Match(mustWindowsCapturedAuthority(t, replacement)); !hasFailureKind(err, FailureRootReplaced) {
		t.Fatalf("replacement provenance error = %v, want %s", err, FailureRootReplaced)
	}
}

func TestWindowsCaseSensitiveParentUsesPerDirectoryResolution(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "case-parent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := enableWindowsCaseSensitiveDirectory(parent); err != nil {
		t.Skipf("per-directory case sensitivity unavailable: %v", err)
	}
	upper := filepath.Join(parent, "Exact")
	lower := filepath.Join(parent, "exact")
	if err := os.Mkdir(upper, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(lower, 0o700); err != nil {
		t.Skipf("runner did not retain mixed-case namespace: %v", err)
	}
	first := captureWindowsContractRoot(t, upper)
	defer first.Close()
	second := captureWindowsContractRoot(t, lower)
	defer second.Close()
	if mustWindowsCapturedAuthority(t, first).Equal(mustWindowsCapturedAuthority(t, second)) {
		t.Fatal("case-sensitive parent coalesced distinct directory names")
	}
}

func TestWindowsObjectIdentityUsesVolumeFileIDAndCreationTime(t *testing.T) {
	info := windowsFileIDInfo{VolumeSerialNumber: 7, FileID: [16]byte{1}}
	first, err := windowsObjectTokenFromInfo(info, 11)
	if err != nil {
		t.Fatal(err)
	}
	second, err := windowsObjectTokenFromInfo(info, 11)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("equal Windows incarnation facts produced different object identities")
	}
	otherFile := info
	otherFile.FileID[0] = 2
	third, err := windowsObjectTokenFromInfo(otherFile, 11)
	if err != nil {
		t.Fatal(err)
	}
	if first == third {
		t.Fatal("different Windows file identifiers produced equal object identities")
	}
	otherVolume := info
	otherVolume.VolumeSerialNumber++
	fourth, err := windowsObjectTokenFromInfo(otherVolume, 11)
	if err != nil {
		t.Fatal(err)
	}
	if first == fourth {
		t.Fatal("different Windows volume identifiers produced equal object identities")
	}
	fifth, err := windowsObjectTokenFromInfo(info, 12)
	if err != nil {
		t.Fatal(err)
	}
	if first == fifth {
		t.Fatal("different Windows creation times produced equal object incarnations")
	}
}

func TestWindowsReparseTargetParserAcceptsRelativeAbsoluteAndMountTargets(t *testing.T) {
	for _, test := range []struct {
		name       string
		tag        uint32
		substitute string
		flags      uint32
		absolute   bool
		volume     string
		components []string
	}{
		{
			name:       "relative symlink",
			tag:        windows.IO_REPARSE_TAG_SYMLINK,
			substitute: `..\target`,
			flags:      windowsSymlinkFlagRelative,
			components: []string{"..", "target"},
		},
		{
			name:       "absolute symlink",
			tag:        windows.IO_REPARSE_TAG_SYMLINK,
			substitute: `\??\C:\target`,
			absolute:   true,
			volume:     "C:",
			components: []string{"target"},
		},
		{
			name:       "mount point",
			tag:        windows.IO_REPARSE_TAG_MOUNT_POINT,
			substitute: `\??\C:\target`,
			absolute:   true,
			volume:     "C:",
			components: []string{"target"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			buffer := windowsTestReparseBuffer(test.tag, test.substitute, "target", test.flags)
			decoded, err := parseWindowsReparseBuffer(buffer, uint32(len(buffer)))
			if err != nil {
				t.Fatalf("parseWindowsReparseBuffer: %v", err)
			}
			parsed, err := parseWindowsReparsePath(decoded)
			if err != nil {
				t.Fatalf("parseWindowsReparsePath: %v", err)
			}
			if parsed.absolute != test.absolute || parsed.volume != test.volume {
				t.Fatalf("parsed path = %#v, want absolute=%v volume=%q", parsed, test.absolute, test.volume)
			}
			if strings.Join(parsed.components, "|") != strings.Join(test.components, "|") {
				t.Fatalf("parsed components = %v, want %v", parsed.components, test.components)
			}
		})
	}
}

func TestWindowsReparseTargetParserRejectsMalformedUnsupportedAndRemoteTargets(t *testing.T) {
	valid := windowsTestReparseBuffer(windows.IO_REPARSE_TAG_SYMLINK, `target`, "target", windowsSymlinkFlagRelative)
	truncated := append([]byte(nil), valid...)
	binary.LittleEndian.PutUint16(truncated[4:6], uint16(len(valid)))
	if _, err := parseWindowsReparseBuffer(truncated, uint32(len(truncated))); !errors.Is(err, errWindowsReparseMalformed) {
		t.Fatalf("truncated record error = %v, want malformed", err)
	}
	unaligned := append([]byte(nil), valid...)
	binary.LittleEndian.PutUint16(unaligned[8:10], 1)
	if _, err := parseWindowsReparseBuffer(unaligned, uint32(len(unaligned))); !errors.Is(err, errWindowsReparseMalformed) {
		t.Fatalf("unaligned record error = %v, want malformed", err)
	}
	invalidSurrogate := windowsTestReparseBuffer(windows.IO_REPARSE_TAG_SYMLINK, `target`, "target", windowsSymlinkFlagRelative)
	binary.LittleEndian.PutUint16(invalidSurrogate[20:22], 0xd800)
	if _, err := parseWindowsReparseBuffer(invalidSurrogate, uint32(len(invalidSurrogate))); !errors.Is(err, errWindowsReparseMalformed) {
		t.Fatalf("invalid surrogate error = %v, want malformed", err)
	}
	unsupported := windowsTestReparseBuffer(0xa0000004, `target`, "target", 0)
	if _, err := parseWindowsReparseBuffer(unsupported, uint32(len(unsupported))); !errors.Is(err, errWindowsReparseUnsupported) {
		t.Fatalf("unsupported tag error = %v, want unsupported", err)
	}
	for _, target := range []windowsReparseTarget{
		{kind: windowsReparseSymbolicLink, path: `\\server\share`, relative: false},
		{kind: windowsReparseSymbolicLink, path: `\Device\HarddiskVolume1\target`, relative: false},
		{kind: windowsReparseSymbolicLink, path: `\device\HarddiskVolume1\target`, relative: false},
		{kind: windowsReparseSymbolicLink, path: `C:target`, relative: false},
		{kind: windowsReparseMountPoint, path: `target`, relative: false},
	} {
		if _, err := parseWindowsReparsePath(target); !errors.Is(err, errWindowsReparseUnsupported) {
			t.Fatalf("parseWindowsReparsePath(%#v) error = %v, want unsupported", target, err)
		}
	}
}

func TestWindowsReparseExpansionCeilingAndPendingTargetOrder(t *testing.T) {
	expansions := maximumPathSymlinkExpansions - 1
	if err := admitWindowsReparseExpansion(&expansions); err != nil {
		t.Fatal(err)
	}
	if err := admitWindowsReparseExpansion(&expansions); !errors.Is(err, errWindowsReparseMalformed) {
		t.Fatalf("expansion over ceiling error = %v, want malformed ceiling", err)
	}
	pending := newWindowsPendingPath([]string{"rest", "entry"}, false)
	pending.pushFront([]string{"target", "nested"}, true)
	got := make([]string, 0, 4)
	for {
		component, ok := pending.pop()
		if !ok {
			break
		}
		got = append(got, component.name)
	}
	if strings.Join(got, "|") != "target|nested|rest|entry" {
		t.Fatalf("pending target order = %v", got)
	}
}

func TestWindowsReparseTargetComponentsRejectUnsafePathNames(t *testing.T) {
	for _, target := range []windowsReparseTarget{
		{kind: windowsReparseSymbolicLink, path: `safe\bad:stream`, relative: true},
		{kind: windowsReparseSymbolicLink, path: `safe\CON`, relative: true},
		{kind: windowsReparseSymbolicLink, path: `\??\C:\safe\bad:stream`, relative: false},
	} {
		if _, err := parseWindowsReparsePath(target); !errors.Is(err, errWindowsReparseMalformed) {
			t.Fatalf("parseWindowsReparsePath(%#v) error = %v, want malformed", target, err)
		}
	}
}

func windowsTestReparseBuffer(tag uint32, substitute string, printName string, flags uint32) []byte {
	metadataSize := 8
	if tag == windows.IO_REPARSE_TAG_SYMLINK {
		metadataSize = 12
	}
	substituteUnits := utf16.Encode([]rune(substitute))
	printUnits := utf16.Encode([]rune(printName))
	payload := make([]byte, metadataSize+(len(substituteUnits)+len(printUnits))*2)
	substituteOffset := uint16(0)
	printOffset := uint16(len(substituteUnits) * 2)
	binary.LittleEndian.PutUint16(payload[0:2], substituteOffset)
	binary.LittleEndian.PutUint16(payload[2:4], uint16(len(substituteUnits)*2))
	binary.LittleEndian.PutUint16(payload[4:6], printOffset)
	binary.LittleEndian.PutUint16(payload[6:8], uint16(len(printUnits)*2))
	if tag == windows.IO_REPARSE_TAG_SYMLINK {
		binary.LittleEndian.PutUint32(payload[8:12], flags)
	}
	for index, unit := range append(substituteUnits, printUnits...) {
		binary.LittleEndian.PutUint16(payload[metadataSize+index*2:], unit)
	}
	buffer := make([]byte, 8+len(payload))
	binary.LittleEndian.PutUint32(buffer[0:4], tag)
	binary.LittleEndian.PutUint16(buffer[4:6], uint16(len(payload)))
	copy(buffer[8:], payload)
	return buffer
}

func TestWindowsRelativeDestinationUsesAdmittedUTF16ComponentLimit(t *testing.T) {
	platform := capturedRootPlatform{maximumComponentUTF16: 3}
	if err := validatePlatformRelativeForRoot(&platform, "abc/😀"); err != nil {
		t.Fatalf("in-limit Windows path rejected: %v", err)
	}
	if err := validatePlatformRelativeForRoot(&platform, "abcd"); !hasFailureKind(err, FailureInvalidDestination) {
		t.Fatalf("over-limit Windows path error = %v, want %s", err, FailureInvalidDestination)
	}
	if err := validatePlatformRelativeForRoot(&capturedRootPlatform{}, "a"); !hasFailureKind(err, FailureUnsupportedPlatform) {
		t.Fatalf("missing volume limit error = %v, want %s", err, FailureUnsupportedPlatform)
	}
}

func TestWindowsCapabilityQueryErrorSeparatesUnsupportedFromOperationalFailure(t *testing.T) {
	unsupported := windowsCapabilityQueryError("query", windows.ERROR_NOT_SUPPORTED)
	if !errors.Is(unsupported, errMountIdentityUnsupported) {
		t.Fatalf("unsupported capability error = %v, want errMountIdentityUnsupported", unsupported)
	}
	operational := windowsCapabilityQueryError("query", windows.ERROR_ACCESS_DENIED)
	if errors.Is(operational, errMountIdentityUnsupported) {
		t.Fatalf("operational query error = %v, must not be classified unsupported", operational)
	}
	if !errors.Is(operational, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("operational query error = %v, want access-denied cause", operational)
	}
}

func TestWindowsIdentityAndCapabilityRejectionHelpers(t *testing.T) {
	for _, info := range []windowsFileIDInfo{
		{},
		{VolumeSerialNumber: 1},
		{FileID: [16]byte{1}},
	} {
		if _, err := windowsObjectTokenFromInfo(info, 1); !errors.Is(err, errMountIdentityUnsupported) {
			t.Fatalf("windowsObjectTokenFromInfo(%+v) error = %v, want unsupported", info, err)
		}
	}
	if _, err := windowsObjectTokenFromInfo(
		windowsFileIDInfo{VolumeSerialNumber: 1, FileID: [16]byte{1}},
		0,
	); !errors.Is(err, errMountIdentityUnsupported) {
		t.Fatalf("zero creation time error = %v, want unsupported", err)
	}
	for _, value := range []string{
		"CON",
		"file.txt:stream",
		"trailing.",
		"trailing ",
		"NUL.txt",
	} {
		if _, err := NewRelativeDestination(value); !hasFailureKind(err, FailureInvalidDestination) {
			t.Fatalf("NewRelativeDestination(%q) error = %v, want %s", value, err, FailureInvalidDestination)
		}
	}
	for _, value := range []string{`\\server\share\root`, `\\?\C:\root`, `C:\root\CON`} {
		if _, err := CaptureRoot(value); !hasFailureKind(err, FailureInvalidRoot) {
			t.Fatalf("CaptureRoot(%q) error = %v, want %s", value, err, FailureInvalidRoot)
		}
	}
	for _, test := range []struct {
		path   string
		remote bool
	}{
		{path: `\\?\C:\root`},
		{path: `\\?\UNC\server\share`, remote: true},
		{path: `\\server\share`, remote: true},
	} {
		if got := windowsRemotePath(test.path); got != test.remote {
			t.Fatalf("windowsRemotePath(%q) = %v, want %v", test.path, got, test.remote)
		}
	}
}

type windowsTestBudget struct {
	limit int
	calls int
}

func (budget *windowsTestBudget) AdmitPathComponents(count int) error {
	if count < 0 {
		return errors.New("negative path component charge")
	}
	if budget.limit > 0 && budget.calls+count > budget.limit {
		return errors.New("injected physical traversal budget exhausted")
	}
	budget.calls += count
	return nil
}

func mustWindowsCapturedAuthority(t *testing.T, root *CapturedRoot) Authority {
	t.Helper()
	authority, err := root.Authority()
	if err != nil {
		t.Fatalf("CapturedRoot.Authority: %v", err)
	}
	return authority
}

func captureWindowsContractRoot(t *testing.T, path string) *CapturedRoot {
	t.Helper()
	root, err := CaptureRoot(path)
	if err != nil {
		if hasFailureKind(err, FailureUnsupportedPlatform) {
			t.Skipf("Windows rootedpath capability unavailable: %v", err)
		}
		t.Fatalf("CaptureRoot(%q): %v", path, err)
	}
	return root
}

func enableWindowsCaseSensitiveDirectory(path string) error {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(
		name,
		windows.FILE_GENERIC_READ|windows.FILE_WRITE_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	info := windowsFileCaseSensitiveInfo{Flags: windows.FILE_CS_FLAG_CASE_SENSITIVE_DIR}
	return windows.SetFileInformationByHandle(
		handle,
		windows.FileCaseSensitiveInfo,
		(*byte)(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
}
