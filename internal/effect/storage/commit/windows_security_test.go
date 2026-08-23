//go:build windows

package commit

import (
	"errors"
	"io/fs"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsEntryAttributeGrammar(t *testing.T) {
	const (
		windowsFileAttributePinned   = uint32(0x00080000)
		windowsFileAttributeUnpinned = uint32(0x00100000)
	)
	allowedFile := uint32(windows.FILE_ATTRIBUTE_NORMAL)
	for _, attributes := range []uint32{windows.FILE_ATTRIBUTE_NORMAL, windows.FILE_ATTRIBUTE_ARCHIVE} {
		if err := validateWindowsObservedEntryAttributes(attributes, false); err != nil {
			t.Fatalf("file attributes 0x%x rejected: %v", attributes, err)
		}
	}
	if err := validateWindowsObservedEntryAttributes(windows.FILE_ATTRIBUTE_DIRECTORY, true); err != nil {
		t.Fatal(err)
	}
	for _, attributes := range []uint32{
		windows.FILE_ATTRIBUTE_READONLY,
		windows.FILE_ATTRIBUTE_HIDDEN,
		windows.FILE_ATTRIBUTE_SYSTEM,
		windows.FILE_ATTRIBUTE_TEMPORARY,
		windows.FILE_ATTRIBUTE_COMPRESSED,
		windows.FILE_ATTRIBUTE_ENCRYPTED,
		windows.FILE_ATTRIBUTE_SPARSE_FILE,
		windows.FILE_ATTRIBUTE_OFFLINE,
		windows.FILE_ATTRIBUTE_INTEGRITY_STREAM,
		windows.FILE_ATTRIBUTE_NO_SCRUB_DATA,
		windowsFileAttributePinned,
		windowsFileAttributeUnpinned,
		windows.FILE_ATTRIBUTE_RECALL_ON_OPEN,
		windows.FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS,
		windows.FILE_ATTRIBUTE_VIRTUAL,
		windows.FILE_ATTRIBUTE_REPARSE_POINT,
	} {
		if err := validateWindowsObservedEntryAttributes(allowedFile|attributes, false); err == nil {
			t.Fatalf("unsupported attribute 0x%x unexpectedly admitted", attributes)
		}
	}
	if err := validateWindowsObservedEntryAttributes(windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_ATTRIBUTE_ARCHIVE, false); err == nil {
		t.Fatal("normal/archive combination unexpectedly admitted")
	}
	if err := validateWindowsObservedEntryAttributes(windows.FILE_ATTRIBUTE_NORMAL, true); err == nil {
		t.Fatal("normal file attribute unexpectedly admitted as directory")
	}
}

func TestWindowsCanonicalModeGrammar(t *testing.T) {
	const (
		ownerSID    = "S-1-5-21-100-200-300-1000"
		groupSID    = "S-1-5-21-100-200-300-513"
		everyoneSID = "S-1-1-0"
	)
	for _, testCase := range []struct {
		name     string
		mode     fs.FileMode
		valid    bool
		owner    windows.ACCESS_MASK
		group    windows.ACCESS_MASK
		everyone windows.ACCESS_MASK
	}{
		{
			name:     "0600",
			mode:     0o600,
			valid:    true,
			owner:    windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE | windows.WRITE_DAC | windows.WRITE_OWNER | windows.DELETE,
			group:    0,
			everyone: 0,
		},
		{
			name:     "0644",
			mode:     0o644,
			valid:    true,
			owner:    windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE | windows.WRITE_DAC | windows.WRITE_OWNER | windows.DELETE,
			group:    windows.FILE_GENERIC_READ,
			everyone: windows.FILE_GENERIC_READ,
		},
		{name: "other wider than group", mode: 0o604, valid: false},
		{name: "group wider than owner", mode: 0o460, valid: false},
		{name: "file-mode metadata", mode: fs.ModeDir | 0o700, valid: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			grammar, err := canonicalWindowsDACLGrammar(testCase.mode, ownerSID, groupSID, everyoneSID)
			if !testCase.valid {
				if err == nil {
					t.Fatal("invalid canonical mode unexpectedly succeeded")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(grammar.entries) != 3 || grammar.entries[0].sid != ownerSID ||
				grammar.entries[1].sid != groupSID || grammar.entries[2].sid != everyoneSID {
				t.Fatalf("canonical principal order = %+v", grammar.entries)
			}
			if grammar.entries[0].mask != testCase.owner || grammar.entries[1].mask != testCase.group ||
				grammar.entries[2].mask != testCase.everyone {
				t.Fatalf("canonical masks = %+v", grammar.entries)
			}
			for _, entry := range grammar.entries {
				if entry.type_ != windows.ACCESS_ALLOWED_ACE_TYPE || entry.flags != 0 {
					t.Fatalf("canonical ACE = %+v", entry)
				}
			}
		})
	}
	if _, err := canonicalWindowsDACLGrammar(0o600, ownerSID, ownerSID, everyoneSID); err == nil {
		t.Fatal("duplicate owner/group SIDs unexpectedly succeeded")
	}
	if _, err := canonicalWindowsDACLGrammar(0o600, "foreign", groupSID, everyoneSID); err == nil {
		t.Fatal("malformed owner SID unexpectedly succeeded")
	}
	if err := validateWindowsCanonicalFileMode(0o400); err == nil {
		t.Fatal("read-only file mode unexpectedly entered the recoverable Windows profile")
	}
	if err := validateWindowsCanonicalDirectoryMode(0o500); err == nil {
		t.Fatal("write-disabled directory mode unexpectedly entered the durable Windows profile")
	}
}

func TestWindowsCanonicalDACLGrammarRoundTrip(t *testing.T) {
	principals := windowsCanonicalSecurityPrincipals{
		ownerSID:    "S-1-5-21-100-200-300-1000",
		groupSID:    "S-1-5-21-100-200-300-513",
		everyoneSID: "S-1-1-0",
	}
	owner, err := windows.StringToSid(principals.ownerSID)
	if err != nil {
		t.Fatal(err)
	}
	group, err := windows.StringToSid(principals.groupSID)
	if err != nil {
		t.Fatal(err)
	}
	everyone, err := windows.StringToSid(principals.everyoneSID)
	if err != nil {
		t.Fatal(err)
	}
	principals.owner, principals.group, principals.everyone = owner, group, everyone
	grammar, err := canonicalWindowsDACLGrammar(0o644, principals.ownerSID, principals.groupSID, principals.everyoneSID)
	if err != nil {
		t.Fatal(err)
	}
	acl, err := buildWindowsCanonicalDACL(grammar, principals)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := parseWindowsDACLFact(acl)
	if err != nil {
		t.Fatal(err)
	}
	if facts.revision != windowsCanonicalACLRevision || len(facts.aces) != 3 {
		t.Fatalf("parsed DACL facts = %+v", facts)
	}
	for index, entry := range grammar.entries {
		got := facts.aces[index]
		if got.sid != entry.sid || got.mask != entry.mask || got.type_ != entry.type_ || got.flags != entry.flags {
			t.Fatalf("ACE %d = %+v, want %+v", index, got, entry)
		}
	}
}

func TestWindowsCanonicalSecurityFactRejections(t *testing.T) {
	base := windowsSecurityFacts{
		ownerSID:    "S-1-5-21-owner",
		groupSID:    "S-1-5-21-group",
		control:     windows.SE_DACL_PRESENT | windows.SE_DACL_PROTECTED | windows.SE_SELF_RELATIVE,
		daclPresent: true,
		dacl: windowsDACLFact{
			revision: windowsCanonicalACLRevision,
			size:     windowsACLHeaderSize + 4,
			aceCount: 1,
			raw:      []byte{windowsCanonicalACLRevision, 0, windowsACLHeaderSize + 4, 0, 1, 0, 0, 0, 1, 2, 3, 4},
			aces:     []windowsACEFact{{sid: "S-1-1-0", type_: windows.ACCESS_ALLOWED_ACE_TYPE, size: 4}},
		},
	}
	if err := validateWindowsObservedSecurityFacts(base); err != nil {
		t.Fatalf("ordinary observation rejected explicit DACL facts: %v", err)
	}
	if err := validateWindowsCanonicalSecurityFacts(base, base); err != nil {
		t.Fatalf("canonical baseline rejected itself: %v", err)
	}
	for _, testCase := range []struct {
		name   string
		mutate func(*windowsSecurityFacts)
	}{
		{name: "foreign owner", mutate: func(facts *windowsSecurityFacts) { facts.ownerSID = "S-1-5-18" }},
		{name: "null DACL", mutate: func(facts *windowsSecurityFacts) { facts.daclNull = true }},
		{name: "non-allow ACE", mutate: func(facts *windowsSecurityFacts) { facts.dacl.aces[0].type_ = windows.ACCESS_DENIED_ACE_TYPE }},
		{name: "inherited ACE", mutate: func(facts *windowsSecurityFacts) { facts.dacl.aces[0].flags = windows.INHERITED_ACE }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			actual := base
			actual.dacl.aces = append([]windowsACEFact(nil), base.dacl.aces...)
			testCase.mutate(&actual)
			if err := validateWindowsCanonicalSecurityFacts(actual, base); !errors.Is(err, errWindowsNativeUnsupported) {
				t.Fatalf("rejection = %v, want typed unsupported", err)
			}
		})
	}
}

func TestWindowsCanonicalSecurityNativeRoundTrip(t *testing.T) {
	root := t.TempDir()
	parent := openWindowsNativeTestDirectory(t, root)
	if _, err := queryWindowsVolumeFactsNative(parent.Handle()); err != nil {
		skipWindowsNativeCapability(t, err)
	}
	initial, err := buildWindowsCanonicalSecurity(0o600)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := createWindowsRelativeFile(
		parent.Handle(),
		"canonical-security.txt",
		windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.FILE_GENERIC_EXECUTE|windows.DELETE|windows.WRITE_DAC,
		windowsPublicationShareMode,
		true,
		initial.descriptor,
	)
	if err != nil {
		t.Fatalf("create file with canonical security: %v", err)
	}
	defer opened.handle.Close()
	created, err := queryWindowsSecurityFacts(opened.handle.Handle())
	if err != nil {
		t.Fatal(err)
	}
	if err := validateWindowsCanonicalSecurityFacts(created, initial.facts); err != nil {
		t.Fatalf("creation-time canonical security: %v\nactual: %#v\nexpected: %#v", err, created, initial.facts)
	}

	for _, mode := range []fs.FileMode{0o600, 0o644} {
		expected, err := buildWindowsCanonicalSecurity(mode)
		if err != nil {
			t.Fatalf("build canonical security for %04o: %v", mode, err)
		}
		actual, err := applyWindowsCanonicalSecurity(opened.handle.Handle(), mode)
		if err != nil {
			t.Fatalf("apply canonical security for %04o: %v", mode, err)
		}
		if !actual.equal(expected.facts) {
			t.Fatalf("canonical security facts did not round-trip for %04o: actual=%+v expected=%+v", mode, actual, expected.facts)
		}
		observedMode, err := windowsCanonicalModeFromSecurity(actual)
		if err != nil {
			t.Fatalf("derive canonical mode for %04o: %v", mode, err)
		}
		if observedMode != mode {
			t.Fatalf("derived canonical mode = %04o, want %04o", observedMode, mode)
		}
	}

	expected, err := buildWindowsCanonicalSecurity(0o600)
	if err != nil {
		t.Fatal(err)
	}

	inheritance := uint32(windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE)
	inheritedACL, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{
		{
			AccessPermissions: expected.facts.dacl.aces[0].mask,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       inheritance,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_USER,
				TrusteeValue: windows.TrusteeValueFromSID(expected.principals.owner),
			},
		},
		{
			AccessPermissions: expected.facts.dacl.aces[1].mask,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       inheritance,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_GROUP,
				TrusteeValue: windows.TrusteeValueFromSID(expected.principals.group),
			},
		},
		{
			AccessPermissions: expected.facts.dacl.aces[2].mask,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       inheritance,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_WELL_KNOWN_GROUP,
				TrusteeValue: windows.TrusteeValueFromSID(expected.principals.everyone),
			},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetSecurityInfo(
		opened.handle.Handle(),
		windows.SE_FILE_OBJECT,
		windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION|windows.UNPROTECTED_DACL_SECURITY_INFORMATION),
		nil,
		nil,
		inheritedACL,
		nil,
	); err != nil {
		t.Fatalf("install inherited/unprotected DACL: %v", err)
	} else {
		facts, queryErr := queryWindowsSecurityFacts(opened.handle.Handle())
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		if err := validateWindowsCanonicalSecurityFacts(facts, expected.facts); !errors.Is(err, errWindowsNativeUnsupported) {
			t.Fatalf("inherited/unprotected DACL validation = %v, want typed rejection", err)
		}
		if _, err := applyWindowsCanonicalSecurity(opened.handle.Handle(), 0o600); err != nil {
			t.Fatalf("restore canonical security after inherited DACL: %v", err)
		}
	}

	if err := windows.SetSecurityInfo(
		opened.handle.Handle(),
		windows.SE_FILE_OBJECT,
		windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION),
		nil,
		nil,
		nil,
		nil,
	); err != nil {
		t.Fatalf("install null protected DACL: %v", err)
	} else {
		facts, queryErr := queryWindowsSecurityFacts(opened.handle.Handle())
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		if err := validateWindowsCanonicalSecurityFacts(facts, expected.facts); !errors.Is(err, errWindowsNativeUnsupported) {
			t.Fatalf("null DACL validation = %v, want typed rejection", err)
		}
	}
}
