package recovery

import (
	"fmt"
	"io/fs"
)

// TreeModes follows the same preorder as the verified backup digest, starting
// with the root. Each entry is three octal digits; none can name a destination.
type TreeModes string

func PackTreeModes(modes []fs.FileMode) TreeModes {
	encoded := make([]byte, 0, 3*len(modes))
	for _, mode := range modes {
		bits := byte(mode.Perm() >> 6)
		encoded = append(encoded, '0'+bits, '0'+byte(mode>>3&7), '0'+byte(mode&7))
	}
	return TreeModes(encoded)
}

func (packed TreeModes) Unpack() ([]fs.FileMode, error) {
	if len(packed) == 0 || len(packed)%3 != 0 || len(packed)/3 > MaximumArtifactTreeEntries+1 {
		return nil, fmt.Errorf("invalid backup tree permission count")
	}
	modes := make([]fs.FileMode, len(packed)/3)
	for i := 0; i < len(packed); i++ {
		if digit := packed[i]; digit >= '0' && digit <= '7' {
			modes[i/3] = modes[i/3]<<3 | fs.FileMode(digit-'0')
		} else {
			return nil, fmt.Errorf("invalid octal backup tree permissions")
		}
	}
	return modes, nil
}
