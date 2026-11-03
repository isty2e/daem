package nativefixture

import (
	"os"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/test/testkit"
)

func RunLifecycleCommand(t *testing.T, args ...string) string {
	t.Helper()
	code, stdout, stderr := testkit.RunCapturedCLI(args...)
	if code != 0 {
		t.Fatalf("%q failed: exit=%d stdout=%s stderr=%s", args, code, stdout, stderr)
	}
	return stdout
}

func LifecycleLimits() configdocument.Limits {
	return configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}
}

func RequireSharedValues(t *testing.T, syntax configdocument.Syntax, content []byte, fragment string, present bool) {
	t.Helper()
	document, err := configdocument.Parse(t.Context(), content, syntax, LifecycleLimits())
	if err != nil {
		t.Fatal("native CLI wrote an invalid document", err)
	}
	expected, err := configdocument.Parse(t.Context(), []byte(fragment), syntax, LifecycleLimits())
	if err != nil {
		t.Fatal(err)
	}
	entries, err := expected.Leaves(t.Context(), LifecycleLimits().MaximumBytes)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		value, found, err := document.Lookup(entry.Path)
		if err != nil || found != present || (found && !value.Equal(entry.Value)) {
			t.Fatalf("shared key %s: present=%t want=%t value=%s error=%v", entry.Path, found, present, value.Canonical(), err)
		}
	}
}

func RequireSharedMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("key mode changed shared file permissions: got=%04o want=%04o", info.Mode().Perm(), want)
	}
}
