package rootedpath

import (
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf16"
)

const windowsDirectoryInfoAlignment = 8

func parseWindowsDirectoryNames(buffer []byte) ([]string, error) {
	const headerSize = 68
	var result []string
	for offset := 0; ; {
		if len(buffer)-offset < headerSize {
			return nil, fmt.Errorf("FILE_FULL_DIR_INFO header is truncated")
		}
		next := binary.LittleEndian.Uint32(buffer[offset:])
		length := binary.LittleEndian.Uint32(buffer[offset+60:])
		if length == 0 || length%2 != 0 || length > uint32(len(buffer)-offset-headerSize) {
			return nil, fmt.Errorf("directory entry name has an invalid UTF-16 span")
		}
		units := make([]uint16, int(length)/2)
		for index := range units {
			units[index] = binary.LittleEndian.Uint16(buffer[offset+headerSize+index*2:])
		}
		for index := 0; index < len(units); index++ {
			switch unit := units[index]; {
			case unit >= 0xd800 && unit <= 0xdbff:
				if index+1 == len(units) || units[index+1] < 0xdc00 || units[index+1] > 0xdfff {
					return nil, fmt.Errorf("directory entry name has an unpaired UTF-16 surrogate")
				}
				index++
			case unit >= 0xdc00 && unit <= 0xdfff:
				return nil, fmt.Errorf("directory entry name has an unpaired UTF-16 surrogate")
			}
		}
		name := string(utf16.Decode(units))
		if strings.ContainsAny(name, "/\\\x00") {
			return nil, fmt.Errorf("directory entry name cannot be represented as an immediate child")
		}
		if name != "." && name != ".." {
			result = append(result, name)
		}
		if next == 0 {
			return result, nil
		}
		if next%windowsDirectoryInfoAlignment != 0 || next < headerSize+length || next > uint32(len(buffer)-offset-headerSize) {
			return nil, fmt.Errorf("directory entry offset is outside FILE_FULL_DIR_INFO")
		}
		offset += int(next)
	}
}
