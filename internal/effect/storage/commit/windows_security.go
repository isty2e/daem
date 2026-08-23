//go:build windows

package commit

import (
	"encoding/binary"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	windowsCanonicalACLRevision = 2
	windowsACLHeaderSize        = 8
	windowsAllowedACEType       = windows.ACCESS_ALLOWED_ACE_TYPE
	windowsCanonicalACEFlags    = uint8(0)
)

const windowsSecurityControlMask = windows.SE_OWNER_DEFAULTED |
	windows.SE_GROUP_DEFAULTED |
	windows.SE_DACL_PRESENT |
	windows.SE_DACL_DEFAULTED |
	windows.SE_DACL_PROTECTED |
	windows.SE_SELF_RELATIVE

type windowsACEFact struct {
	sid   string
	mask  windows.ACCESS_MASK
	type_ uint8
	flags uint8
	size  uint16
}

type windowsDACLFact struct {
	revision byte
	size     uint16
	aceCount uint16
	raw      []byte
	aces     []windowsACEFact
}

func (facts windowsDACLFact) equal(other windowsDACLFact) bool {
	if facts.revision != other.revision || facts.aceCount != other.aceCount || len(facts.aces) != len(other.aces) {
		return false
	}
	for index := range facts.aces {
		if facts.aces[index] != other.aces[index] {
			return false
		}
	}
	return true
}

type windowsCanonicalSecurityPrincipals struct {
	owner    *windows.SID
	group    *windows.SID
	everyone *windows.SID

	ownerSID    string
	groupSID    string
	everyoneSID string
}

type windowsCanonicalACEGrammar struct {
	sid   string
	mask  windows.ACCESS_MASK
	type_ uint8
	flags uint8
}

type windowsCanonicalDACLGrammar struct {
	revision byte
	entries  []windowsCanonicalACEGrammar
}

type windowsCanonicalSecurity struct {
	principals windowsCanonicalSecurityPrincipals
	dacl       *windows.ACL
	descriptor *windows.SECURITY_DESCRIPTOR
	facts      windowsSecurityFacts
}

func validateWindowsObservedSecurityFacts(facts windowsSecurityFacts) error {
	if facts.ownerSID == "" || facts.groupSID == "" {
		return windowsNativeUnsupported(windowsNativePhaseSecurity, "owner and group SIDs are required", nil)
	}
	if !facts.daclPresent || facts.daclNull || facts.dacl.aceCount == 0 {
		return windowsNativeUnsupported(windowsNativePhaseSecurity, "a non-null explicit DACL is required", nil)
	}
	return nil
}

func validateWindowsCanonicalSecurityFacts(actual, expected windowsSecurityFacts) error {
	if err := validateWindowsObservedSecurityFacts(actual); err != nil {
		return err
	}
	if actual.control&windows.SE_DACL_PROTECTED == 0 ||
		actual.control&windows.SE_DACL_DEFAULTED != 0 ||
		actual.ownerDefaulted || actual.groupDefaulted || actual.daclDefaulted {
		return windowsNativeUnsupported(windowsNativePhaseSecurity, "security descriptor is inherited or defaulted", nil)
	}
	if !actual.equal(expected) {
		return windowsNativeUnsupported(windowsNativePhaseSecurity, "applied security descriptor did not retain the canonical grammar", nil)
	}
	return nil
}

func canonicalWindowsDACLGrammar(
	mode fs.FileMode,
	ownerSID string,
	groupSID string,
	everyoneSID string,
) (windowsCanonicalDACLGrammar, error) {
	if err := validateWindowsCanonicalMode(mode); err != nil {
		return windowsCanonicalDACLGrammar{}, err
	}
	if !validWindowsSIDString(ownerSID) || !validWindowsSIDString(groupSID) || !validWindowsSIDString(everyoneSID) {
		return windowsCanonicalDACLGrammar{}, fmt.Errorf("canonical DACL SIDs are invalid")
	}
	if strings.EqualFold(ownerSID, groupSID) || strings.EqualFold(ownerSID, everyoneSID) ||
		strings.EqualFold(groupSID, everyoneSID) {
		return windowsCanonicalDACLGrammar{}, fmt.Errorf("canonical DACL SIDs must be distinct")
	}
	owner, group, other := windowsModeTriples(mode)
	return windowsCanonicalDACLGrammar{
		revision: windowsCanonicalACLRevision,
		entries: []windowsCanonicalACEGrammar{
			{
				sid:   ownerSID,
				mask:  windowsModeRights(owner) | windows.WRITE_DAC | windows.WRITE_OWNER | windows.DELETE,
				type_: windowsAllowedACEType,
				flags: windowsCanonicalACEFlags,
			},
			{
				sid:   groupSID,
				mask:  windowsModeRights(group),
				type_: windowsAllowedACEType,
				flags: windowsCanonicalACEFlags,
			},
			{
				sid:   everyoneSID,
				mask:  windowsModeRights(other),
				type_: windowsAllowedACEType,
				flags: windowsCanonicalACEFlags,
			},
		},
	}, nil
}

func validWindowsSIDString(value string) bool {
	parts := strings.Split(value, "-")
	if len(parts) < 3 || parts[0] != "S" {
		return false
	}
	revision, err := strconv.ParseUint(parts[1], 10, 8)
	if err != nil || revision != 1 {
		return false
	}
	if _, err := strconv.ParseUint(parts[2], 10, 48); err != nil {
		return false
	}
	if len(parts) > 18 {
		return false
	}
	for _, part := range parts[3:] {
		if _, err := strconv.ParseUint(part, 10, 32); err != nil {
			return false
		}
	}
	return true
}

func validateWindowsCanonicalMode(mode fs.FileMode) error {
	if mode&^fs.ModePerm != 0 {
		return fmt.Errorf("Windows canonical security mode must contain permission bits only")
	}
	owner, group, other := windowsModeTriples(mode)
	if group&^owner != 0 || other&^group != 0 {
		return fmt.Errorf("Windows canonical security mode must monotonically narrow owner, group, and other rights")
	}
	return nil
}

func validateWindowsCanonicalFileMode(mode fs.FileMode) error {
	if err := validateWindowsCanonicalMode(mode); err != nil {
		return err
	}
	owner, _, _ := windowsModeTriples(mode)
	if owner&6 != 6 {
		return fmt.Errorf("Windows canonical files require owner read and write permissions for verified recovery")
	}
	return nil
}

func validateWindowsCanonicalDirectoryMode(mode fs.FileMode) error {
	if err := validateWindowsCanonicalMode(mode); err != nil {
		return err
	}
	owner, _, _ := windowsModeTriples(mode)
	if owner&7 != 7 {
		return fmt.Errorf("Windows canonical directories require owner read, write, and traversal permissions")
	}
	return nil
}

func windowsModeTriples(mode fs.FileMode) (owner, group, other fs.FileMode) {
	permissions := mode.Perm()
	return (permissions >> 6) & 7, (permissions >> 3) & 7, permissions & 7
}

func windowsModeRights(permission fs.FileMode) windows.ACCESS_MASK {
	var rights windows.ACCESS_MASK
	if permission&4 != 0 {
		rights |= windows.FILE_GENERIC_READ
	}
	if permission&2 != 0 {
		rights |= windows.FILE_GENERIC_WRITE
	}
	if permission&1 != 0 {
		rights |= windows.FILE_GENERIC_EXECUTE
	}
	return rights
}

func windowsPermissionFromRights(rights windows.ACCESS_MASK) (fs.FileMode, error) {
	for permission := fs.FileMode(0); permission <= 7; permission++ {
		if windowsModeRights(permission) == rights {
			return permission, nil
		}
	}
	return 0, windowsNativeUnsupported(
		windowsNativePhaseSecurity,
		"DACL ACE contains rights outside the canonical mode grammar",
		nil,
	)
}

func windowsCanonicalModeFromSecurity(facts windowsSecurityFacts) (fs.FileMode, error) {
	principals, err := currentWindowsCanonicalSecurityPrincipals()
	if err != nil {
		return 0, err
	}
	if len(facts.dacl.aces) != 3 || facts.ownerSID != principals.ownerSID || facts.groupSID != principals.groupSID {
		return 0, windowsNativeUnsupported(
			windowsNativePhaseSecurity,
			"security descriptor principals are outside the canonical mode grammar",
			nil,
		)
	}
	ownerRights := facts.dacl.aces[0].mask
	const ownerControl = windows.WRITE_DAC | windows.WRITE_OWNER | windows.DELETE
	if ownerRights&ownerControl != ownerControl {
		return 0, windowsNativeUnsupported(
			windowsNativePhaseSecurity,
			"owner ACE lacks canonical control rights",
			nil,
		)
	}
	owner, err := windowsPermissionFromRights(ownerRights &^ ownerControl)
	if err != nil {
		return 0, err
	}
	group, err := windowsPermissionFromRights(facts.dacl.aces[1].mask)
	if err != nil {
		return 0, err
	}
	other, err := windowsPermissionFromRights(facts.dacl.aces[2].mask)
	if err != nil {
		return 0, err
	}
	mode := owner<<6 | group<<3 | other
	expected, err := buildWindowsCanonicalSecurity(mode)
	if err != nil {
		return 0, err
	}
	if err := validateWindowsCanonicalSecurityFacts(facts, expected.facts); err != nil {
		return 0, err
	}
	return mode, nil
}

func currentWindowsCanonicalSecurityPrincipals() (windowsCanonicalSecurityPrincipals, error) {
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return windowsCanonicalSecurityPrincipals{}, normalizeWindowsNativeError(windowsNativePhaseSecurity, err, false)
	}
	primary, err := token.GetTokenPrimaryGroup()
	if err != nil {
		return windowsCanonicalSecurityPrincipals{}, normalizeWindowsNativeError(windowsNativePhaseSecurity, err, false)
	}
	if user == nil || primary == nil {
		return windowsCanonicalSecurityPrincipals{}, windowsNativeUnsupported(
			windowsNativePhaseSecurity,
			"process token principal data is unavailable",
			nil,
		)
	}
	owner, ownerSID, err := copyWindowsCanonicalSID(user.User.Sid, "process token user")
	if err != nil {
		return windowsCanonicalSecurityPrincipals{}, err
	}
	group, groupSID, err := copyWindowsCanonicalSID(primary.PrimaryGroup, "process token primary group")
	if err != nil {
		return windowsCanonicalSecurityPrincipals{}, err
	}
	everyone, everyoneSID, err := copyWindowsCanonicalSIDFromWellKnown(windows.WinWorldSid, "Everyone")
	if err != nil {
		return windowsCanonicalSecurityPrincipals{}, err
	}
	if windows.EqualSid(owner, group) || windows.EqualSid(owner, everyone) || windows.EqualSid(group, everyone) {
		return windowsCanonicalSecurityPrincipals{}, windowsNativeUnsupported(
			windowsNativePhaseSecurity,
			"canonical DACL principals are not distinct",
			nil,
		)
	}
	return windowsCanonicalSecurityPrincipals{
		owner:       owner,
		group:       group,
		everyone:    everyone,
		ownerSID:    ownerSID,
		groupSID:    groupSID,
		everyoneSID: everyoneSID,
	}, nil
}

func copyWindowsCanonicalSID(sid *windows.SID, role string) (*windows.SID, string, error) {
	if sid == nil || !sid.IsValid() {
		return nil, "", windowsNativeUnsupported(windowsNativePhaseSecurity, role+" SID is invalid", nil)
	}
	copy, err := sid.Copy()
	if err != nil {
		return nil, "", normalizeWindowsNativeError(windowsNativePhaseSecurity, err, false)
	}
	text := copy.String()
	if text == "" || !copy.IsValid() {
		return nil, "", windowsNativeUnsupported(windowsNativePhaseSecurity, role+" SID cannot be represented", nil)
	}
	return copy, text, nil
}

func copyWindowsCanonicalSIDFromWellKnown(
	sidType windows.WELL_KNOWN_SID_TYPE,
	role string,
) (*windows.SID, string, error) {
	sid, err := windows.CreateWellKnownSid(sidType)
	if err != nil {
		return nil, "", normalizeWindowsNativeError(windowsNativePhaseSecurity, err, false)
	}
	return copyWindowsCanonicalSID(sid, role)
}

func buildWindowsCanonicalDACL(
	grammar windowsCanonicalDACLGrammar,
	principals windowsCanonicalSecurityPrincipals,
) (*windows.ACL, error) {
	if grammar.revision != windowsCanonicalACLRevision || len(grammar.entries) != 3 {
		return nil, windowsNativeUnsupported(windowsNativePhaseSecurity, "canonical DACL grammar has the wrong shape", nil)
	}
	sids := []*windows.SID{principals.owner, principals.group, principals.everyone}
	for index, entry := range grammar.entries {
		if entry.sid == "" || entry.type_ != windowsAllowedACEType || entry.flags != windowsCanonicalACEFlags ||
			!strings.EqualFold(entry.sid, []string{principals.ownerSID, principals.groupSID, principals.everyoneSID}[index]) {
			return nil, windowsNativeUnsupported(windowsNativePhaseSecurity, "canonical DACL grammar contains an invalid ACE", nil)
		}
		if sids[index] == nil || !sids[index].IsValid() {
			return nil, windowsNativeUnsupported(windowsNativePhaseSecurity, "canonical DACL contains an invalid SID", nil)
		}
	}
	length := windowsACLHeaderSize
	for _, sid := range sids {
		length += alignWindowsOffset(int(unsafe.Offsetof(windows.ACCESS_ALLOWED_ACE{}.SidStart))+sid.Len(), 4)
	}
	if length > int(^uint16(0)) {
		return nil, windowsNativeUnsupported(windowsNativePhaseSecurity, "canonical DACL is too large", nil)
	}
	buffer := make([]byte, length)
	buffer[0] = grammar.revision
	binary.LittleEndian.PutUint16(buffer[2:4], uint16(length))
	binary.LittleEndian.PutUint16(buffer[4:6], uint16(len(grammar.entries)))
	offset := windowsACLHeaderSize
	for index, entry := range grammar.entries {
		sid := sids[index]
		aceSize := alignWindowsOffset(int(unsafe.Offsetof(windows.ACCESS_ALLOWED_ACE{}.SidStart))+sid.Len(), 4)
		buffer[offset] = entry.type_
		buffer[offset+1] = entry.flags
		binary.LittleEndian.PutUint16(buffer[offset+2:offset+4], uint16(aceSize))
		binary.LittleEndian.PutUint32(buffer[offset+4:offset+8], uint32(entry.mask))
		sidBytes := unsafe.Slice((*byte)(unsafe.Pointer(sid)), sid.Len())
		copy(buffer[offset+int(unsafe.Offsetof(windows.ACCESS_ALLOWED_ACE{}.SidStart)):], sidBytes)
		offset += aceSize
	}
	return (*windows.ACL)(unsafe.Pointer(&buffer[0])), nil
}

func buildWindowsCanonicalSecurity(mode fs.FileMode) (*windowsCanonicalSecurity, error) {
	principals, err := currentWindowsCanonicalSecurityPrincipals()
	if err != nil {
		return nil, err
	}
	grammar, err := canonicalWindowsDACLGrammar(mode, principals.ownerSID, principals.groupSID, principals.everyoneSID)
	if err != nil {
		return nil, err
	}
	dacl, err := buildWindowsCanonicalDACL(grammar, principals)
	if err != nil {
		return nil, err
	}
	absolute, err := windows.NewSecurityDescriptor()
	if err != nil {
		return nil, normalizeWindowsNativeError(windowsNativePhaseSecurity, err, false)
	}
	if err := absolute.SetOwner(principals.owner, false); err != nil {
		return nil, normalizeWindowsNativeError(windowsNativePhaseSecurity, err, false)
	}
	if err := absolute.SetGroup(principals.group, false); err != nil {
		return nil, normalizeWindowsNativeError(windowsNativePhaseSecurity, err, false)
	}
	if err := absolute.SetDACL(dacl, true, false); err != nil {
		return nil, normalizeWindowsNativeError(windowsNativePhaseSecurity, err, false)
	}
	if err := absolute.SetControl(windows.SE_DACL_PROTECTED, windows.SE_DACL_PROTECTED); err != nil {
		return nil, normalizeWindowsNativeError(windowsNativePhaseSecurity, err, false)
	}
	selfRelative, err := absolute.ToSelfRelative()
	if err != nil {
		return nil, normalizeWindowsNativeError(windowsNativePhaseSecurity, err, false)
	}
	facts, err := windowsSecurityFactsFromDescriptor(selfRelative)
	if err != nil {
		return nil, err
	}
	if err := validateWindowsCanonicalSecurityFacts(facts, facts); err != nil {
		return nil, err
	}
	return &windowsCanonicalSecurity{
		principals: principals,
		dacl:       dacl,
		descriptor: selfRelative,
		facts:      facts,
	}, nil
}

func applyWindowsCanonicalSecurity(handle windows.Handle, mode fs.FileMode) (windowsSecurityFacts, error) {
	if handle == 0 || handle == windows.InvalidHandle {
		return windowsSecurityFacts{}, fmt.Errorf("Windows security apply handle is required")
	}
	standard, err := queryWindowsStandardFacts(handle)
	if err != nil {
		return windowsSecurityFacts{}, err
	}
	if standard.directory {
		err = validateWindowsCanonicalDirectoryMode(mode)
	} else {
		err = validateWindowsCanonicalFileMode(mode)
	}
	if err != nil {
		return windowsSecurityFacts{}, windowsNativeUnsupported(windowsNativePhaseSecurity, "mode is outside the recoverable Windows profile", err)
	}
	canonical, err := buildWindowsCanonicalSecurity(mode)
	if err != nil {
		return windowsSecurityFacts{}, err
	}
	before, err := queryWindowsSecurityFacts(handle)
	if err != nil {
		return windowsSecurityFacts{}, err
	}
	if before.ownerSID != canonical.principals.ownerSID || before.groupSID != canonical.principals.groupSID {
		return windowsSecurityFacts{}, windowsNativeUnsupported(
			windowsNativePhaseSecurity,
			"entry owner or primary group is outside the canonical creation profile",
			nil,
		)
	}
	securityInformation := windows.SECURITY_INFORMATION(
		windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION,
	)
	if err := windows.SetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		securityInformation,
		nil,
		nil,
		canonical.dacl,
		nil,
	); err != nil {
		return windowsSecurityFacts{}, normalizeWindowsNativeError(windowsNativePhaseSecurity, err, false)
	}
	actual, err := queryWindowsSecurityFacts(handle)
	if err != nil {
		return windowsSecurityFacts{}, err
	}
	if err := validateWindowsCanonicalSecurityFacts(actual, canonical.facts); err != nil {
		return actual, err
	}
	return actual, nil
}

func parseWindowsDACLFact(acl *windows.ACL) (windowsDACLFact, error) {
	if acl == nil {
		return windowsDACLFact{}, windowsNativeUnsupported(windowsNativePhaseSecurity, "null DACL is not observable metadata", nil)
	}
	header := unsafe.Slice((*byte)(unsafe.Pointer(acl)), windowsACLHeaderSize)
	aclSize := binary.LittleEndian.Uint16(header[2:4])
	aceCount := binary.LittleEndian.Uint16(header[4:6])
	if int(aclSize) < windowsACLHeaderSize {
		return windowsDACLFact{}, windowsNativeUnsupported(windowsNativePhaseSecurity, "DACL size is invalid", nil)
	}
	raw := append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(acl)), int(aclSize))...)
	facts := windowsDACLFact{
		revision: header[0],
		size:     aclSize,
		aceCount: aceCount,
		raw:      raw,
		aces:     make([]windowsACEFact, 0, aceCount),
	}
	offset := windowsACLHeaderSize
	for index := uint16(0); index < aceCount; index++ {
		if offset+int(unsafe.Offsetof(windows.ACCESS_ALLOWED_ACE{}.SidStart)) > int(aclSize) {
			return windowsDACLFact{}, windowsNativeUnsupported(windowsNativePhaseSecurity, "DACL ACE header is truncated", nil)
		}
		aceSize := int(binary.LittleEndian.Uint16(raw[offset+2 : offset+4]))
		if aceSize < int(unsafe.Offsetof(windows.ACCESS_ALLOWED_ACE{}.SidStart)) ||
			aceSize%4 != 0 || offset+aceSize > int(aclSize) {
			return windowsDACLFact{}, windowsNativeUnsupported(windowsNativePhaseSecurity, "DACL ACE size is invalid", nil)
		}
		ace := windowsACEFact{
			mask:  windows.ACCESS_MASK(binary.LittleEndian.Uint32(raw[offset+4 : offset+8])),
			type_: raw[offset],
			flags: raw[offset+1],
			size:  uint16(aceSize),
		}
		if windowsACEHasInlineSID(ace.type_) {
			sid := (*windows.SID)(unsafe.Pointer(&raw[offset+int(unsafe.Offsetof(windows.ACCESS_ALLOWED_ACE{}.SidStart))]))
			if !sid.IsValid() || sid.Len() <= 0 || int(unsafe.Offsetof(windows.ACCESS_ALLOWED_ACE{}.SidStart))+sid.Len() > aceSize {
				return windowsDACLFact{}, windowsNativeUnsupported(windowsNativePhaseSecurity, "DACL ACE SID is invalid", nil)
			}
			text := sid.String()
			if text == "" {
				return windowsDACLFact{}, windowsNativeUnsupported(windowsNativePhaseSecurity, "DACL ACE SID cannot be represented", nil)
			}
			expectedSize := alignWindowsOffset(int(unsafe.Offsetof(windows.ACCESS_ALLOWED_ACE{}.SidStart))+sid.Len(), 4)
			if ace.type_ == windows.ACCESS_ALLOWED_ACE_TYPE && aceSize != expectedSize {
				return windowsDACLFact{}, windowsNativeUnsupported(windowsNativePhaseSecurity, "DACL ACE contains unrepresentable data", nil)
			}
			ace.sid = text
		}
		facts.aces = append(facts.aces, ace)
		offset += aceSize
	}
	if offset != int(aclSize) {
		return windowsDACLFact{}, windowsNativeUnsupported(windowsNativePhaseSecurity, "DACL contains trailing or missing data", nil)
	}
	return facts, nil
}

func windowsACEHasInlineSID(aceType uint8) bool {
	switch aceType {
	case windows.ACCESS_ALLOWED_ACE_TYPE, windows.ACCESS_DENIED_ACE_TYPE, 2, 9, 10, 11:
		return true
	default:
		return false
	}
}

func copyWindowsSelfRelativeDescriptor(descriptor *windows.SECURITY_DESCRIPTOR) ([]byte, error) {
	control, _, err := descriptor.Control()
	if err != nil {
		return nil, normalizeWindowsNativeError(windowsNativePhaseSecurity, err, false)
	}
	if control&windows.SE_SELF_RELATIVE == 0 {
		return nil, windowsNativeUnsupported(windowsNativePhaseSecurity, "security descriptor is not self-relative", nil)
	}
	length := int(descriptor.Length())
	if length < int(unsafe.Sizeof(windows.SECURITY_DESCRIPTOR{})) {
		return nil, windowsNativeUnsupported(windowsNativePhaseSecurity, "self-relative security descriptor length is invalid", nil)
	}
	raw := unsafe.Slice((*byte)(unsafe.Pointer(descriptor)), length)
	return append([]byte(nil), raw...), nil
}
