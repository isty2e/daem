package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

func TestNativeWholeDiffUsesTheSelectedRootAfterObservationCloses(t *testing.T) {
	for _, selected := range []string{"pi", "codex", "claude-code", "opencode", "antigravity-cli"} {
		t.Run(selected, func(t *testing.T) {
			_, manifest, _, _ := captureCLIFixture(t, `{}`)
			host, path := captureHostDocument(t, manifest, selected, "global", "captured.jsonc", "document")
			before, after := []byte("{\"chosen\":1}\n"), []byte("{\"chosen\":2}\n")
			if err := os.WriteFile(host, before, 0o600); err != nil {
				t.Fatal(err)
			}
			args := []string{"import", "host-config", "chosen", "--manifest", manifest, "--target", selected, "--scope", "global", "--host-path", path, "--ownership", "document"}
			fingerprint := captureFingerprint(t, args)
			for _, command := range [][]string{
				append(args, "--yes", "--expect-plan", fingerprint),
				{"lock", "--manifest", manifest},
				{"apply", "--manifest", manifest, "--manage-existing", "--yes", "--verbose"},
			} {
				if code, out, diagnostic := runCaptureCLI(t, command); code != 0 {
					t.Fatal(command, code, out, diagnostic)
				}
			}
			content, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatal(err)
			}
			environment, err := declarationmanifest.Decode(content)
			if err != nil {
				t.Fatal(err)
			}
			local, _ := environment.HostConfigs()[0].Source().Local()
			if err := os.WriteFile(local.Path(), after, 0o600); err != nil {
				t.Fatal(err)
			}
			if code, out, diagnostic := runCaptureCLI(t, []string{"lock", "--manifest", manifest}); code != 0 {
				t.Fatal(code, out, diagnostic)
			}
			planned, err := applyworkflow.PlanDryRun(t.Context(), applyworkflow.CommandInput{ManifestPath: manifest})
			if err != nil {
				t.Fatal(err)
			}
			diffs, err := applyworkflow.BuildDiffs(t.Context(), planned)
			if err != nil || len(diffs.Diffs) != 1 || !bytes.Equal(diffs.Diffs[0].CurrentContent, before) || !bytes.Equal(diffs.Diffs[0].DesiredContent, after) {
				t.Fatal("native diff selected a different document", diffs, err)
			}
			if code, out, diagnostic := runCaptureCLI(t, []string{"apply", "--manifest", manifest, "--dry-run", "--diff"}); code != 0 {
				t.Fatal("native CLI diff failed", code, out, diagnostic)
			}
			actual, err := os.ReadFile(host)
			if err != nil || !bytes.Equal(actual, before) {
				t.Fatal("diff changed the selected host", err)
			}
		})
	}
}

func TestNativeMutationCannotWriteInsideAnOrdinarySourceAtACustomRoot(t *testing.T) {
	for _, selected := range []string{"claude-code", "opencode"} {
		t.Run(selected, func(t *testing.T) {
			root, manifest, _, _ := captureCLIFixture(t, `{}`)
			host, path := captureHostDocument(t, manifest, selected, "global", "captured.jsonc", "document")
			source := filepath.Join(root, "source.jsonc")
			if err := os.WriteFile(source, []byte(`{"chosen":2}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(filepath.Dir(host), "SKILL.md"), []byte("---\nname: from-root\ndescription: source containment fixture\n---\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			body := fmt.Sprintf("version=1\ntargets=[%q]\n[[host_config]]\nid='chosen'\nscope='global'\npath=%q\nsource=%q\nownership='document'\n[[skill]]\nname='from-root'\nscope='global'\nsource={path=%q,mode='vendor'}\n", selected, path, source, filepath.Dir(host))
			if err := os.WriteFile(manifest, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if code, out, diagnostic := runCaptureCLI(t, []string{"lock", "--manifest", manifest}); code != 0 {
				t.Fatal(code, out, diagnostic)
			}
			prepared, err := applyworkflow.PlanWrite(t.Context(), applyworkflow.CommandInput{ManifestPath: manifest})
			_ = prepared.Close()
			if err == nil || !strings.Contains(err.Error(), "local source") {
				t.Fatal("native mutation escaped local-source containment", err)
			}
			if _, err := os.Lstat(host); !os.IsNotExist(err) {
				t.Fatal("refused plan created the native document", err)
			}
		})
	}
}
