package nativefixture

import (
	"bytes"
	"testing"
)

func RequireMCPUserComments(t *testing.T, content []byte) {
	t.Helper()
	for _, comment := range []string{"# User preferences\n", "# User theme\n"} {
		if !bytes.Contains(content, []byte(comment)) {
			t.Fatalf("shared mutation lost an unmanaged comment: %s", content)
		}
	}
}
