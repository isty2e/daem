package clipresent

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/output"
)

func TestNativeKeyDisplaySafety(t *testing.T) {
	for _, segment := range []string{"line\nbreak", "\x1b[31m", "direction\u202eoverride"} {
		key, err := output.NewKeyPath("ext", segment)
		if err != nil {
			t.Fatal(err)
		}
		text := nativeKeyDisplay(key)
		if strings.ContainsAny(text, "\n\r\x1b\u202e") {
			t.Fatalf("key label contains active terminal characters: %q", text)
		}
	}

	key, err := output.NewKeyPath(strings.Repeat("long-key", maximumHumanErrorDetailRunes))
	if err != nil {
		t.Fatal(err)
	}
	text := nativeKeyDisplay(key)
	if len(text) > maximumHumanErrorDetailRunes || !strings.Contains(text, "[truncated]") {
		t.Fatal("oversized key label was not bounded")
	}
}
