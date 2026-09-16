package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
)

func TestMigrateStateRequiresCompleteDisclosureAndConfirmation(t *testing.T) {
	for _, name := range []string{"accept", "decline", "disclosure_failure"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", filepath.Join(root, "home"))
			for _, kind := range []string{"CONFIG", "STATE", "CACHE", "DATA"} {
				t.Setenv("XDG_"+kind+"_HOME", filepath.Join(root, strings.ToLower(kind)))
			}
			manifest := filepath.Join(root, "config", "daem", "daem.toml")
			legacy := filepath.Join(filepath.Dir(manifest), ".daem", "state.json")
			if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(manifest, []byte("version = 1\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			content, err := statefile.Marshal(durable.EmptySnapshot())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(legacy, content, 0o600); err != nil {
				t.Fatal(err)
			}

			answer := "yes\n"
			if name == "decline" {
				answer = "no\n"
			}
			var stdout, stderr bytes.Buffer
			var output io.Writer = &stdout
			if name == "disclosure_failure" {
				output = migrationFailedDisclosure{}
			}
			code := RunWithOptions([]string{"migrate", "state", "--manifest", manifest}, interactiveRunOptions(strings.NewReader(answer), output, &stderr))
			destination := filepath.Join(root, "state", "daem", "state.json")
			if name == "accept" {
				if code != 0 {
					t.Fatalf("migration: code=%d, stderr=%s", code, stderr.String())
				}
				if _, err := statefile.Load(t.Context(), destination); err != nil {
					t.Fatal(err)
				}

				for _, repeat := range []struct {
					name     string
					tty      bool
					yes      bool
					wantCode int
				}{
					{name: "non_tty", wantCode: 2},
					{name: "tty", tty: true},
					{name: "non_tty_yes", yes: true},
				} {
					t.Run("repeat_"+repeat.name, func(t *testing.T) {
						var stdout, stderr bytes.Buffer
						options := RunOptions{Stdout: &stdout, Stderr: &stderr}
						if repeat.tty {
							options = interactiveRunOptions(strings.NewReader(""), &stdout, &stderr)
						}
						args := []string{"migrate", "state", "--manifest", manifest}
						if repeat.yes {
							args = append(args, "--yes")
						}

						if code := RunWithOptions(args, options); code != repeat.wantCode {
							t.Fatalf("repeat: code=%d, stderr=%s", code, stderr.String())
						}
						if strings.Contains(stderr.String(), "Proceed with") {
							t.Fatal("completed repeat prompted again")
						}
						if repeat.wantCode == 0 && !strings.Contains(stdout.String(), "already_migrated") {
							t.Fatalf("repeat did not report completion: %s", stdout.String())
						}
					})
				}
			} else {
				if code != 1 {
					t.Fatalf("refusal code = %d", code)
				}
				assertCLIPathMissing(t, destination)
				assertCLIPathMissing(t, filepath.Join(root, "data"))
				got, err := os.ReadFile(legacy)
				if err != nil || !bytes.Equal(got, content) {
					t.Fatalf("refusal changed source: %v", err)
				}
			}
			if name == "disclosure_failure" && strings.Contains(stderr.String(), "Proceed with") {
				t.Fatal("prompt followed failed disclosure")
			}
		})
	}
}

type migrationFailedDisclosure struct{}

func (migrationFailedDisclosure) Write([]byte) (int, error) {
	return 0, errors.New("disclosure unavailable")
}

func TestMigrateStateRejectsInvalidAuthorizationAndArguments(t *testing.T) {
	for _, args := range [][]string{
		{"migrate", "unknown"},
		{"migrate", "state", "extra"},
		{"migrate", "state", "--dry-run", "--yes"},
		{"migrate", "state", "--json"},
		{"migrate", "state"},
	} {
		var stdout, stderr bytes.Buffer
		if code := RunWithOptions(args, RunOptions{Stdout: &stdout, Stderr: &stderr}); code != 2 {
			t.Fatalf("%q: code=%d, stderr=%s", args, code, stderr.String())
		}
	}
}
