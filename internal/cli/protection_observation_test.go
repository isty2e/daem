//go:build darwin || linux

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/ownershiptest"
)

type protectionCommandOutput struct {
	ResultType           string `json:"result_type"`
	CheckClean           *bool  `json:"check_clean"`
	ActionCount          *int   `json:"action_count"`
	PermissionProtection *struct {
		Observation string `json:"observation"`
		Reason      string `json:"reason"`
		NextStep    string `json:"next_step"`
		Records     []struct {
			Invocation string `json:"invocation"`
			Phase      string `json:"phase"`
			Documents  []struct {
				Destinations []string `json:"destinations"`
			} `json:"documents"`
		} `json:"observed_records"`
	} `json:"permission_protection"`
}

func runProtectionCommand(t *testing.T, manifest string, command string, arguments ...string) (protectionCommandOutput, int) {
	t.Helper()
	args := append([]string{command, "--json", "--manifest", manifest}, arguments...)
	var stdout, stderr bytes.Buffer
	code := RunWithOptions(args, RunOptions{Stdout: &stdout, Stderr: &stderr})
	var value protectionCommandOutput
	if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
		t.Fatalf("%v: invalid JSON (%d): %v stdout=%s stderr=%s", args, code, err, stdout.String(), stderr.String())
	}
	return value, code
}

func TestOwnerProtectionSurvivesMissingAndMalformedCommandInputs(t *testing.T) {
	for _, source := range []string{"missing", "malformed", "malformed-lock", "malformed-state"} {
		for _, command := range []struct {
			name string
			args []string
		}{
			{"status", nil}, {"apply", []string{"--dry-run"}}, {"apply", []string{"--yes"}}, {"doctor", nil},
		} {
			t.Run(source+"/"+command.name+"/"+strings.Join(command.args, ","), func(t *testing.T) {
				fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml")
				if source == "malformed" {
					if err := os.WriteFile(fixture.Paths.ManifestPath, []byte("not valid toml ["), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if source == "malformed-lock" || source == "malformed-state" {
					if err := os.WriteFile(fixture.Paths.ManifestPath, []byte("version = 1\ntargets = [\"codex\"]\n"), 0o600); err != nil {
						t.Fatal(err)
					}
					var stdout, stderr bytes.Buffer
					if code := RunWithOptions([]string{"lock", "--manifest", fixture.Paths.ManifestPath}, RunOptions{Stdout: &stdout, Stderr: &stderr}); code != 0 {
						t.Fatalf("lock failed: %s %s", stdout.String(), stderr.String())
					}
					if source == "malformed-lock" {
						if err := os.WriteFile(fixture.Paths.LockfilePath, []byte("invalid lock ["), 0o600); err != nil {
							t.Fatal(err)
						}
					}
				}
				before, err := os.ReadFile(fixture.Paths.OwnershipRegistryPath)
				if err != nil {
					t.Fatal(err)
				}
				value, code := runProtectionCommand(t, fixture.Paths.ManifestPath, command.name, command.args...)
				protection := value.PermissionProtection
				if code == 0 || protection == nil || protection.Observation != "observed" || len(protection.Records) != 1 {
					t.Fatalf("lost independent discovery: code=%d output=%+v", code, value)
				}
				row := protection.Records[0]
				if row.Invocation != fixture.Protection.Invocation().String() || row.Phase != "armed" || len(row.Documents) != 1 || len(row.Documents[0].Destinations) != 1 || row.Documents[0].Destinations[0] != ".codex/config.toml" || protection.NextStep == "" {
					t.Fatalf("incomplete owner disclosure: %+v", protection)
				}
				after, err := os.ReadFile(fixture.Paths.OwnershipRegistryPath)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("discovery changed protection authority: %v", err)
				}
				info, err := os.Stat(fixture.Documents[0])
				if err != nil || info.Mode().Perm() != 0o644 {
					t.Fatalf("discovery changed permissions: %v", err)
				}
			})
		}
	}
}

func TestOwnerProtectionDoesNotBlockNoOpOrDisjointApplyAndStatusTracksSettlement(t *testing.T) {
	for _, disjoint := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-op", true: "disjoint"}[disjoint], func(t *testing.T) {
			fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml")
			if err := os.Remove(fixture.Paths.StatefilePath); err != nil {
				t.Fatal(err)
			}
			root := filepath.Dir(fixture.Paths.ManifestPath)
			if disjoint {
				writeApplyConfirmationFixture(t, root)
			} else {
				if err := os.WriteFile(fixture.Paths.ManifestPath, []byte("version = 1\ntargets = [\"codex\"]\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				var stdout, stderr bytes.Buffer
				if code := RunWithOptions([]string{"lock", "--manifest", fixture.Paths.ManifestPath}, RunOptions{Stdout: &stdout, Stderr: &stderr}); code != 0 {
					t.Fatalf("lock failed: %s %s", stdout.String(), stderr.String())
				}
			}
			for _, mode := range []string{"--dry-run", "--yes"} {
				value, code := runProtectionCommand(t, fixture.Paths.ManifestPath, "apply", mode)
				if code != 0 || value.PermissionProtection == nil || len(value.PermissionProtection.Records) != 1 {
					t.Fatalf("protected owner blocked independent Apply: code=%d output=%+v", code, value)
				}
				if !disjoint && (value.ActionCount == nil || *value.ActionCount != 0) {
					t.Fatal("diagnostic protection became a no-op Apply action")
				}
			}
			if disjoint {
				if content, err := os.ReadFile(filepath.Join(root, "AGENTS.md")); err != nil || string(content) != "shared instructions\n" {
					t.Fatalf("disjoint effect absent: %v %q", err, content)
				}
			}
			rows, err := fixture.Store.LoadPendingProtections(t.Context())
			if err != nil || len(rows) != 1 || !rows[0].Equal(fixture.Protection) {
				t.Fatalf("Apply altered pending protection: %v", err)
			}
			statusArgs := []string{"--check"}
			if disjoint {
				statusArgs = append(statusArgs, "--target", "codex")
			}
			value, code := runProtectionCommand(t, fixture.Paths.ManifestPath, "status", statusArgs...)
			if code != 1 || value.CheckClean == nil || *value.CheckClean || value.PermissionProtection == nil {
				t.Fatalf("status certified pending owner clean: %d %+v", code, value)
			}
			if _, code := runPermissionRecoverCLI(t, fixture.Paths.ManifestPath, "--yes"); code != 0 {
				t.Fatal("recovery failed")
			}
			value, code = runProtectionCommand(t, fixture.Paths.ManifestPath, "status", "--check")
			if code != 0 || value.CheckClean == nil || !*value.CheckClean || value.PermissionProtection != nil {
				t.Fatalf("settled owner not clean: %d %+v", code, value)
			}
		})
	}
}

func TestUnavailableProtectionInventoryRemainsExplicitOnSourceFailure(t *testing.T) {
	fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml")
	if err := os.WriteFile(fixture.Paths.OwnershipRegistryPath, []byte("malformed"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"status", "doctor", "apply"} {
		args := []string{}
		if command == "apply" {
			args = append(args, "--yes")
		}
		value, code := runProtectionCommand(t, fixture.Paths.ManifestPath, command, args...)
		if code == 0 || value.PermissionProtection == nil || value.PermissionProtection.Observation != "unavailable" || value.PermissionProtection.Reason == "" || len(value.PermissionProtection.Records) != 0 {
			t.Fatalf("unknown inventory became empty evidence: %d %+v", code, value)
		}
	}
}

func TestHumanSourceFailureRetainsObservedProtectionAndSafeRecoveryAdvice(t *testing.T) {
	fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml")
	for _, command := range []string{"status", "doctor", "apply"} {
		args := []string{command, "--manifest", fixture.Paths.ManifestPath}
		if command == "apply" {
			args = append(args, "--yes")
		}
		var stdout, stderr bytes.Buffer
		code := RunWithOptions(args, RunOptions{Stdout: &stdout, Stderr: &stderr})
		text := stdout.String() + stderr.String()
		if code == 0 || !strings.Contains(text, fixture.Protection.Invocation().String()) || !strings.Contains(text, ".codex/config.toml") || !strings.Contains(text, "recover --dry-run") {
			t.Fatalf("human %s lost recovery evidence: %d %s", command, code, text)
		}
		if strings.Contains(text, fixture.Owner.Key().String()) {
			t.Fatal("human output exposed physical owner namespace")
		}
	}
}

func TestRecoveryBarrierFailureStillDisclosesOwnerProtection(t *testing.T) {
	fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml")
	if err := os.MkdirAll(filepath.Join(fixture.Paths.StateDir, "recovery", "active-operation"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"status", "doctor", "apply"} {
		args := []string{}
		if command == "apply" {
			args = append(args, "--yes")
		}
		value, code := runProtectionCommand(t, fixture.Paths.ManifestPath, command, args...)
		if code != 1 || value.ResultType != "cli_error" || value.PermissionProtection == nil || len(value.PermissionProtection.Records) != 1 {
			t.Fatalf("barrier hid independent protection: %s %d %+v", command, code, value)
		}
	}
}
