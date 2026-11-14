package rootedpath

import (
	"encoding/binary"
	"slices"
	"testing"
	"unicode/utf16"
)

func TestWindowsDirectoryNamesPreserveNamesWithoutEntryIdentities(t *testing.T) {
	var buffer []byte
	for index, name := range []string{".", "..", "alpha.json", "\U0001f680.js"} {
		units := utf16.Encode([]rune(name))
		size := (68 + len(units)*2 + 7) &^ 7
		record := make([]byte, size)
		if index != 3 {
			binary.LittleEndian.PutUint32(record, uint32(size))
		}
		binary.LittleEndian.PutUint32(record[60:], uint32(len(units)*2))
		for at, unit := range units {
			binary.LittleEndian.PutUint16(record[68+at*2:], unit)
		}
		buffer = append(buffer, record...)
	}
	names, err := parseWindowsDirectoryNames(buffer)
	if err != nil || !slices.Equal(names, []string{"alpha.json", "\U0001f680.js"}) {
		t.Fatal("names-only records needed entry identity or lost Unicode data", names, err)
	}
}

func TestWindowsDirectoryNamesRejectMalformedRecordsInsteadOfFiltering(t *testing.T) {
	for _, scenario := range []string{"truncated", "empty", "unaligned", "high surrogate", "low surrogate", "invalid surrogate pair", "separator", "slash", "nul", "span overflow", "next offset"} {
		t.Run(scenario, func(t *testing.T) {
			buffer := make([]byte, 72)
			binary.LittleEndian.PutUint32(buffer[60:], 2)
			binary.LittleEndian.PutUint16(buffer[68:], 'x')
			switch scenario {
			case "truncated":
				buffer = buffer[:67]
			case "empty":
				binary.LittleEndian.PutUint32(buffer[60:], 0)
			case "unaligned":
				binary.LittleEndian.PutUint32(buffer[60:], 3)
			case "high surrogate":
				binary.LittleEndian.PutUint16(buffer[68:], 0xd800)
			case "low surrogate":
				binary.LittleEndian.PutUint16(buffer[68:], 0xdc00)
			case "invalid surrogate pair":
				binary.LittleEndian.PutUint32(buffer[60:], 4)
				binary.LittleEndian.PutUint16(buffer[68:], 0xd800)
				binary.LittleEndian.PutUint16(buffer[70:], 'x')
			case "separator":
				binary.LittleEndian.PutUint16(buffer[68:], '\\')
			case "slash":
				binary.LittleEndian.PutUint16(buffer[68:], '/')
			case "nul":
				binary.LittleEndian.PutUint16(buffer[68:], 0)
			case "span overflow":
				binary.LittleEndian.PutUint32(buffer[60:], 128)
			case "next offset":
				binary.LittleEndian.PutUint32(buffer, 2)
			}
			if names, err := parseWindowsDirectoryNames(buffer); err == nil || len(names) != 0 {
				t.Fatal("malformed directory record became valid partial names", names, err)
			}
		})
	}
}

func TestWindowsDirectoryNamesDiscardValidPrefixBeforeMalformedRecord(t *testing.T) {
	buffer := make([]byte, 144)
	binary.LittleEndian.PutUint32(buffer, 72)
	binary.LittleEndian.PutUint32(buffer[60:], 2)
	binary.LittleEndian.PutUint16(buffer[68:], 'a')
	binary.LittleEndian.PutUint32(buffer[72+60:], 128)
	if names, err := parseWindowsDirectoryNames(buffer); err == nil || len(names) != 0 {
		t.Fatal("malformed second entry exposed a valid partial prefix", names, err)
	}
}

func TestWindowsDirectoryNamesRequireAlignedRecordOffsets(t *testing.T) {
	buffer := make([]byte, 145)
	binary.LittleEndian.PutUint32(buffer, 73)
	binary.LittleEndian.PutUint32(buffer[60:], 2)
	binary.LittleEndian.PutUint16(buffer[68:], 'a')
	binary.LittleEndian.PutUint32(buffer[73+60:], 2)
	binary.LittleEndian.PutUint16(buffer[73+68:], 'b')
	if names, err := parseWindowsDirectoryNames(buffer); err == nil || len(names) != 0 {
		t.Fatal("unaligned native records became a valid directory observation", names, err)
	}
}
