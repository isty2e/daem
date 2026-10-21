package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/execcheck"
)

func TestPiSourceDiagnosticCLIProjectsOnlySourceFreeWarnings(t *testing.T) {
	for _, scope := range []string{"project", "global"} {
		for _, test := range []struct {
			ref, code string
		}{
			{"github:acme/PI_SOURCE_MARKER#v1", "extension.pi_source.bare_github"},
			{"git+https://router.home.arpa/acme/PI_SOURCE_MARKER.git#v1", "extension.pi_source.git_plus"},
			{"git:github.com/acme/control", ""},
			{"https://github.com/acme/control.git", ""},
		} {
			t.Run(scope+"/"+test.ref, func(t *testing.T) {
				root := t.TempDir()
				t.Setenv("HOME", filepath.Join(root, "home"))
				t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(root, "agent"))
				testkit.SetDefaultRootEnv(t, root)
				canary := execcheck.New(t, "pi")
				manifest := filepath.Join(root, "daem.toml")
				testkit.WriteFile(t, root, "daem.toml", fmt.Sprintf(`version = 1
targets = ["pi"]

[[extension]]
id = "source-example"
carrier = "pi-package"
scope = %q
source = { host_source = %q }
`, scope, test.ref))
				var stdout, stderr bytes.Buffer
				if exit := testkit.RunCLI([]string{"lock", "--manifest", manifest, "--json"}, &stdout, &stderr); exit != 0 || stderr.Len() != 0 {
					t.Fatalf("lock exit=%d stdout=%s stderr=%s", exit, &stdout, &stderr)
				}
				if strings.Contains(stdout.String(), "extension.pi_source") {
					t.Fatal("warning changed lock output")
				}
				settingsBase := filepath.Join(root, ".pi")
				if scope == "global" {
					settingsBase = filepath.Join(root, "agent")
				}
				testkit.WriteFile(t, settingsBase, "settings.json", `{"packages":[]}`)

				for _, command := range [][]string{{"status"}, {"status", "--check"}, {"apply", "--dry-run"}} {
					args := append(append([]string(nil), command...), "--manifest", manifest, "--json")
					stdout.Reset()
					stderr.Reset()
					exit := testkit.RunCLI(args, &stdout, &stderr)
					if exit != 0 || stderr.Len() != 0 {
						t.Fatalf("%v exit=%d stdout=%s stderr=%s", command, exit, &stdout, &stderr)
					}
					payload := clijson.DecodePlan(t, stdout.Bytes())
					if payload.HasErrors || (command[0] == "status" && (payload.CheckClean == nil || !*payload.CheckClean)) {
						t.Fatalf("warning disturbed clean fixture result: %#v", payload)
					}
					want := 0
					if test.code != "" {
						want = 1
					}
					if len(payload.Diagnostics) != want {
						t.Fatalf("%v diagnostics=%#v, want %d", command, payload.Diagnostics, want)
					}
					for _, diagnostic := range payload.Diagnostics {
						encoded, err := json.Marshal(diagnostic)
						if err != nil {
							t.Fatal(err)
						}
						if diagnostic.Code != test.code || diagnostic.Severity != "warn" || diagnostic.ResourceID != "extension/source-example" ||
							diagnostic.Scope != scope || strings.Contains(string(encoded), "PI_SOURCE_MARKER") || strings.Contains(string(encoded), "router.home.arpa") {
							t.Fatalf("incorrect/source-reflecting diagnostic: %s", encoded)
						}
					}

					for _, verbose := range []bool{false, true} {
						humanArgs := append(append([]string(nil), command...), "--manifest", manifest)
						if verbose {
							humanArgs = append(humanArgs, "--verbose")
						}
						stdout.Reset()
						stderr.Reset()
						if exit := testkit.RunCLI(humanArgs, &stdout, &stderr); exit != 0 || stderr.Len() != 0 {
							t.Fatalf("human %v exit=%d stdout=%s stderr=%s", command, exit, &stdout, &stderr)
						}
						piSourceWarningLines(t, stdout.String(), "warn ", want)
					}
				}

				var requests []subprocess.CommandRequest
				executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
					Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
						requests = append(requests, request)
						want := []string{"install", test.ref}
						if scope == "project" {
							want = append(want, "-l")
						}
						if request.Command != "pi" || !slices.Equal(request.Args, want) {
							t.Fatalf("unexpected host effect: %#v", request)
						}
						testkit.WriteFile(t, settingsBase, "settings.json", fmt.Sprintf(`{"packages":[%q]}`, test.ref))
						return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
					},
				})
				stdout.Reset()
				stderr.Reset()
				if exit := testkit.RunCLIWithOptions([]string{"apply", "--yes", "--manifest", manifest, "--json"}, clipkg.RunOptions{
					Stdout:              &stdout,
					Stderr:              &stderr,
					ApplyExecuteOptions: applyworkflow.ExecuteOptions{HostRouteExecutor: executor},
				}); exit != 0 || stderr.Len() != 0 {
					t.Fatalf("confirmed apply exit=%d stdout=%s stderr=%s", exit, &stdout, &stderr)
				}
				applied := clijson.DecodeApplyResult(t, stdout.Bytes())
				want := 0
				if test.code != "" {
					want = 1
				}
				if applied.HasErrors || len(requests) != 1 || len(applied.Diagnostics) != want {
					t.Fatalf("confirmed apply changed effects/diagnostics: requests=%#v result=%#v", requests, applied)
				}
				for _, diagnostic := range applied.Diagnostics {
					encoded, err := json.Marshal(diagnostic)
					if err != nil {
						t.Fatal(err)
					}
					if diagnostic.Code != test.code || diagnostic.Severity != "warn" || diagnostic.Scope != scope ||
						strings.Contains(string(encoded), "PI_SOURCE_MARKER") || strings.Contains(string(encoded), "router.home.arpa") {
						t.Fatalf("confirmed apply reflected/changed diagnostic: %s", encoded)
					}
				}

				addRoot := filepath.Join(root, "add")
				addManifest := filepath.Join(addRoot, "daem.toml")
				testkit.WriteFile(t, addRoot, "daem.toml", "version = 1\ntargets = [\"pi\"]\n")
				add := []string{"add", "extension", "source-example", test.ref, "--scope", scope, "--manifest", addManifest}
				stdout.Reset()
				stderr.Reset()
				if exit := testkit.RunCLI(append(slices.Clone(add), "--dry-run", "--json"), &stdout, &stderr); exit != 0 || stderr.Len() != 0 {
					t.Fatalf("add JSON exit=%d stdout=%s stderr=%s", exit, &stdout, &stderr)
				}
				var added struct {
					Warnings []string `json:"warnings"`
					Changes  []struct {
						ManifestBlock string `json:"manifest_block"`
					} `json:"changes"`
				}
				if err := json.Unmarshal(stdout.Bytes(), &added); err != nil {
					t.Fatal(err)
				}
				want = 0
				if test.code != "" {
					want = 1
				}
				if len(added.Warnings) != want || len(added.Changes) != 1 || !strings.Contains(added.Changes[0].ManifestBlock, test.ref) {
					t.Fatalf("incorrect add warning/block: %#v", added)
				}
				for _, warning := range added.Warnings {
					if strings.Contains(warning, "PI_SOURCE_MARKER") || strings.Contains(warning, "router.home.arpa") {
						t.Fatalf("add warning reflected source: %q", warning)
					}
				}
				stdout.Reset()
				stderr.Reset()
				if exit := testkit.RunCLI(append(slices.Clone(add), "--dry-run", "--verbose"), &stdout, &stderr); exit != 0 || stderr.Len() != 0 {
					t.Fatalf("add human exit=%d stdout=%s stderr=%s", exit, &stdout, &stderr)
				}
				piSourceWarningLines(t, stdout.String(), "warning: ", want)

				stdout.Reset()
				stderr.Reset()
				if exit := testkit.RunCLI(append(slices.Clone(add), "--json"), &stdout, &stderr); exit != 0 || stderr.Len() != 0 {
					t.Fatalf("add write exit=%d stdout=%s stderr=%s", exit, &stdout, &stderr)
				}
				var written struct {
					Warnings []string `json:"warnings"`
				}
				if err := json.Unmarshal(stdout.Bytes(), &written); err != nil {
					t.Fatal(err)
				}
				if len(written.Warnings) != want {
					t.Fatalf("add write lost warnings: %#v", written)
				}
				for _, warning := range written.Warnings {
					if strings.Contains(warning, "PI_SOURCE_MARKER") || strings.Contains(warning, "router.home.arpa") {
						t.Fatalf("add write warning reflected source: %q", warning)
					}
				}
				execcheck.AssertClean(t, canary, "Pi source diagnostic computation")
			})
		}
	}
}

func piSourceWarningLines(t *testing.T, output, prefix string, want int) {
	t.Helper()
	count := 0
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, prefix) || !strings.Contains(line, "source-example") {
			continue
		}
		count++
		if strings.Contains(line, "PI_SOURCE_MARKER") || strings.Contains(line, "router.home.arpa") {
			t.Fatalf("human warning reflected source: %q", line)
		}
	}
	if count != want {
		t.Fatalf("warning lines=%d, want %d: %s", count, want, output)
	}
}
