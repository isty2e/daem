//go:build windows

package rootedpath

import (
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

type capturedDirectory struct {
	handle        windows.Handle
	name          string
	path          string
	object        identityToken
	mount         identityToken
	recovery      identityToken
	caseSensitive bool
}

type capturedRootPlatform struct {
	directories           []capturedDirectory
	maximumComponentUTF16 uint32
}

func captureRootPlatform(
	selectedRoot string,
	selectionMode rootSelectionMode,
	traversal *physicalTraversal,
) (string, capturedRootPlatform, identityToken, mountIdentities, error) {
	absolute, names, err := prepareWindowsAbsolutePath(selectedRoot, traversal)
	if err != nil {
		return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, err
	}
	if selectionMode != rootSelectionResolveAlias && selectionMode != rootSelectionNoFollow {
		return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, newFailure(
			FailureInvalidRoot,
			absolute,
			"root selection mode is invalid",
			nil,
		)
	}
	if selectionMode == rootSelectionResolveAlias {
		physicalRoot, platform, object, mount, missing, resolveErr := resolveDirectoryPathPlatform(
			absolute,
			false,
			traversal,
		)
		if resolveErr != nil {
			return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, resolveErr
		}
		if len(missing) != 0 {
			_ = closeCapturedRootPlatform(&platform)
			return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, newFailure(
				FailureRootUnavailable,
				absolute,
				"selected root is unavailable",
				nil,
			)
		}
		if filepath.Dir(physicalRoot) == physicalRoot {
			_ = closeCapturedRootPlatform(&platform)
			return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, newFailure(
				FailureInvalidRoot,
				physicalRoot,
				"filesystem root cannot be mutation authority",
				nil,
			)
		}
		return physicalRoot, platform, object, mount, nil
	}
	platform, err := openWindowsVolumeRoot(filepath.VolumeName(absolute))
	if err != nil {
		return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, err
	}
	fail := func(failure error) (string, capturedRootPlatform, identityToken, mountIdentities, error) {
		_ = closeCapturedRootPlatform(&platform)
		return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, failure
	}

	for _, name := range names {
		if traversal != nil {
			if err := traversal.visitComponent(); err != nil {
				return fail(err)
			}
		}
		parent := platform.directories[len(platform.directories)-1]
		if err := validatePlatformRelativeForRoot(&platform, name); err != nil {
			return fail(err)
		}
		handle, facts, openErr := openWindowsChild(parent.handle, name)
		if openErr != nil {
			return fail(windowsRootFailure(filepath.Join(parent.path, name), "open selected root component", openErr))
		}
		if facts.reparse {
			return fail(errors.Join(
				newFailure(
					FailureRootReplaced,
					filepath.Join(parent.path, name),
					"selected root contains a reparse component",
					nil,
				),
				closeWindowsHandle(handle),
			))
		}
		if !facts.isDirectory {
			return fail(errors.Join(
				newFailure(
					FailureRootReplaced,
					filepath.Join(parent.path, name),
					"selected root component is not a directory",
					nil,
				),
				closeWindowsHandle(handle),
			))
		}
		if err := validateWindowsResolvedDepth(traversal, facts); err != nil {
			return fail(errors.Join(err, closeWindowsHandle(handle)))
		}
		if facts.mount != parent.mount {
			return fail(errors.Join(
				newFailure(FailureMountChanged, facts.path, "selected root crosses an admitted volume", nil),
				closeWindowsHandle(handle),
			))
		}
		platform.directories = append(platform.directories, capturedDirectory{
			handle:        handle,
			name:          name,
			path:          facts.path,
			object:        facts.object,
			mount:         facts.mount,
			recovery:      facts.recovery,
			caseSensitive: facts.caseSensitive,
		})
	}

	if err := validateCapturedRootPlatform(&platform); err != nil {
		return fail(err)
	}
	root := platform.directories[len(platform.directories)-1]
	if filepath.Dir(root.path) == root.path {
		return fail(newFailure(FailureInvalidRoot, root.path, "filesystem root cannot be mutation authority", nil))
	}
	return root.path, platform, root.object, newMountIdentities(
		root.mount,
		availableRecoveryMountEvidence(root.recovery),
	), nil
}

type windowsChildProbe struct {
	exists  bool
	reparse bool
	handle  windows.Handle
	facts   windowsDirectoryFacts
}

type windowsReparseKind uint8

const (
	windowsReparseSymbolicLink windowsReparseKind = iota + 1
	windowsReparseMountPoint
	windowsSymlinkFlagRelative = 1
)

type windowsReparseTarget struct {
	kind     windowsReparseKind
	path     string
	relative bool
}

type windowsReparseObservation struct {
	target   windowsReparseTarget
	identity identityToken
}

type windowsReparsePath struct {
	absolute   bool
	volume     string
	components []string
}

type windowsPendingComponent struct {
	name       string
	required   bool
	frameIndex int
}

type windowsReparseFrame struct {
	parent      windows.Handle
	name        string
	path        string
	observation windowsReparseObservation
}

type windowsPendingPath struct {
	stack  []windowsPendingComponent
	frames []windowsReparseFrame
}

var (
	errWindowsReparseUnsupported = errors.New("unsupported Windows reparse target")
	errWindowsReparseMalformed   = errors.New("malformed Windows reparse target")
	errWindowsReparseChanged     = errors.New("Windows reparse target changed while resolving")
)

func newWindowsPendingPath(names []string, required bool) windowsPendingPath {
	pending := windowsPendingPath{stack: make([]windowsPendingComponent, 0, len(names))}
	pending.pushFront(names, required)
	return pending
}

func (pending *windowsPendingPath) pushFront(names []string, required bool) {
	for index := len(names) - 1; index >= 0; index-- {
		pending.stack = append(pending.stack, windowsPendingComponent{name: names[index], required: required, frameIndex: -1})
	}
}

func (pending *windowsPendingPath) pushReparseTarget(
	names []string,
	frame windowsReparseFrame,
) {
	frameIndex := len(pending.frames)
	pending.frames = append(pending.frames, frame)
	pending.stack = append(pending.stack, windowsPendingComponent{frameIndex: frameIndex})
	for index := len(names) - 1; index >= 0; index-- {
		pending.stack = append(pending.stack, windowsPendingComponent{name: names[index], required: true, frameIndex: -1})
	}
}

func (pending *windowsPendingPath) closeFrames() error {
	if pending == nil {
		return nil
	}
	var failures []error
	for index := range pending.frames {
		if err := closeWindowsHandle(pending.frames[index].parent); err != nil {
			failures = append(failures, err)
		}
		pending.frames[index].parent = windows.InvalidHandle
	}
	pending.frames = nil
	return errors.Join(failures...)
}

func (pending *windowsPendingPath) pop() (windowsPendingComponent, bool) {
	if pending == nil || len(pending.stack) == 0 {
		return windowsPendingComponent{}, false
	}
	last := len(pending.stack) - 1
	component := pending.stack[last]
	pending.stack = pending.stack[:last]
	return component, true
}

func (pending *windowsPendingPath) remainingInOrder() []string {
	if pending == nil || len(pending.stack) == 0 {
		return nil
	}
	remaining := make([]string, 0, len(pending.stack))
	for index := len(pending.stack) - 1; index >= 0; index-- {
		if pending.stack[index].name != "" && pending.stack[index].name != "." {
			remaining = append(remaining, pending.stack[index].name)
		}
	}
	return remaining
}

func admitWindowsReparseExpansion(expansions *int) error {
	if expansions == nil {
		return fmt.Errorf("Windows reparse expansion counter is required")
	}
	(*expansions)++
	if *expansions > maximumPathSymlinkExpansions {
		return fmt.Errorf("%w: too many symbolic links", errWindowsReparseMalformed)
	}
	return nil
}

func readWindowsReparseTarget(handle windows.Handle) (windowsReparseTarget, error) {
	buffer := make([]byte, windows.MAXIMUM_REPARSE_DATA_BUFFER_SIZE)
	var bytesReturned uint32
	if err := windows.DeviceIoControl(
		handle,
		windows.FSCTL_GET_REPARSE_POINT,
		nil,
		0,
		&buffer[0],
		uint32(len(buffer)),
		&bytesReturned,
		nil,
	); err != nil {
		return windowsReparseTarget{}, err
	}
	return parseWindowsReparseBuffer(buffer, bytesReturned)
}

func parseWindowsReparseBuffer(buffer []byte, bytesReturned uint32) (windowsReparseTarget, error) {
	const headerSize = 8
	if bytesReturned < headerSize || uint64(bytesReturned) > uint64(len(buffer)) {
		return windowsReparseTarget{}, fmt.Errorf("%w: invalid returned reparse length", errWindowsReparseMalformed)
	}
	dataLength := int(binary.LittleEndian.Uint16(buffer[4:6]))
	if dataLength < 0 || headerSize+dataLength > int(bytesReturned) || headerSize+dataLength > len(buffer) {
		return windowsReparseTarget{}, fmt.Errorf("%w: reparse data length exceeds returned buffer", errWindowsReparseMalformed)
	}
	data := buffer[headerSize : headerSize+dataLength]
	tag := binary.LittleEndian.Uint32(buffer[:4])
	switch tag {
	case windows.IO_REPARSE_TAG_SYMLINK:
		const metadataSize = 12
		if len(data) < metadataSize {
			return windowsReparseTarget{}, fmt.Errorf("%w: symbolic-link metadata is truncated", errWindowsReparseMalformed)
		}
		substitute, err := decodeWindowsReparseName(data, metadataSize,
			binary.LittleEndian.Uint16(data[0:2]), binary.LittleEndian.Uint16(data[2:4]), true)
		if err != nil {
			return windowsReparseTarget{}, err
		}
		if _, err := decodeWindowsReparseName(data, metadataSize,
			binary.LittleEndian.Uint16(data[4:6]), binary.LittleEndian.Uint16(data[6:8]), false); err != nil {
			return windowsReparseTarget{}, err
		}
		flags := binary.LittleEndian.Uint32(data[8:12])
		if flags&^uint32(windowsSymlinkFlagRelative) != 0 {
			return windowsReparseTarget{}, fmt.Errorf("%w: symbolic-link flags are unsupported", errWindowsReparseUnsupported)
		}
		return windowsReparseTarget{
			kind:     windowsReparseSymbolicLink,
			path:     substitute,
			relative: flags&windowsSymlinkFlagRelative != 0,
		}, nil
	case windows.IO_REPARSE_TAG_MOUNT_POINT:
		const metadataSize = 8
		if len(data) < metadataSize {
			return windowsReparseTarget{}, fmt.Errorf("%w: mount-point metadata is truncated", errWindowsReparseMalformed)
		}
		substitute, err := decodeWindowsReparseName(data, metadataSize,
			binary.LittleEndian.Uint16(data[0:2]), binary.LittleEndian.Uint16(data[2:4]), true)
		if err != nil {
			return windowsReparseTarget{}, err
		}
		if _, err := decodeWindowsReparseName(data, metadataSize,
			binary.LittleEndian.Uint16(data[4:6]), binary.LittleEndian.Uint16(data[6:8]), false); err != nil {
			return windowsReparseTarget{}, err
		}
		return windowsReparseTarget{kind: windowsReparseMountPoint, path: substitute}, nil
	default:
		return windowsReparseTarget{}, fmt.Errorf("%w: tag 0x%08x", errWindowsReparseUnsupported, tag)
	}
}

func decodeWindowsReparseName(data []byte, pathOffset int, offset uint16, length uint16, required bool) (string, error) {
	if offset%2 != 0 || length%2 != 0 {
		return "", fmt.Errorf("%w: reparse UTF-16 span is unaligned", errWindowsReparseMalformed)
	}
	start := pathOffset + int(offset)
	end := start + int(length)
	if start < pathOffset || end < start || end > len(data) {
		return "", fmt.Errorf("%w: reparse UTF-16 span is outside the record", errWindowsReparseMalformed)
	}
	if length == 0 {
		if required {
			return "", fmt.Errorf("%w: reparse substitute name is empty", errWindowsReparseMalformed)
		}
		return "", nil
	}
	units := make([]uint16, int(length)/2)
	for index := range units {
		units[index] = binary.LittleEndian.Uint16(data[start+index*2:])
	}
	for index := 0; index < len(units); index++ {
		unit := units[index]
		switch {
		case unit >= 0xd800 && unit <= 0xdbff:
			if index+1 >= len(units) || units[index+1] < 0xdc00 || units[index+1] > 0xdfff {
				return "", fmt.Errorf("%w: unpaired high surrogate", errWindowsReparseMalformed)
			}
			index++
		case unit >= 0xdc00 && unit <= 0xdfff:
			return "", fmt.Errorf("%w: unpaired low surrogate", errWindowsReparseMalformed)
		}
	}
	value := string(utf16.Decode(units))
	if strings.IndexByte(value, 0) >= 0 {
		return "", fmt.Errorf("%w: reparse UTF-16 name contains NUL", errWindowsReparseMalformed)
	}
	return value, nil
}

func observeWindowsReparse(handle windows.Handle) (windowsReparseObservation, error) {
	target, err := readWindowsReparseTarget(handle)
	if err != nil {
		return windowsReparseObservation{}, err
	}
	basic, err := queryWindowsFileBasicInfo(handle)
	if err != nil {
		return windowsReparseObservation{}, err
	}
	info, err := queryWindowsFileID(handle)
	if err != nil {
		return windowsReparseObservation{}, err
	}
	identity, err := windowsObjectTokenFromInfo(info, basic.CreationTime)
	if err != nil {
		return windowsReparseObservation{}, err
	}
	return windowsReparseObservation{target: target, identity: identity}, nil
}

func parseWindowsReparsePath(target windowsReparseTarget) (windowsReparsePath, error) {
	if target.kind != windowsReparseSymbolicLink && target.kind != windowsReparseMountPoint {
		return windowsReparsePath{}, fmt.Errorf("%w: unknown reparse target kind", errWindowsReparseUnsupported)
	}
	if target.path == "" || strings.IndexByte(target.path, 0) >= 0 {
		return windowsReparsePath{}, fmt.Errorf("%w: empty or NUL-containing target", errWindowsReparseMalformed)
	}
	value := strings.ReplaceAll(target.path, "/", `\`)
	namespacePrefix := false
	if strings.HasPrefix(value, `\??\`) {
		value = value[4:]
		namespacePrefix = true
	}
	if strings.HasPrefix(value, `\\?\`) {
		value = value[4:]
		namespacePrefix = true
	}
	upper := strings.ToUpper(value)
	if strings.HasPrefix(value, `\\`) || strings.HasPrefix(upper, `\DEVICE\`) || strings.HasPrefix(upper, `\GLOBAL??\`) {
		return windowsReparsePath{}, fmt.Errorf("%w: UNC or device target is not admitted", errWindowsReparseUnsupported)
	}
	absolute := len(value) >= 3 && isASCIIAlpha(value[0]) && value[1] == ':' && value[2] == '\\'
	if absolute {
		if target.relative {
			return windowsReparsePath{}, fmt.Errorf("%w: relative flag conflicts with absolute target", errWindowsReparseMalformed)
		}
		components := splitWindowsTargetComponents(value[3:])
		if err := validateWindowsTargetComponents(components); err != nil {
			return windowsReparsePath{}, err
		}
		return windowsReparsePath{absolute: true, volume: value[:2], components: components}, nil
	}
	if target.kind == windowsReparseMountPoint || !target.relative || namespacePrefix || len(value) >= 2 && value[1] == ':' || strings.HasPrefix(value, `\`) {
		return windowsReparsePath{}, fmt.Errorf("%w: target is not a local absolute drive or retained-parent relative path", errWindowsReparseUnsupported)
	}
	components := splitWindowsTargetComponents(value)
	if err := validateWindowsTargetComponents(components); err != nil {
		return windowsReparsePath{}, err
	}
	return windowsReparsePath{components: components}, nil
}

func validateWindowsTargetComponents(components []string) error {
	for _, component := range components {
		if component == "." || component == ".." {
			continue
		}
		if err := validateWindowsComponent(component, FailureInvalidDestination); err != nil {
			return fmt.Errorf("%w: %v", errWindowsReparseMalformed, err)
		}
	}
	return nil
}

func splitWindowsTargetComponents(value string) []string {
	parts := strings.Split(value, `\`)
	components := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		components = append(components, part)
	}
	return components
}

func probeWindowsChild(parent windows.Handle, name string) (windowsChildProbe, error) {
	handle, facts, err := openWindowsChild(parent, name)
	if err != nil {
		if windowsNotFound(err) {
			return windowsChildProbe{}, nil
		}
		if windowsReparseError(err) {
			return windowsChildProbe{}, fmt.Errorf("open reparse point without following: %w", err)
		}
		return windowsChildProbe{}, err
	}
	if facts.reparse {
		return windowsChildProbe{exists: true, reparse: true, handle: handle, facts: facts}, nil
	}
	return windowsChildProbe{exists: true, handle: handle, facts: facts}, nil
}

func openWindowsVolumeRoot(volume string) (capturedRootPlatform, error) {
	if len(volume) != 2 || !isASCIIAlpha(volume[0]) || volume[1] != ':' {
		return capturedRootPlatform{}, newFailure(FailureInvalidRoot, volume, "Windows volume must be a local drive", nil)
	}
	rootPath := volume + `\`
	name, err := windows.UTF16PtrFromString(rootPath)
	if err != nil {
		return capturedRootPlatform{}, newFailure(FailureInvalidRoot, rootPath, "encode Windows volume root", err)
	}
	handle, err := windows.CreateFile(
		name,
		windows.FILE_GENERIC_READ|windows.FILE_TRAVERSE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return capturedRootPlatform{}, windowsRootFailure(rootPath, "open Windows volume root", err)
	}
	facts, err := queryWindowsDirectoryFacts(handle)
	if err != nil {
		_ = windows.CloseHandle(handle)
		return capturedRootPlatform{}, windowsRootFailure(rootPath, "admit Windows volume root", err)
	}
	if !facts.isDirectory || facts.reparse {
		_ = windows.CloseHandle(handle)
		return capturedRootPlatform{}, newFailure(FailureUnsupportedPlatform, rootPath, "Windows volume root is not a fixed directory", nil)
	}
	return capturedRootPlatform{
		directories: []capturedDirectory{{
			handle:        handle,
			path:          facts.path,
			object:        facts.object,
			mount:         facts.mount,
			recovery:      facts.recovery,
			caseSensitive: facts.caseSensitive,
		}},
		maximumComponentUTF16: facts.maximumComponentUTF16,
	}, nil
}

func openWindowsChild(parent windows.Handle, name string) (windows.Handle, windowsDirectoryFacts, error) {
	return openWindowsChildWithAccess(parent, name, windows.FILE_GENERIC_READ|windows.FILE_TRAVERSE)
}

func openWindowsChildWithAccess(
	parent windows.Handle,
	name string,
	access uint32,
) (windows.Handle, windowsDirectoryFacts, error) {
	if err := validatePlatformComponent(name); err != nil {
		return windows.InvalidHandle, windowsDirectoryFacts{}, err
	}
	caseSensitive, err := queryWindowsCaseSensitive(parent)
	if err != nil {
		return windows.InvalidHandle, windowsDirectoryFacts{}, err
	}
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return windows.InvalidHandle, windowsDirectoryFacts{}, err
	}
	attributes := uint32(0)
	if !caseSensitive {
		attributes |= windows.OBJ_CASE_INSENSITIVE
	}
	objectAttributes := &windows.OBJECT_ATTRIBUTES{
		Length:        uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
		RootDirectory: parent,
		ObjectName:    objectName,
		Attributes:    attributes,
	}
	options := uint32(windows.FILE_OPEN_FOR_BACKUP_INTENT | windows.FILE_SYNCHRONOUS_IO_NONALERT | windows.FILE_OPEN_REPARSE_POINT)
	var handle windows.Handle
	if err := windows.NtCreateFile(
		&handle,
		access,
		objectAttributes,
		&windows.IO_STATUS_BLOCK{},
		nil,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.FILE_OPEN,
		options,
		0,
		0,
	); err != nil {
		return windows.InvalidHandle, windowsDirectoryFacts{}, err
	}
	basic, basicErr := queryWindowsFileBasicInfo(handle)
	if basicErr != nil {
		_ = windows.CloseHandle(handle)
		return windows.InvalidHandle, windowsDirectoryFacts{}, basicErr
	}
	if basic.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		path, pathErr := windowsHandlePath(handle, windowsVolumeNameDOS)
		if pathErr != nil {
			_ = windows.CloseHandle(handle)
			return windows.InvalidHandle, windowsDirectoryFacts{}, windowsCapabilityQueryError(
				"inspect Windows reparse path",
				pathErr,
			)
		}
		return handle, windowsDirectoryFacts{
			path:        normalizeWindowsFinalPath(path),
			isDirectory: basic.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0,
			reparse:     true,
		}, nil
	}
	facts, err := queryWindowsDirectoryFacts(handle)
	if err != nil {
		_ = windows.CloseHandle(handle)
		return windows.InvalidHandle, windowsDirectoryFacts{}, err
	}
	return handle, facts, nil
}

func prepareWindowsAbsolutePath(value string, traversal *physicalTraversal) (string, []string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil, newFailure(FailureInvalidRoot, value, "selected root is required", nil)
	}
	if strings.IndexFunc(value, isForbiddenPathRune) >= 0 {
		return "", nil, newFailure(FailureInvalidRoot, value, "selected root contains a control character", nil)
	}
	if traversal != nil && (!filepath.IsAbs(value) || filepath.Clean(value) != value) {
		return "", nil, newFailure(FailureInvalidRoot, value, "bounded root must use canonical absolute spelling", nil)
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", nil, newFailure(FailureRootUnavailable, value, "resolve selected root", err)
	}
	absolute = filepath.Clean(absolute)
	if err := validatePlatformPhysicalRoot(absolute); err != nil {
		return "", nil, err
	}
	volume, names, err := splitWindowsAbsolutePath(absolute)
	if err != nil {
		return "", nil, err
	}
	if volume == "" {
		return "", nil, newFailure(FailureInvalidRoot, absolute, "selected root must have a local drive", nil)
	}
	return absolute, names, nil
}

func splitWindowsAbsolutePath(value string) (string, []string, error) {
	if !filepath.IsAbs(value) {
		return "", nil, newFailure(FailureInvalidRoot, value, "path must be absolute", nil)
	}
	volume := filepath.VolumeName(value)
	if len(volume) != 2 || !isASCIIAlpha(volume[0]) || volume[1] != ':' {
		return "", nil, newFailure(FailureInvalidRoot, value, "path must use a local drive", nil)
	}
	root := volume + `\`
	clean := filepath.Clean(value)
	relative := strings.TrimPrefix(clean, root)
	if relative == "" {
		return volume, nil, nil
	}
	components := strings.Split(relative, `\`)
	for _, component := range components {
		if component == "" || component == "." || component == ".." {
			continue
		}
		if err := validateWindowsComponent(component, FailureInvalidRoot); err != nil {
			return "", nil, err
		}
	}
	return volume, components, nil
}

func resolveDirectoryPathPlatform(
	selectedPath string,
	allowMissing bool,
	traversal *physicalTraversal,
) (string, capturedRootPlatform, identityToken, mountIdentities, []string, error) {
	absolute, names, err := prepareWindowsAbsolutePath(selectedPath, traversal)
	if err != nil {
		return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, nil, err
	}
	platform, err := openWindowsVolumeRoot(filepath.VolumeName(absolute))
	if err != nil {
		return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, nil, err
	}
	fail := func(failure error) (string, capturedRootPlatform, identityToken, mountIdentities, []string, error) {
		_ = closeCapturedRootPlatform(&platform)
		return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, nil, failure
	}
	pending := newWindowsPendingPath(names, false)
	expansions := 0
	missing, resolveErr := resolveWindowsPendingPath(&platform, &pending, allowMissing, traversal, &expansions)
	if resolveErr != nil {
		return fail(errors.Join(resolveErr, pending.closeFrames()))
	}
	if closeErr := pending.closeFrames(); closeErr != nil {
		return fail(closeErr)
	}
	resolved, resolvedPlatform, object, mount, missingSuffix, resolvedErr := resolvedWindowsDirectory(platform, missing)
	if resolvedErr != nil {
		return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, nil, resolvedErr
	}
	return resolved, resolvedPlatform, object, mount, missingSuffix, nil
}

func resolveWindowsPendingPath(
	platform *capturedRootPlatform,
	pending *windowsPendingPath,
	allowMissing bool,
	traversal *physicalTraversal,
	expansions *int,
) ([]string, error) {
	if platform == nil || len(platform.directories) == 0 {
		return nil, newFailure(FailureRootUnavailable, "", "Windows root witness is not initialized", nil)
	}
	for {
		component, ok := pending.pop()
		if !ok {
			return nil, nil
		}
		if component.frameIndex >= 0 {
			if component.frameIndex >= len(pending.frames) {
				return nil, newFailure(FailureRootUnavailable, "", "Windows reparse frame is unavailable", nil)
			}
			frame := pending.frames[component.frameIndex]
			if err := verifyWindowsReparseObservation(frame.parent, frame.name, frame.observation); err != nil {
				closeErr := closeWindowsHandle(frame.parent)
				pending.frames[component.frameIndex].parent = windows.InvalidHandle
				return nil, windowsReparseFailure(frame.path, "revalidate destination reparse target", errors.Join(err, closeErr), allowMissing)
			}
			if err := closeWindowsHandle(frame.parent); err != nil {
				pending.frames[component.frameIndex].parent = windows.InvalidHandle
				return nil, windowsReparseFailure(frame.path, "close reparse parent witness", err, allowMissing)
			}
			pending.frames[component.frameIndex].parent = windows.InvalidHandle
			if err := verifyWindowsResolvedTarget(platform); err != nil {
				return nil, windowsReparseFailure(frame.path, "revalidate resolved reparse target", err, allowMissing)
			}
			continue
		}
		if traversal != nil {
			if err := traversal.visitComponent(); err != nil {
				return nil, err
			}
		}
		switch component.name {
		case "", ".":
			continue
		case "..":
			if len(platform.directories) > 1 {
				last := len(platform.directories) - 1
				if err := closeWindowsHandle(platform.directories[last].handle); err != nil {
					return nil, newFailure(FailureRootUnavailable, platform.directories[last].path, "close parent-traversed directory", err)
				}
				platform.directories = platform.directories[:last]
			}
			continue
		}

		parent := platform.directories[len(platform.directories)-1]
		if err := validatePlatformRelativeForRoot(platform, component.name); err != nil {
			return nil, err
		}
		candidatePath := filepath.Join(parent.path, component.name)
		probe, probeErr := probeWindowsChild(parent.handle, component.name)
		if probeErr != nil {
			return nil, windowsRootFailure(candidatePath, "inspect destination ancestor", probeErr)
		}
		if !probe.exists {
			if component.required || !allowMissing {
				failureKind := FailureRootUnavailable
				detail := "destination ancestor is unavailable"
				if component.required && allowMissing {
					failureKind = FailureDanglingAncestorSymlink
					detail = "destination ancestor alias target is unavailable"
				}
				return nil, newFailure(failureKind, candidatePath, detail, nil)
			}
			missing := append([]string{component.name}, pending.remainingInOrder()...)
			return missing, nil
		}
		if probe.reparse {
			observation, observeErr := observeWindowsReparse(probe.handle)
			closeErr := closeWindowsHandle(probe.handle)
			if observeErr != nil || closeErr != nil {
				return nil, windowsReparseFailure(candidatePath, "inspect destination reparse target", errors.Join(observeErr, closeErr), allowMissing)
			}
			if err := admitWindowsReparseExpansion(expansions); err != nil {
				return nil, windowsReparseFailure(candidatePath, "expand destination reparse target", err, allowMissing)
			}
			target, targetErr := parseWindowsReparsePath(observation.target)
			if targetErr != nil {
				return nil, windowsReparseFailure(candidatePath, "parse destination reparse target", targetErr, allowMissing)
			}
			parentHandle, duplicateErr := duplicateWindowsHandle(parent.handle)
			if duplicateErr != nil {
				return nil, windowsReparseFailure(candidatePath, "retain reparse parent witness", duplicateErr, allowMissing)
			}
			if target.absolute {
				if err := resetWindowsPlatformForTarget(platform, target.volume); err != nil {
					return nil, errors.Join(err, closeWindowsHandle(parentHandle))
				}
			}
			pending.pushReparseTarget(target.components, windowsReparseFrame{
				parent:      parentHandle,
				name:        component.name,
				path:        candidatePath,
				observation: observation,
			})
			continue
		}

		handle, facts := probe.handle, probe.facts
		if !facts.isDirectory {
			failureKind := FailureRootUnavailable
			if allowMissing {
				failureKind = FailureAncestorNotDirectory
			}
			return nil, errors.Join(
				newFailure(failureKind, candidatePath, "destination ancestor is not a directory", nil),
				closeWindowsHandle(handle),
			)
		}
		if err := validateWindowsResolvedDepth(traversal, facts); err != nil {
			return nil, errors.Join(err, closeWindowsHandle(handle))
		}
		if facts.mount != parent.mount {
			return nil, errors.Join(
				newFailure(FailureMountChanged, facts.path, "destination crosses an admitted volume", nil),
				closeWindowsHandle(handle),
			)
		}
		platform.directories = append(platform.directories, capturedDirectory{
			handle:        handle,
			name:          component.name,
			path:          facts.path,
			object:        facts.object,
			mount:         facts.mount,
			recovery:      facts.recovery,
			caseSensitive: facts.caseSensitive,
		})
	}
}

func windowsReparseFailure(path string, detail string, err error, allowMissing bool) error {
	kind := FailureRootUnavailable
	if errors.Is(err, errWindowsReparseUnsupported) || errors.Is(err, errMountIdentityUnsupported) {
		kind = FailureUnsupportedPlatform
	}
	if errors.Is(err, errWindowsReparseChanged) {
		kind = FailureRootReplaced
		if allowMissing {
			kind = FailureAncestorChanged
		}
	}
	return newFailure(kind, path, detail, err)
}

func verifyWindowsReparseObservation(parent windows.Handle, name string, expected windowsReparseObservation) error {
	handle, facts, err := openWindowsChild(parent, name)
	if err != nil {
		return fmt.Errorf("%w: reopen reparse point: %w", errWindowsReparseChanged, err)
	}
	if !facts.reparse {
		return errors.Join(
			fmt.Errorf("%w: reopened entry is no longer a reparse point", errWindowsReparseChanged),
			closeWindowsHandle(handle),
		)
	}
	observed, observeErr := observeWindowsReparse(handle)
	closeErr := closeWindowsHandle(handle)
	if observeErr != nil || closeErr != nil {
		return errors.Join(observeErr, closeErr)
	}
	if observed.identity != expected.identity || observed.target != expected.target {
		return fmt.Errorf("%w: reparse point incarnation or target changed", errWindowsReparseChanged)
	}
	return nil
}

func verifyWindowsResolvedTarget(platform *capturedRootPlatform) error {
	if platform == nil || len(platform.directories) < 2 {
		return nil
	}
	last := platform.directories[len(platform.directories)-1]
	parent := platform.directories[len(platform.directories)-2]
	handle, facts, err := openWindowsChild(parent.handle, last.name)
	if err != nil {
		return fmt.Errorf("%w: reopen final target component: %w", errWindowsReparseChanged, err)
	}
	if facts.reparse || facts.object != last.object {
		return errors.Join(
			fmt.Errorf("%w: final target incarnation changed", errWindowsReparseChanged),
			closeWindowsHandle(handle),
		)
	}
	if err := closeWindowsHandle(handle); err != nil {
		return fmt.Errorf("%w: close final target revalidation handle: %w", errWindowsReparseChanged, err)
	}
	return nil
}

func resetWindowsPlatformForTarget(platform *capturedRootPlatform, volume string) error {
	if platform == nil || len(platform.directories) == 0 {
		return newFailure(FailureRootUnavailable, volume, "Windows root witness is not initialized", nil)
	}
	replacement, err := openWindowsVolumeRoot(volume)
	if err != nil {
		return err
	}
	if replacement.directories[0].mount != platform.directories[0].mount {
		_ = closeCapturedRootPlatform(&replacement)
		return newFailure(FailureMountChanged, volume, "reparse target crosses an admitted volume", nil)
	}
	if closeErr := closeCapturedRootPlatform(platform); closeErr != nil {
		_ = closeCapturedRootPlatform(&replacement)
		return newFailure(FailureRootUnavailable, volume, "close superseded Windows root witness", closeErr)
	}
	*platform = replacement
	return nil
}

func validateWindowsResolvedDepth(traversal *physicalTraversal, facts windowsDirectoryFacts) error {
	if traversal == nil {
		return nil
	}
	depth, err := absolutePathDepth(facts.path)
	if err != nil {
		return err
	}
	return traversal.validateResolvedDepth(depth)
}

func resolvedWindowsDirectory(
	platform capturedRootPlatform,
	missing []string,
) (string, capturedRootPlatform, identityToken, mountIdentities, []string, error) {
	if err := validateCapturedRootPlatform(&platform); err != nil {
		_ = closeCapturedRootPlatform(&platform)
		return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, nil, err
	}
	root := platform.directories[len(platform.directories)-1]
	return root.path, platform, root.object, newMountIdentities(
		root.mount,
		availableRecoveryMountEvidence(root.recovery),
	), missing, nil
}

func windowsRootFailure(path string, detail string, err error) error {
	kind := FailureRootUnavailable
	if errors.Is(err, errMountIdentityUnsupported) {
		kind = FailureUnsupportedPlatform
	} else if windowsReparseError(err) {
		kind = FailureRootReplaced
	}
	return newFailure(kind, path, detail, err)
}

func windowsNotFound(err error) bool {
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		return true
	}
	var status windows.NTStatus
	if !errors.As(err, &status) {
		return false
	}
	return status == windows.STATUS_NO_SUCH_FILE || status == windows.STATUS_OBJECT_NAME_NOT_FOUND || status == windows.STATUS_OBJECT_PATH_NOT_FOUND
}

func windowsReparseError(err error) bool {
	if errors.Is(err, windows.ERROR_REPARSE) || errors.Is(err, windows.ERROR_REPARSE_POINT_ENCOUNTERED) {
		return true
	}
	var status windows.NTStatus
	if !errors.As(err, &status) {
		return false
	}
	return status == windows.STATUS_REPARSE_POINT_ENCOUNTERED || status == windows.STATUS_IO_REPARSE_TAG_NOT_HANDLED || status == windows.STATUS_REPARSE_POINT_NOT_RESOLVED
}

func closeWindowsHandle(handle windows.Handle) error {
	if handle == 0 || handle == windows.InvalidHandle {
		return nil
	}
	return windows.CloseHandle(handle)
}

func closeCapturedRootPlatform(platform *capturedRootPlatform) error {
	if platform == nil {
		return nil
	}
	var failures []error
	for index := len(platform.directories) - 1; index >= 0; index-- {
		if err := closeWindowsHandle(platform.directories[index].handle); err != nil {
			failures = append(failures, err)
		}
		platform.directories[index].handle = windows.InvalidHandle
	}
	platform.directories = nil
	platform.maximumComponentUTF16 = 0
	return errors.Join(failures...)
}
