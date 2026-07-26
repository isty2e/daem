package cli_test

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunAddExtensionWritesEveryAdmittedCarrierRowAndEquivalentLock(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		target      string
		carrier     string
		scope       string
		sourceKind  string
		sourceField string
		sourceRef   string
		omitTarget  bool
		omitScope   bool
	}{
		{
			name: "claude default project marketplace", id: "claude-managed",
			target: "claude-code", carrier: "claude-code-plugin", scope: "project",
			sourceKind: "marketplace", sourceField: "marketplace", sourceRef: "plugin@market",
			omitTarget: true, omitScope: true,
		},
		{
			name: "codex explicit global marketplace", id: "codex-managed",
			target: "codex", carrier: "codex-plugin", scope: "global",
			sourceKind: "marketplace", sourceField: "marketplace", sourceRef: "plugin@market",
		},
		{
			name: "opencode default project host source", id: "opencode-managed",
			target: "opencode", carrier: "opencode-plugin", scope: "project",
			sourceKind: "host-source", sourceField: "host_source", sourceRef: "@acme/plugin",
			omitScope: true,
		},
		{
			name: "pi default project host source", id: "pi-managed",
			target: "pi", carrier: "pi-package", scope: "project",
			sourceKind: "host-source", sourceField: "host_source", sourceRef: "github:acme/plugin",
			omitScope: true,
		},
		{
			name: "antigravity explicit global host source", id: "antigravity-managed",
			target: "antigravity-cli", carrier: "antigravity-cli-plugin", scope: "global",
			sourceKind: "host-source", sourceField: "host_source", sourceRef: "plugin@publisher",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			manifestPath := filepath.Join(root, "daem.toml")
			lockfilePath := filepath.Join(root, "daem.lock.toml")
			original := fmt.Sprintf("version = 1\ntargets = [%q]\n", test.target)
			testkit.WriteFile(t, root, "daem.toml", original)

			args := []string{
				"add", "extension", test.id, test.sourceRef,
				"--manifest", manifestPath,
			}
			if !test.omitTarget {
				args = append(args, "--target", test.target)
			}
			if !test.omitScope {
				args = append(args, "--scope", test.scope)
			}
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
			if exitCode != 0 || stderr.Len() != 0 {
				t.Fatalf("add exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}

			manifestBytes := testkit.ReadFile(t, manifestPath)
			expectedManifest := fmt.Sprintf(`%s
[[extension]]
id = %q
carrier = %q
targets = [%q]
scope = %q
source = { %s = %q }
`, original, test.id, test.carrier, test.target, test.scope, test.sourceField, test.sourceRef)
			if string(manifestBytes) != expectedManifest {
				t.Fatalf("manifest = %q, want %q", manifestBytes, expectedManifest)
			}
			normalized, err := declarationmanifest.Decode(manifestBytes)
			if err != nil {
				t.Fatalf("declarationmanifest.Decode returned error: %v", err)
			}
			if len(normalized.Extensions()) != 1 {
				t.Fatalf("extensions = %#v, want one", normalized.Extensions())
			}
			extension := normalized.Extensions()[0]
			if extension.ID().Name() != test.id || string(extension.Carrier()) != test.carrier ||
				string(extension.Target()) != test.target || string(extension.Scope()) != test.scope ||
				string(extension.Source().Kind()) != test.sourceKind || extension.Source().Ref() != test.sourceRef {
				t.Fatalf("extension = %#v, want %s/%s/%s/%s/%s", extension, test.carrier, test.target, test.scope, test.sourceKind, test.sourceRef)
			}

			directRoot := t.TempDir()
			directManifestPath := filepath.Join(directRoot, "daem.toml")
			directLockfilePath := filepath.Join(directRoot, "daem.lock.toml")
			testkit.WriteFile(t, directRoot, "daem.toml", expectedManifest)
			stdout.Reset()
			stderr.Reset()
			exitCode = testkit.RunVerboseCLI([]string{
				"lock", "--manifest", directManifestPath,
			}, &stdout, &stderr)
			if exitCode != 0 || stderr.Len() != 0 {
				t.Fatalf("direct lock exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			if !bytes.Equal(testkit.ReadFile(t, lockfilePath), testkit.ReadFile(t, directLockfilePath)) {
				t.Fatalf("authoring lock differs from direct-manifest lock")
			}
		})
	}
}

func TestRunAddExtensionRejectsInvalidCarrierSelectionWithoutWrites(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		targets  []string
		scope    string
		manifest string
		want     string
	}{
		{
			name:     "ambiguous inherited targets",
			source:   "plugin@market",
			manifest: "version = 1\ntargets = [\"claude-code\", \"opencode\"]\n",
			want:     "ambiguous across manifest targets",
		},
		{
			name: "codex project scope", source: "plugin@market", targets: []string{"codex"}, scope: "project",
			want: "--target codex supports only --scope global",
		},
		{
			name: "antigravity project scope", source: "plugin@publisher", targets: []string{"antigravity-cli"}, scope: "project",
			want: "--target antigravity-cli supports only --scope global",
		},
		{
			name: "missing source", targets: []string{"opencode"},
			want: "missing extension source",
		},
		{
			name: "multiple targets", source: "@acme/plugin", targets: []string{"opencode", "pi"},
			want: "extension authoring accepts at most one distinct --target",
		},
		{
			name: "source leading whitespace", source: " @acme/plugin", targets: []string{"pi"},
			want: "extension source must not contain leading or trailing whitespace",
		},
		{
			name: "source control character", source: "@acme/plugin\nnext", targets: []string{"pi"},
			want: "extension source must not contain control characters",
		},
		{
			name: "source option looking", source: "--approve", targets: []string{"pi"},
			want: "extension source must not begin with '-'",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			manifestPath := filepath.Join(root, "daem.toml")
			lockfilePath := filepath.Join(root, "daem.lock.toml")
			original := test.manifest
			if original == "" {
				original = "version = 1\ntargets = [\"claude-code\"]\n"
			}
			testkit.WriteFile(t, root, "daem.toml", original)

			args := []string{"add", "extension", "invalid"}
			if strings.HasPrefix(test.source, "-") {
				args = []string{"add", "extension", "--manifest", manifestPath}
				for _, target := range test.targets {
					args = append(args, "--target", target)
				}
				if test.scope != "" {
					args = append(args, "--scope", test.scope)
				}
				args = append(args, "--dry-run", "--", "invalid", test.source)
			} else {
				if test.source != "" {
					args = append(args, test.source)
				}
				args = append(args, "--manifest", manifestPath)
				for _, target := range test.targets {
					args = append(args, "--target", target)
				}
				if test.scope != "" {
					args = append(args, "--scope", test.scope)
				}
				args = append(args, "--dry-run")
			}
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunCLI(args, &stdout, &stderr)
			if exitCode == 0 || !bytes.Contains(stderr.Bytes(), []byte(test.want)) {
				t.Fatalf("exitCode=%d stdout=%q stderr=%q, want %q", exitCode, stdout.String(), stderr.String(), test.want)
			}
			testkit.AssertFileContent(t, manifestPath, original)
			testkit.AssertPathMissing(t, lockfilePath)
		})
	}
}
