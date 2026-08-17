package filesnapshot

import "testing"

func TestValidDirentNameRejectsTraversal(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", ".", "..", "a/b", `a\b`, "a\x00b"} {
		if err := validDirentName(name); err == nil {
			t.Fatalf("validDirentName(%q) succeeded", name)
		}
	}
	if err := validDirentName("plugin.json"); err != nil {
		t.Fatalf("validDirentName(plugin.json) = %v", err)
	}
}
