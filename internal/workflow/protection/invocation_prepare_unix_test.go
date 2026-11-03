//go:build darwin || linux

package protection

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	journalrecovery "github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/subprocess"
)

func TestInvocationRequiresItsPreparedInputBeforePublishing(t *testing.T) {
	for _, change := range []string{"unprepared", "paths", "requests", "leases"} {
		t.Run(change, func(t *testing.T) {
			fixture, input, bind := invocationFixture(t)
			if change != "unprepared" {
				var err error
				input, err = PrepareInvocation(t.Context(), input, journalrecovery.NewPhysicalCaptureBudget(nil))
				if err != nil {
					t.Fatal(err)
				}
			}
			switch change {
			case "paths":
				input.Paths.OwnershipRegistryPath += ".other"
			case "requests":
				input.Requests[0].Target = "other"
			case "leases":
				input.Leases = nil
			}
			marker := filepath.Join(fixture.Paths.ManifestRoot, "must-not-run")
			result, err := ExecuteInvocation(t.Context(), input, subprocess.CommandExecutor{}, subprocess.CommandAttemptRequest{Command: "/usr/bin/touch", Args: []string{marker}}, bind, nil)
			if err == nil {
				t.Fatal("unprepared or replaced invocation input was accepted")
			}
			if _, present := result.Protection(); present {
				t.Fatal("replaced input published protection")
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("replaced input reached command execution", err)
			}
			if rows, err := fixture.Store.LoadPendingProtections(t.Context()); err != nil || len(rows) != 0 {
				t.Fatal("replaced input changed the registry", err)
			}
		})
	}
}

func TestInvocationPreparationDoesNotFreezeFinalFileInode(t *testing.T) {
	fixture, input, bind := invocationFixture(t)
	input, err := prepareInvocationFixture(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range fixture.Documents {
		replacement := path + ".replacement"
		if err := os.WriteFile(replacement, []byte("malformed host payload\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(replacement, path); err != nil {
			t.Fatal(err)
		}
	}
	result, err := ExecuteInvocation(t.Context(), input, subprocess.CommandExecutor{}, subprocess.CommandAttemptRequest{Command: "/bin/true"}, bind, nil)
	if err != nil {
		t.Fatal("ordinary inode replacement invalidated prepared invocation", err)
	}
	assertInvocationSettled(t, fixture, result)
}
