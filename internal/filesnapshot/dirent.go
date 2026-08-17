package filesnapshot

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

func validDirentName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("file snapshot directory entry name %q is invalid", name)
	}
	if strings.ContainsRune(name, '/') || strings.ContainsRune(name, '\\') || strings.ContainsRune(name, 0) {
		return fmt.Errorf("file snapshot directory entry name %q is invalid", name)
	}
	if !utf8.ValidString(name) {
		return fmt.Errorf("file snapshot directory entry name is not valid UTF-8")
	}
	return nil
}
