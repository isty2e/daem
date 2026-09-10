//go:build unix

package tooling

import (
	"context"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestInstallerJourneysBelongToFullAndRaceLanes(t *testing.T) {
	root := findRepoRoot(t)
	for _, lane := range []string{"core", "full", "race"} {
		t.Run(lane, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, filepath.Join(root, "tools", "test.sh"), "packages", lane)
			command.Dir = root
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("list %s packages: %v\n%s", lane, err, output)
			}
			included := slices.Contains(strings.Fields(string(output)), "github.com/isty2e/daem/test/install")
			if included != (lane != "core") {
				t.Fatalf("installer journey ownership for lane %q: included=%t", lane, included)
			}
		})
	}
}
