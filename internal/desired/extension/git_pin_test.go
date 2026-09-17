package extension

import (
	"strings"
	"testing"
)

func TestGitCommitPinChangePreservesTheExactLocator(t *testing.T) {
	oldPin, newPin := strings.Repeat("a", 40), strings.Repeat("b", 40)
	for _, locator := range []string{
		"git:github.com/team/repo@", "git:https://github.com/team/repo@",
		"git:git@github.com:team/repo@", "https://github.com/team/repo#",
	} {
		t.Run(locator, func(t *testing.T) {
			if !GitCommitPinChange(locator+oldPin, locator+newPin) {
				t.Fatal("exact locator commit change was refused")
			}
		})
	}
	before := "git:github.com/team/repo@" + oldPin
	for _, after := range []string{
		before, "git:github.com/team/repo@main", "git:github.com/team/repo@deadbeef",
		"git:https://github.com/team/repo@" + newPin,
		"git:github.com/other/repo@" + newPin,
		"git:github.com/team/repo.git@" + newPin,
		"git:ssh://git@github.com:2222/team/repo@" + newPin,
		"npm:repo@" + newPin, "./repo", "git:github.com/team/repo@" + strings.Repeat("B", 40),
	} {
		t.Run(after, func(t *testing.T) {
			if GitCommitPinChange(before, after) {
				t.Fatal("non-canonical or non-pin change was admitted")
			}
		})
	}
}
