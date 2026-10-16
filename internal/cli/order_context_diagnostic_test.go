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
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	lockrefine "github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/realization/lockfile"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

func TestRunApplyOrderContextFailureIsNotLockInput(t *testing.T) {
	manifestFor := func(names ...string) string {
		var builder strings.Builder
		builder.WriteString("version=1\ntargets=[\"opencode\"]\n")
		for _, name := range names {
			fmt.Fprintf(&builder, "[[extension]]\nid=%q\ncarrier=\"opencode-plugin\"\ntargets=[\"opencode\"]\nscope=\"global\"\nsource={host_source=%q}\n", name, "@acme/"+name+"@1.0.0")
		}
		return builder.String()
	}
	for _, lockedMembers := range []int{1, 2} {
		t.Run(fmt.Sprintf("locked-members-%d", lockedMembers), func(t *testing.T) {
			root := isolatedApplyAuthorityRoot(t)
			t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
			t.Setenv("APPDATA", filepath.Join(root, "appdata"))
			t.Setenv("LOCALAPPDATA", filepath.Join(root, "localappdata"))
			config := filepath.Join(root, "config")
			manifestPath := filepath.Join(root, "daem.toml")
			seed := manifestFor("first")
			if lockedMembers == 2 {
				seed = manifestFor("first", "second")
			}
			if err := os.WriteFile(manifestPath, []byte(seed), 0o600); err != nil {
				t.Fatal(err)
			}
			var seedOut, seedErr bytes.Buffer
			if code := runWithoutTerminal([]string{"lock", "--manifest", manifestPath}, &seedOut, &seedErr); code != 0 {
				t.Fatalf("seed lock code=%d stderr=%q", code, &seedErr)
			}
			if err := os.WriteFile(manifestPath, []byte(manifestFor("first", "second")), 0o600); err != nil {
				t.Fatal(err)
			}
			before := snapshotApplyInputFiles(t, root)
			for _, name := range []string{"XDG_CONFIG_HOME", "HOME", "USERPROFILE", "home"} {
				t.Setenv(name, "")
			}
			if _, err := os.UserHomeDir(); err == nil {
				t.Fatal("fixture did not remove home context")
			}
			paths, err := daempaths.Resolve(manifestPath)
			if err != nil {
				t.Fatalf("project path fixture cannot reach order validation: %v", err)
			}
			environment, err := declarationmanifest.LoadSelected(t.Context(), paths)
			if err != nil {
				t.Fatalf("fixture does not reach order context after manifest resolution: %v", err)
			}
			locked, err := lockfile.Load(t.Context(), paths.LockfilePath)
			if err != nil {
				t.Fatalf("fixture lock is not readable: %v", err)
			}
			if err := lockrefine.ValidateCurrentExtensionOrder(environment.Extensions(), locked, aggregatecodec.ExtensionOrderIdentityResolver(paths)); err == nil {
				t.Fatal("order identity unexpectedly resolved without global config context")
			}

			dry, dryErr := applyworkflow.PlanDryRun(t.Context(), applyworkflow.CommandInput{ManifestPath: manifestPath})
			if dryErr == nil || dry.ReconciliationReady {
				t.Fatal("context failure produced a plan")
			}
			prepared, writeErr := applyworkflow.PlanWrite(t.Context(), applyworkflow.CommandInput{ManifestPath: manifestPath})
			if prepared != nil {
				if err := prepared.Close(); err != nil {
					t.Fatal(err)
				}
			}
			for _, failure := range []error{dryErr, writeErr} {
				if failure == nil {
					t.Fatal("context failure was admitted by a planning mode")
				}
				if _, marked := applyworkflow.InputErrorCause(failure); marked || errors.Is(failure, applyworkflow.ErrReadLockfile) {
					t.Error("host identity context was classified as lockfile input")
				}
				if applyworkflow.ClassifyFailure(failure, applyworkflow.CommandResult{}).Reason() == applyworkflow.FailureReasonLockfileUnavailable {
					t.Error("runtime identity failure became lockfile_unavailable")
				}
			}
			for _, flags := range [][]string{{"--dry-run"}, {"--yes"}, {"--dry-run", "--verbose"}, {"--yes", "--verbose"}, {"--dry-run", "--json"}, {"--yes", "--json"}, {}} {
				var stdout, stderr bytes.Buffer
				args := append([]string{"apply", "--manifest", manifestPath}, flags...)
				var code int
				if len(flags) == 0 {
					code = RunWithOptions(args, interactiveRunOptions(strings.NewReader(""), &stdout, &stderr))
				} else {
					code = runWithoutTerminal(args, &stdout, &stderr)
				}
				if code != 1 {
					t.Errorf("context refusal code=%d stdout bytes=%d", code, stdout.Len())
				}
				diagnostic := refusalDiagnosticText(t, "apply", &stdout, &stderr, slices.Contains(flags, "--json"))
				for _, misleading := range []string{"invalid manifest", "lockfile is unavailable", "correct the reported manifest", "next: run daem lock"} {
					if strings.Contains(diagnostic, misleading) {
						t.Error("host environment failure advised repairing valid input files")
					}
				}
			}
			if after := snapshotApplyInputFiles(t, root); !reflect.DeepEqual(before, after) {
				t.Error("context failure modified input or state files")
			}

			t.Setenv("XDG_CONFIG_HOME", config)
			restored := lockrefine.ValidateCurrentExtensionOrder(environment.Extensions(), locked, aggregatecodec.ExtensionOrderIdentityResolver(paths))
			if lockedMembers == 2 && restored != nil || lockedMembers == 1 && !errors.Is(restored, lockrefine.ErrStaleExtensionOrder) {
				t.Fatalf("restoring only config context did not restore data validation: %v", restored)
			}
			if err := os.WriteFile(manifestPath, []byte(manifestFor("second", "first")), 0o600); err != nil {
				t.Fatal(err)
			}
			_, stale := applyworkflow.PlanDryRun(t.Context(), applyworkflow.CommandInput{ManifestPath: manifestPath})
			if !errors.Is(stale, lockrefine.ErrStaleExtensionOrder) || !errors.Is(stale, applyworkflow.ErrReadLockfile) {
				t.Fatalf("genuine stale order lost its input classification: %v", stale)
			}
			if _, marked := applyworkflow.InputErrorCause(stale); !marked {
				t.Error("genuine stale lock order lost input provenance")
			}
		})
	}
}
