package apply

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/realization/aggregate"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
	workflowrecover "github.com/isty2e/daem/internal/workflow/recover"
	workflowstatus "github.com/isty2e/daem/internal/workflow/status"
)

func TestCodexMCPAndNativeKeysShareOnlyTheirSelectedPhysicalDocument(t *testing.T) {
	for _, scenario := range []struct {
		name                  string
		initial               string
		separate              bool
		unicode               bool
		symlink               bool
		project               bool
		removal               bool
		recovery              bool
		nativeOnlyObservation bool
	}{
		{name: "MCP first", initial: "MCP", nativeOnlyObservation: true},
		{name: "native first", initial: "native"},
		{name: "fresh shared"},
		{name: "fresh in existing unicode root", unicode: true},
		{name: "shared through ordinary selector symlink", initial: "MCP", symlink: true},
		{name: "separate files", initial: "MCP", separate: true},
		{name: "fresh separate files", separate: true},
		{name: "project shared document", initial: "MCP", project: true},
		{name: "shared removal transitions", initial: "MCP", removal: true},
		{name: "loaded shared-document recovery", initial: "MCP", recovery: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			home := filepath.Join(base, "home")
			if scenario.unicode {
				home = filepath.Join(base, "hôme")
				if err := os.MkdirAll(home, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			nativeRoot := filepath.Join(home, ".codex")
			if scenario.separate || scenario.symlink {
				nativeRoot = filepath.Join(base, "native-codex")
			}
			for name, path := range map[string]string{
				"HOME": home, "XDG_DATA_HOME": filepath.Join(base, "data"),
				"XDG_STATE_HOME": filepath.Join(base, "state"), "XDG_CONFIG_HOME": filepath.Join(base, "config"),
				"CODEX_HOME": nativeRoot,
			} {
				t.Setenv(name, path)
			}
			if scenario.symlink {
				for _, path := range []string{home, nativeRoot} {
					if err := os.MkdirAll(path, 0o700); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(nativeRoot, filepath.Join(home, ".codex")); err != nil {
					t.Fatal(err)
				}
			}
			workspace := filepath.Join(base, "workspace")
			if err := os.MkdirAll(workspace, 0o700); err != nil {
				t.Fatal(err)
			}
			manifest := filepath.Join(workspace, "daem.toml")
			ordinaryRoot := filepath.Join(home, ".codex")
			scope := "global"
			if scenario.project {
				scope = "project"
				ordinaryRoot = filepath.Join(workspace, ".codex")
				nativeRoot = ordinaryRoot
			}
			source := filepath.Join(workspace, "preferences.toml")
			if err := os.WriteFile(source, []byte("model='managed'\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			const header = "version=1\ntargets=['codex']\n"
			mcp := fmt.Sprintf("[[mcp_server]]\nname='search'\nscope=%q\ntransport='stdio'\ncommand='node'\nargs=['search.js']\n", scope)
			native := fmt.Sprintf("[[host_config]]\nid='preferences'\nscope=%q\npath='config.toml'\nsource=%q\nownership='keys'\n", scope, source)
			apply := func(contents string) CommandResult {
				t.Helper()
				if err := os.WriteFile(manifest, []byte(header+contents), 0o600); err != nil {
					t.Fatal(err)
				}
				if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: manifest}); err != nil {
					t.Fatal(err)
				}
				prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifest})
				if err != nil {
					t.Fatal(err)
				}
				result, executeErr := ExecuteWithOptions(t.Context(), prepared, ExecuteOptions{})
				closeErr := prepared.Close()
				if executeErr != nil || closeErr != nil {
					t.Fatalf("apply failed: %v; close: %v", executeErr, closeErr)
				}
				return result
			}
			switch scenario.initial {
			case "MCP":
				apply(mcp)
			case "native":
				apply(native)
			}
			apply(mcp + native)

			type config struct {
				Model      string `toml:"model"`
				MCPServers map[string]struct {
					Command string
					Args    []string
				} `toml:"mcp_servers"`
			}
			read := func(path string) config {
				t.Helper()
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var value config
				if err := toml.Unmarshal(data, &value); err != nil {
					t.Fatal(err)
				}
				return value
			}
			ordinary := read(filepath.Join(ordinaryRoot, "config.toml"))
			selected := read(filepath.Join(nativeRoot, "config.toml"))
			if ordinary.MCPServers["search"].Command != "node" || selected.Model != "managed" {
				t.Fatalf("contributions did not reach their selected files: ordinary=%+v native=%+v", ordinary, selected)
			}
			if scenario.separate {
				if ordinary.Model != "" || len(selected.MCPServers) != 0 {
					t.Fatal("separate ordinary/native contributions were redirected")
				}
			} else if ordinary.Model != "managed" || selected.MCPServers["search"].Command != "node" {
				t.Fatal("shared contributions did not survive composition")
			}
			status, err := workflowstatus.Run(t.Context(), workflowstatus.CommandInput{ManifestPath: manifest})
			if err != nil || !status.CheckClean() {
				t.Fatalf("settled document did not remain clean: %v; %+v", err, status.Reconciliation)
			}
			if repeated := apply(mcp + native); repeated.ExecutionAttempted || repeated.ActionCount != 0 {
				t.Fatalf("settled shared configuration was not a no-op: actions=%d attempted=%v", repeated.ActionCount, repeated.ExecutionAttempted)
			}
			if scenario.nativeOnlyObservation {
				prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifest})
				if err != nil {
					t.Fatal(err)
				}
				defer prepared.Close()
				selected := prepared.lifecycle.planned.context
				nativeOnly, err := selected.RuntimeEnvironment.HostConfigsOnly()
				if err != nil {
					t.Fatal(err)
				}
				epoch, err := nativeconfig.LoadPersistenceEpoch(t.Context(), selected.Paths)
				if err != nil {
					t.Fatal(err)
				}
				observed, err := nativeconfig.ObserveDocuments(t.Context(), nativeconfig.DocumentObservationInput{
					Paths: selected.Paths, Environment: nativeOnly, Lockfile: selected.Lockfile,
					Selection: selected.Selection, PersistenceEpoch: &epoch,
				})
				if err != nil {
					t.Fatal("native-only refresh observation lost the established MCP member", err)
				}
				defer observed.Close()
				input, present := observed.NativeInput()
				if !present || input.Documents == nil {
					t.Fatal("native-only observation was unavailable")
				}
				validationErr := input.Documents.Evidence().ValidateBefore(t.Context())
				closeErr := observed.Close()
				if validationErr != nil || closeErr != nil {
					t.Fatalf("native-only member validation failed: %v; close: %v", validationErr, closeErr)
				}
			}
			if scenario.recovery {
				nativePath := filepath.Join(nativeRoot, "config.toml")
				original, err := os.ReadFile(nativePath)
				if err != nil {
					t.Fatal(err)
				}
				original = append([]byte("# retained foreign syntax\nunmanaged = 'kept'\n"), original...)
				if err := os.WriteFile(nativePath, original, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(source, []byte("model='changed'\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: manifest}); err != nil {
					t.Fatal(err)
				}
				prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifest})
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(t.Context())
				filesystem := &interruptSharedDocumentPublication{Store: storagecommit.Adapter{}, host: nativePath, cancel: cancel}
				_, executeErr := ExecuteWithOptions(ctx, prepared, ExecuteOptions{NativeFilesystem: filesystem})
				cancel()
				if err := prepared.Close(); err != nil {
					t.Fatal(err)
				}
				if !filesystem.interrupted || !errors.Is(executeErr, context.Canceled) {
					t.Fatalf("native publication was not interrupted after its real write: %v", executeErr)
				}
				current, err := os.ReadFile(nativePath)
				if err != nil || bytes.Equal(current, original) {
					t.Fatal("interruption did not retain a real native publication", err)
				}

				recovery, err := workflowrecover.Plan(t.Context(), workflowrecover.PlanInput{ManifestPath: manifest})
				if err != nil {
					t.Fatal(err)
				}
				_, recoverErr := workflowrecover.Execute(t.Context(), recovery, workflowrecover.ExecuteOptions{})
				closeErr := recovery.Close()
				if recoverErr != nil || closeErr != nil {
					t.Fatalf("loaded shared-document recovery failed: %v; close: %v", recoverErr, closeErr)
				}
				restored, err := os.ReadFile(nativePath)
				if err != nil || !bytes.Equal(restored, original) {
					t.Fatal("recovery lost the shared document's original bytes", err)
				}
				apply(mcp + native)
				retried := read(nativePath)
				if retried.Model != "changed" || retried.MCPServers["search"].Command != "node" {
					t.Fatal("retry failed to publish the changed native value and preserve MCP")
				}
				status, err := workflowstatus.Run(t.Context(), workflowstatus.CommandInput{ManifestPath: manifest})
				if err != nil || !status.CheckClean() {
					t.Fatal("recovered and retried document was not clean", err)
				}
			}
			if scenario.removal {
				apply(native)
				remaining := read(filepath.Join(nativeRoot, "config.toml"))
				if remaining.Model != "managed" || len(remaining.MCPServers) != 0 {
					t.Fatal("removing MCP changed the surviving native configuration")
				}
				apply(mcp + native)
				apply(mcp)
				remaining = read(filepath.Join(ordinaryRoot, "config.toml"))
				if remaining.Model != "" || remaining.MCPServers["search"].Command != "node" {
					t.Fatal("removing native keys changed the surviving MCP configuration")
				}
				if repeated := apply(mcp); repeated.ExecutionAttempted || repeated.ActionCount != 0 {
					t.Fatal("ordinary-only state did not return to a true no-op")
				}
				status, err := workflowstatus.Run(t.Context(), workflowstatus.CommandInput{ManifestPath: manifest})
				if err != nil || !status.CheckClean() {
					t.Fatal("removed native configuration did not leave clean MCP management", err)
				}
			}
			if scenario.symlink {
				if err := os.WriteFile(source, []byte("model='changed'\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: manifest}); err != nil {
					t.Fatal(err)
				}
				prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifest})
				if err != nil {
					t.Fatal(err)
				}
				defer prepared.Close()

				var request mutation.PhysicalAuthorityRequest
				for _, decision := range prepared.lifecycle.planned.assessment.Reconciliation.Aggregates() {
					if _, native := aggregate.HostConfigSyntaxForCodec(decision.CodecContractID()); native {
						continue
					}
					if decision.MutatesHost() || decision.MutatesState() {
						t.Fatal("selected MCP was not a no-op source dependency")
					}
					request, err = nativeAuthorityRequestFor(prepared.lifecycle.planned, decision.DocumentAddress())
					if err != nil {
						t.Fatal(err)
					}
				}
				if request.Path == "" {
					t.Fatal("no selected ordinary no-op participated in the native plan")
				}

				store, err := mutation.NewStore(filepath.Join(base, "source-lease-probe"))
				if err != nil {
					t.Fatal(err)
				}
				leases, err := store.Acquire(t.Context(), prepared.lifecycle.authorityEvidence.domains...)
				if err != nil {
					t.Fatal(err)
				}
				defer leases.Release()
				if matches, err := leases.MatchCurrentPhysicalRequests(t.Context(), request); err != nil || !matches {
					t.Fatalf("initial native request was not covered: %v", err)
				}

				nativePath := filepath.Join(nativeRoot, "config.toml")
				original, err := os.ReadFile(nativePath)
				if err != nil {
					t.Fatal(err)
				}
				other := filepath.Join(base, "different-ordinary-source")
				if err := os.MkdirAll(other, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(other, "config.toml"), original, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(home, ".codex")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(other, filepath.Join(home, ".codex")); err != nil {
					t.Fatal(err)
				}
				if matches, err := leases.MatchCurrentPhysicalRequests(t.Context(), request); err != nil || matches {
					t.Fatalf("native request ignored the retargeted no-op source: matches=%v error=%v", matches, err)
				}
				if err := leases.Release(); err != nil {
					t.Fatal(err)
				}

				if _, err := ExecuteWithOptions(t.Context(), prepared, ExecuteOptions{}); err == nil {
					t.Fatal("retargeted ordinary source was applied")
				}
				for _, path := range []string{nativePath, filepath.Join(other, "config.toml")} {
					current, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(current, original) {
						t.Fatalf("refused source retarget changed %q: %v", path, err)
					}
				}
				if fresh, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifest}); err == nil {
					fresh.Close()
					t.Fatal("a new plan silently reassigned established MCP management to the retargeted source")
				}
				if err := os.Remove(filepath.Join(home, ".codex")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(nativeRoot, filepath.Join(home, ".codex")); err != nil {
					t.Fatal(err)
				}
				t.Setenv("CODEX_HOME", other)
				if fresh, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifest}); err == nil {
					fresh.Close()
					t.Fatal("a new native locator silently reassigned established native management")
				}
				t.Setenv("CODEX_HOME", nativeRoot)
				apply(mcp + native)
				if restored := read(nativePath); restored.Model != "changed" || restored.MCPServers["search"].Command != "node" {
					t.Fatal("restoring both selectors failed to restore a valid source plan")
				}
			}
		})
	}
}

type interruptSharedDocumentPublication struct {
	mutationfs.Store
	host        string
	cancel      context.CancelFunc
	interrupted bool
}

func (filesystem *interruptSharedDocumentPublication) ReplaceRootedFile(ctx context.Context, capability rootedpath.CommitCapability, content []byte, mode os.FileMode, expected mutationfs.EntryIdentity) error {
	path, err := capability.Destination().LexicalPath()
	if err != nil {
		return errors.Join(err, capability.Close())
	}
	if path == filesystem.host && filesystem.interrupted {
		return errors.Join(errors.New("shared document compensation is unavailable"), capability.Close())
	}
	if err := filesystem.Store.ReplaceRootedFile(ctx, capability, content, mode, expected); err != nil {
		return err
	}
	if path == filesystem.host && !filesystem.interrupted {
		filesystem.interrupted = true
		filesystem.cancel()
		return context.Canceled
	}
	return nil
}
