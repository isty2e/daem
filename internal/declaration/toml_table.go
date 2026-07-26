package declaration

import (
	"reflect"
	"strings"

	burnttoml "github.com/BurntSushi/toml"
)

type TableHeader struct {
	Segments []string
	Array    bool
}

// ParseTableHeader reads a single TOML table-header line through BurntSushi/toml.
func ParseTableHeader(trimmedLine string) (TableHeader, bool) {
	line := strings.TrimSpace(trimmedLine)
	if !strings.HasPrefix(line, "[") {
		return TableHeader{}, false
	}

	var decoded map[string]any
	if _, err := burnttoml.Decode(line+"\n", &decoded); err != nil {
		return TableHeader{}, false
	}
	segments, array, ok := singleHeaderPath(decoded)
	if !ok {
		return TableHeader{}, false
	}
	return TableHeader{Segments: segments, Array: array}, true
}

// StartsArrayTableRoot reports whether a line opens a root array table such as [[skill]].
func StartsArrayTableRoot(trimmedLine string, root string) bool {
	header, ok := ParseTableHeader(trimmedLine)
	return ok && header.Array && len(header.Segments) == 1 && header.Segments[0] == root
}

// StartsTableOutsideRoot reports whether a table-header line exits the current root table family.
func StartsTableOutsideRoot(trimmedLine string, root string) bool {
	header, ok := ParseTableHeader(trimmedLine)
	if !ok {
		return false
	}
	if len(header.Segments) == 0 || header.Segments[0] != root {
		return true
	}
	return len(header.Segments) == 1
}

func singleHeaderPath(decoded map[string]any) ([]string, bool, bool) {
	if len(decoded) != 1 {
		return nil, false, false
	}
	for key, value := range decoded {
		segments, array, ok := tableValuePath(value)
		if !ok {
			return nil, false, false
		}
		return append([]string{key}, segments...), array, true
	}
	return nil, false, false
}

func tableValuePath(value any) ([]string, bool, bool) {
	if isEmptyArrayTable(value) {
		return nil, true, true
	}
	children, ok := value.(map[string]any)
	if !ok {
		return nil, false, false
	}
	if len(children) == 0 {
		return nil, false, true
	}
	if len(children) != 1 {
		return nil, false, false
	}
	for key, child := range children {
		segments, array, ok := tableValuePath(child)
		if !ok {
			return nil, false, false
		}
		return append([]string{key}, segments...), array, true
	}
	return nil, false, false
}

func isEmptyArrayTable(value any) bool {
	reflected := reflect.ValueOf(value)
	if !reflected.IsValid() || reflected.Kind() != reflect.Slice || reflected.Len() != 1 {
		return false
	}
	first := reflected.Index(0)
	if first.Kind() == reflect.Interface {
		first = first.Elem()
	}
	return first.IsValid() && first.Kind() == reflect.Map && first.Len() == 0
}
