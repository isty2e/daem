package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	daempaths "github.com/isty2e/daem/internal/paths"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

func TestRunMCPTransportRejectionsOmitPayload(t *testing.T) {
	for pairIndex, values := range [][2]string{
		{"to\u200bken=opaque-one", "to\u200bken=opaque-two"},
		{`to\u200bken=opaque-one`, `to\u200bken=opaque-two`},
		{"custom=opaque-one", "custom=opaque-two"},
	} {
		for _, commandCase := range []struct {
			args []string
			json bool
		}{
			{[]string{"apply", "--dry-run"}, false},
			{[]string{"apply", "--yes", "--verbose"}, false},
			{[]string{"apply", "--dry-run", "--json"}, true},
			{[]string{"apply", "--yes", "--json"}, true},
			{[]string{"status", "--verbose"}, false},
			{[]string{"lock", "--dry-run", "--json"}, true},
		} {
			command := commandCase.args
			t.Run(fmt.Sprintf("case%d/%s", pairIndex, strings.Join(command, " ")), func(t *testing.T) {
				root := isolatedApplyAuthorityRoot(t)
				t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
				manifestPath := filepath.Join(root, "daem.toml")
				args := append(append([]string{}, command...), "--manifest", manifestPath)
				var first string
				for index, transport := range values {
					manifest := fmt.Sprintf("version=1\ntargets=[\"codex\"]\n[[mcp_server]]\nname=\"tools\"\ntransport=%q\ncommand=\"must-not-run\"\n", transport)
					if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
						t.Fatal(err)
					}
					before := snapshotApplyInputFiles(t, root)
					var stdout, stderr bytes.Buffer
					if code := runWithoutTerminal(args, &stdout, &stderr); code != 1 {
						t.Fatalf("transport refusal code=%d stdout bytes=%d", code, stdout.Len())
					}
					message := refusalDiagnosticText(t, command[0], &stdout, &stderr, commandCase.json)
					if strings.Contains(message, "opaque-one") || strings.Contains(message, "opaque-two") {
						t.Error("transport refusal echoes rejected payload")
					}
					if !strings.Contains(message, "mcp_server[0].transport") {
						t.Error("transport refusal lost its owning field")
					}
					if index == 0 {
						first = message
					} else if message != first {
						t.Error("transport diagnostic depends on the rejected value")
					}
					if after := snapshotApplyInputFiles(t, root); !reflect.DeepEqual(before, after) {
						t.Error("transport refusal changed workspace files")
					}
				}
			})
		}
	}
}

func TestRunApplyEnvironmentFailureIsNotManifestInput(t *testing.T) {
	root := isolatedApplyAuthorityRoot(t)
	home := filepath.Join(root, "home")
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(root, "appdata"))
	t.Setenv("LOCALAPPDATA", filepath.Join(root, "localappdata"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	manifestPath := filepath.Join(root, "daem.toml")
	if err := os.WriteFile(manifestPath, []byte(`version=1
targets=["pi"]
[[extension]]
id="tools"
carrier="pi-package"
targets=["pi"]
scope="project"
source={host_source="./packages/tools"}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	before := snapshotApplyInputFiles(t, root)
	for _, name := range []string{"HOME", "USERPROFILE", "home"} {
		t.Setenv(name, "")
	}
	if _, err := os.UserHomeDir(); err == nil {
		t.Fatal("fixture did not remove the current platform's home context")
	}
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatalf("fixture cannot select project paths without HOME: %v", err)
	}
	if _, err := declarationmanifest.Load(t.Context(), manifestPath); err != nil {
		t.Fatalf("fixture is not a valid decoded declaration: %v", err)
	}
	if _, err := declarationmanifest.LoadSelected(t.Context(), paths); err == nil {
		t.Fatal("local carrier selection unexpectedly resolved without a home")
	} else if _, marked := declarationmanifest.InputErrorCause(err); marked {
		t.Error("manifest boundary marked contextual resolution as declaration input")
	}

	dry, dryErr := applyworkflow.PlanDryRun(t.Context(), applyworkflow.CommandInput{ManifestPath: manifestPath})
	if dryErr == nil || dry.ReconciliationReady {
		t.Fatal("environment failure produced an apply plan")
	}
	if _, marked := applyworkflow.InputErrorCause(dryErr); marked {
		t.Error("context resolution was admitted as manifest input")
	}
	prepared, writeErr := applyworkflow.PlanWrite(t.Context(), applyworkflow.CommandInput{ManifestPath: manifestPath})
	if prepared != nil {
		if err := prepared.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if writeErr == nil {
		t.Fatal("write planning unexpectedly resolved without a home")
	}
	if _, marked := applyworkflow.InputErrorCause(writeErr); marked {
		t.Error("write-mode context resolution was admitted as manifest input")
	}
	want := applyworkflow.ClassifyFailure(dryErr, dry.CommandResult).Detail()
	for _, flags := range [][]string{{"--dry-run"}, {"--yes"}, {"--dry-run", "--verbose"}, {"--yes", "--verbose"}, {"--dry-run", "--json"}, {"--yes", "--json"}, {}} {
		var stdout, stderr bytes.Buffer
		args := append([]string{"apply", "--manifest", manifestPath}, flags...)
		var code int
		if len(flags) == 0 {
			code = RunWithOptions(args, interactiveRunOptions(strings.NewReader(""), &stdout, &stderr))
		} else {
			code = runWithoutTerminal(args, &stdout, &stderr)
		}
		diagnostic := refusalDiagnosticText(t, "apply", &stdout, &stderr, slices.Contains(flags, "--json"))
		if code != 1 || !strings.Contains(diagnostic, want) {
			t.Errorf("environment refusal did not retain closed disposition: code=%d stdout=%d", code, stdout.Len())
		}
		for _, misleading := range []string{"invalid manifest", "correct the reported manifest", "next: run daem lock"} {
			if strings.Contains(diagnostic, misleading) {
				t.Error("environment failure was presented as repairable manifest input")
			}
		}
	}
	if after := snapshotApplyInputFiles(t, root); !reflect.DeepEqual(before, after) {
		t.Error("environment refusal changed the declaration or state")
	}

	for _, name := range []string{"HOME", "USERPROFILE", "home"} {
		t.Setenv(name, home)
	}
	if _, err := declarationmanifest.LoadSelected(t.Context(), paths); err != nil {
		t.Fatalf("restoring only HOME did not restore the same valid declaration: %v", err)
	}
	_, err = applyworkflow.PlanDryRun(t.Context(), applyworkflow.CommandInput{ManifestPath: manifestPath})
	if !errors.Is(err, applyworkflow.ErrReadLockfile) {
		t.Fatalf("restored environment did not reach the missing-lock input: %v", err)
	}
	if _, marked := applyworkflow.InputErrorCause(err); !marked {
		t.Error("the genuine missing-lock input lost its provenance")
	}
}
