package declaration

import (
	"errors"
	"fmt"
	"testing"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
)

func TestUnknownManifestKeyPositionPreservesSelectedKeyAndError(t *testing.T) {
	inlinePrefix := "instructions.guide = { source=\"notes.md\", "
	arrayPrefix := "skill = [{name=\"one\", "
	for _, test := range []struct {
		name    string
		content string
		key     toml.Key
		line    int
		column  int
	}{
		{"root", "version=1\n  bogus=true\n", toml.Key{"bogus"}, 2, 3},
		{"group", "version=1\n [ bogus . \"dotted.key\" ]\nflag=true\n", toml.Key{"bogus", "dotted.key"}, 2, 4},
		{"dotted", "version=1\n instructions.guide.bogus=true\n", toml.Key{"instructions", "guide", "bogus"}, 2, 2},
		{"inline", "version=1\n" + inlinePrefix + "bogus=true}\n", toml.Key{"instructions", "guide", "bogus"}, 2, utf8.RuneCountInString(inlinePrefix) + 1},
		{"quoted root", "version=1\n\"instructions.guide\"=true\n", toml.Key{"instructions.guide"}, 2, 1},
		{"repeated array table", "version=1\n[[skill]]\nname=\"one\"\n bogus=1\n[[skill]]\nname=\"two\"\n bogus=2\n", toml.Key{"skill", "bogus"}, 4, 2},
		{"inline array", "version=1\n" + arrayPrefix + "bogus=true}, {name=\"two\", bogus=false}]\n", toml.Key{"skill", "bogus"}, 2, utf8.RuneCountInString(arrayPrefix) + 1},
		{"BOM and quoted Unicode", "\ufeffversion=1\r\n\t\"가.b\"=1\r\n", toml.Key{"가.b"}, 2, 2},
		{"decoy text", "version=1\n[instructions.guide]\nsource=\"\"\"\nbogus=not-a-key\n\"\"\"\n  bogus=true\n", toml.Key{"instructions", "guide", "bogus"}, 6, 3},
		{"first unknown order", "version=1\nlater=1\nearlier=2\n", toml.Key{"later"}, 2, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeManifest([]byte(test.content))
			var unknown UnknownManifestKeyError
			if !errors.As(fmt.Errorf("outer: %w", err), &unknown) {
				t.Fatalf("error=%v, want typed unknown key", err)
			}
			line, column, ok := unknown.Position()
			if !ok || line != test.line || column != test.column {
				t.Fatalf("position=(%d,%d,%t), want (%d,%d,true)", line, column, ok, test.line, test.column)
			}
			legacy := fmt.Sprintf("unknown manifest key %q", test.key.String())
			if err.Error() != legacy || unknown.Unwrap().Error() != legacy {
				t.Fatalf("location changed shared error: %q / %q, want %q", err, unknown.Unwrap(), legacy)
			}
		})
	}
}

func TestUnknownManifestKeyWithoutPositionRetainsCause(t *testing.T) {
	cause := errors.New("unknown manifest key")
	err := UnknownManifestKeyError{cause: cause}
	if line, column, ok := err.Position(); ok || line != 0 || column != 0 || !errors.Is(err, cause) {
		t.Fatalf("missing position changed cause or invented coordinates: (%d,%d,%t)", line, column, ok)
	}
}
