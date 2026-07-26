package clipresent

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintLockOnlyResourceDetailsListsSkillAndHookResources(t *testing.T) {
	var stdout bytes.Buffer

	PrintLockOnlyResourceDetails(&stdout, LockOnlyResources{
		Skills: []LockOnlyResource{
			{Kind: "skill", Name: "oracle", Targets: []string{"future-agent", "pi"}},
		},
		Hooks: []LockOnlyResource{
			{Kind: "hook", Name: "protect-env", Targets: []string{"opencode"}},
		},
	})

	for _, want := range []string{
		"  - skill/oracle targets=future-agent,pi\n",
		"  - hook/protect-env targets=opencode\n",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}
