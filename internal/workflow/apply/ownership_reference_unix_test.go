//go:build darwin || linux

package apply

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestOwnershipWritersSupplyObservedReferences(t *testing.T) {
	for _, test := range []struct {
		name, scope       string
		native            bool
		provisional       bool
		ordinaryPromotion bool
	}{
		{name: "ordinary global claim", scope: "global"},
		{name: "native project claim and binding", scope: "project", native: true},
		{name: "native global claim and binding", scope: "global", native: true},
		{name: "native provisional publication", scope: "global", native: true, provisional: true},
		{name: "native ordinary provisional publication", scope: "project", native: true, ordinaryPromotion: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if (test.provisional || test.ordinaryPromotion) && runtime.GOOS != "darwin" {
				t.Skip("normalization-sensitive provisional paths require Darwin")
			}
			base := t.TempDir()
			for key, directory := range map[string]string{
				"HOME": "home", "XDG_CONFIG_HOME": "config", "XDG_STATE_HOME": "state", "XDG_DATA_HOME": "data",
				"PI_CODING_AGENT_DIR": "pi", "CODEX_HOME": "codex",
			} {
				selected := filepath.Join(base, directory)
				t.Setenv(key, selected)
				if err := os.MkdirAll(selected, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if test.provisional {
				prompts := filepath.Join(os.Getenv("PI_CODING_AGENT_DIR"), "prompts")
				if err := os.Mkdir(prompts, 0o700); err != nil {
					t.Fatal(err)
				}
				observed, err := mutation.ObserveDirectoryEntryAuthority(filepath.Join(prompts, "Caf\u00e9.md"))
				if err != nil {
					t.Fatal(err)
				}
				if _, provisional := observed.Provisional(); !provisional {
					t.Skip("filesystem provides exact missing-path semantics")
				}
			}
			if test.ordinaryPromotion {
				skills := filepath.Join(os.Getenv("HOME"), ".pi", "agent", "skills")
				if err := os.MkdirAll(skills, 0o700); err != nil {
					t.Fatal(err)
				}
				observed, err := mutation.ObserveDirectoryEntryAuthority(filepath.Join(skills, "Caf\u00e9"))
				if err != nil {
					t.Fatal(err)
				}
				if _, provisional := observed.Provisional(); !provisional {
					t.Skip("filesystem provides exact missing-path semantics")
				}
			}
			physical := filepath.Join(base, "workspace")
			if err := os.Mkdir(physical, 0o700); err != nil {
				t.Fatal(err)
			}
			selected := filepath.Join(base, "selected")
			if err := os.Symlink(physical, selected); err != nil {
				t.Fatal(err)
			}
			paths := applyTestPaths(t, selected)
			if test.native {
				source := filepath.Join(selected, "preferences.json")
				destination, policy, body := "settings.json", "keys", `{"theme":"located-registry"}`
				if test.provisional {
					destination, policy, body = "prompts/Caf\u00e9.md", "document", "published prompt\n"
				}
				manifest := fmt.Sprintf("version=1\ntargets=['pi']\n[[host_config]]\nid='preferences'\nscope=%q\npath=%q\nsource=%q\nownership=%q\n", test.scope, destination, source, policy)
				if test.ordinaryPromotion {
					skill := filepath.Join(selected, "skill-source")
					writeApplyFile(t, filepath.Join(skill, "SKILL.md"), "---\nname: Caf\u00e9\ndescription: native ordinary publication\n---\n")
					manifest += fmt.Sprintf("[[skill]]\nname='Caf\u00e9'\nscope='global'\nsource={path=%q,mode='vendor'}\ntargets=['pi']\n", skill)
				}
				writeApplyFile(t, paths.ManifestPath, manifest)
				writeApplyFile(t, source, body)
			} else {
				source := filepath.Join(selected, "instructions.md")
				writeApplyFile(t, paths.ManifestPath, fmt.Sprintf("version=1\ntargets=['codex']\n[instructions.global]\nsource=%q\n", source))
				writeApplyFile(t, source, "observed ownership\n")
			}
			if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: paths.ManifestPath}); err != nil {
				t.Fatal(err)
			}
			owner := seedWorkflowStateImage(t, paths)
			image, err := ownershipstore.NewRegistryImage(ownership.EmptyRegistry(), nil)
			if err != nil {
				t.Fatal(err)
			}
			content, err := ownershipstore.MarshalImage(image)
			if err != nil {
				t.Fatal(err)
			}
			writeApplyFile(t, paths.OwnershipRegistryPath, string(content))
			prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: paths.ManifestPath})
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			if _, err := ExecuteWithOptions(t.Context(), prepared, ExecuteOptions{PlanWasDisclosed: true}); err != nil {
				t.Fatal(err)
			}
			content, err = os.ReadFile(paths.OwnershipRegistryPath)
			if err != nil {
				t.Fatal(err)
			}
			image, err = ownershipstore.UnmarshalImage(content)
			wantReferences := 2
			if test.ordinaryPromotion {
				wantReferences++
			}
			if err != nil || len(image.References()) != wantReferences {
				t.Fatal("writer downgraded the registry or omitted required references", err)
			}
			actualOwner, present := image.Reference(owner.Identity())
			if !present || !actualOwner.Equal(owner) {
				t.Fatal("writer lost the selected State self alias")
			}
			for _, reference := range image.References() {
				if !reference.Identity().Equal(owner.Identity()) {
					path := reference.Locator()
					entry, err := os.Stat(path)
					if err != nil {
						t.Fatal("output reference did not locate the published host effect", err)
					}
					if entry.IsDir() {
						path = filepath.Join(path, "SKILL.md")
					}
					if content, err := os.ReadFile(path); err != nil || len(content) == 0 {
						t.Fatal("document reference did not locate the published host effect", err)
					}
				}
			}
			claims, bindings := image.Registry().Claims(), image.Registry().DocumentBindings()
			if len(claims) == 0 || (len(bindings) != 0) != test.native {
				t.Fatalf("fixture ownership producer: claims=%d bindings=%d scope=%s native=%t", len(claims), len(bindings), test.scope, test.native)
			}
			stateBytes, err := os.ReadFile(paths.StatefilePath)
			if err != nil {
				t.Fatal(err)
			}
			state, err := statefile.UnmarshalImage(stateBytes)
			if err != nil || !state.Reference().Equal(owner) {
				t.Fatal("ownership update changed State encoding or self reference", err)
			}
		})
	}
}
