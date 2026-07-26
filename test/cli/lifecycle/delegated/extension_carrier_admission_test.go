package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestRunLockRejectsNonAdmittedExtensionCarriersWithoutWritingLockfile(t *testing.T) {
	for _, carrier := range []string{
		"claude-plugin",
		"pi-extension",
	} {
		t.Run(carrier, func(t *testing.T) {
			tempDir := t.TempDir()
			manifestPath := filepath.Join(tempDir, "daem.toml")
			lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
			originalLockfile := "version = 1\n# plugin carriers are not admitted\n"
			testkit.WriteFile(t, tempDir, "daem.toml", futureExtensionManifest(carrier))
			testkit.WriteFile(t, tempDir, "daem.lock.toml", originalLockfile)

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
			if exitCode != 1 {
				t.Fatalf("exitCode = %d, want 1; stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			for _, want := range []string{"invalid manifest", "unsupported extension carrier", carrier, "claude-code-plugin"} {
				if !strings.Contains(stderr.String(), want) {
					t.Fatalf("stderr = %q, want %q", stderr.String(), want)
				}
			}
			testkit.AssertFileContent(t, lockfilePath, originalLockfile)
		})
	}
}

func TestRunLockRejectsNonAdmittedAntigravityExtensionRowsWithoutWritingLockfile(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		want     string
	}{
		{
			name: "inherited global scope",
			manifest: `
version = 1
targets = ["antigravity-cli"]

[defaults]
scope = "global"

[[extension]]
id = "guidance-managed"
carrier = "antigravity-cli-plugin"
source = { host_source = "modern-web-guidance@google" }
`,
			want: "requires explicit scope",
		},
		{
			name: "marketplace source",
			manifest: `
version = 1
targets = ["antigravity-cli"]

[[extension]]
id = "guidance-managed"
carrier = "antigravity-cli-plugin"
scope = "global"
source = { marketplace = "modern-web-guidance@google" }
`,
			want: `requires source kind "host-source"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tempDir := t.TempDir()
			manifestPath := filepath.Join(tempDir, "daem.toml")
			lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
			originalLockfile := "version = 1\n# invalid Antigravity rows must not rewrite lockfile\n"
			testkit.WriteFile(t, tempDir, "daem.toml", test.manifest)
			testkit.WriteFile(t, tempDir, "daem.lock.toml", originalLockfile)

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
			if exitCode != 1 {
				t.Fatalf("exitCode = %d, want 1; stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), "invalid manifest") || !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("stderr = %q, want invalid manifest and %q", stderr.String(), test.want)
			}
			testkit.AssertFileContent(t, lockfilePath, originalLockfile)
		})
	}
}

func TestRunLockRejectsInvalidCodexExtensionLocalityWithoutWritingLockfile(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		wants    []string
	}{
		{
			name:     "project scope",
			manifest: codexProjectExtensionManifest(),
			wants: []string{
				"invalid manifest",
				"extension[0]",
				`extension carrier "codex-plugin" does not support scope`,
				"project",
			},
		},
		{
			name:     "default global scope",
			manifest: codexDefaultGlobalExtensionManifest(),
			wants: []string{
				"invalid manifest",
				"extension[0].scope",
				`extension carrier "codex-plugin" requires explicit scope = "global"`,
				"defaults.scope does not authorize this host mutation",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tempDir := t.TempDir()
			manifestPath := filepath.Join(tempDir, "daem.toml")
			lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
			originalLockfile := "version = 1\n# invalid Codex locality rows must not rewrite lockfile\n"
			testkit.WriteFile(t, tempDir, "daem.toml", test.manifest)
			testkit.WriteFile(t, tempDir, "daem.lock.toml", originalLockfile)

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
			if exitCode != 1 {
				t.Fatalf("exitCode = %d, want 1; stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			for _, want := range test.wants {
				if !strings.Contains(stderr.String(), want) {
					t.Fatalf("stderr = %q, want %q", stderr.String(), want)
				}
			}
			assertNoPluginDiagnosticAuthority(t, stderr.String())
			testkit.AssertFileContent(t, lockfilePath, originalLockfile)
		})
	}
}

func TestRunLockRejectsUnsupportedExtensionFieldsWithoutWritingLockfile(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		want     string
	}{
		{
			name: "broad inherited targets",
			manifest: `
version = 1
targets = ["claude-code", "codex"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = { marketplace = "context7@market" }
`,
			want: "supports exactly one target",
		},
		{
			name: "removed absence policy field",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = { marketplace = "context7@market" }
on_absent = "remove-binding"
`,
			want: "unknown manifest key",
		},
		{
			name: "url source field",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = { url = "https://example.invalid/context7" }
`,
			want: "unknown manifest key",
		},
		{
			name: "git source field",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = { git = "https://github.com/acme/context7.git", path = "plugins/context7", ref = "main" }
`,
			want: "unknown manifest key",
		},
		{
			name: "github source field",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = { github = "acme/context7" }
`,
			want: "unknown manifest key",
		},
		{
			name: "local path source field",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = { path = "./plugins/context7" }
`,
			want: "unknown manifest key",
		},
		{
			name: "npm source field",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = { npm = "@acme/context7" }
`,
			want: "unknown manifest key",
		},
		{
			name: "source selector field",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = { selector = "marketplace:context7" }
`,
			want: "unknown manifest key",
		},
		{
			name: "string source selector",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = "marketplace:context7"
`,
			want: "expected table but found string",
		},
		{
			name: "empty source table",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = {}
`,
			want: "set exactly one of marketplace or host_source",
		},
		{
			name: "raw host config source fields",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = { command = "npx", args = ["-y", "@acme/context7"] }
`,
			want: "unknown manifest key",
		},
		{
			name: "host source field",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = { host_source = "@acme/context7" }
`,
			want: `requires source kind "marketplace"`,
		},
		{
			name: "host source URL credentials",
			manifest: `
version = 1
targets = ["opencode"]

[[extension]]
id = "formatter"
carrier = "opencode-plugin"
source = { host_source = "https://user:secret@example.com/formatter.tgz" }
`,
			want: "inline credentials",
		},
		{
			name: "host source query credential",
			manifest: `
version = 1
targets = ["pi"]

[[extension]]
id = "tools"
carrier = "pi-package"
source = { host_source = "npm:@acme/tools?access_token=secret" }
`,
			want: "query fields",
		},
		{
			name: "marketplace and host source",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = { marketplace = "context7@market", host_source = "@acme/context7" }
`,
			want: "set exactly one of marketplace or host_source",
		},
		{
			name: "inherited global scope",
			manifest: `
version = 1
targets = ["claude-code"]

[defaults]
scope = "global"

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = { marketplace = "context7@market" }
`,
			want: "requires explicit scope",
		},
		{
			name: "public user scope",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
scope = "user"
source = { marketplace = "context7@market" }
`,
			want: `unknown scope "user"`,
		},
		{
			name: "host local scope",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
scope = "local"
source = { marketplace = "context7@market" }
`,
			want: `unknown scope "local"`,
		},
		{
			name: "host managed scope",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
scope = "managed"
source = { marketplace = "context7@market" }
`,
			want: `unknown scope "managed"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tempDir := t.TempDir()
			manifestPath := filepath.Join(tempDir, "daem.toml")
			lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
			originalLockfile := "version = 1\n# unsupported fields must not rewrite lockfile\n"
			testkit.WriteFile(t, tempDir, "daem.toml", test.manifest)
			testkit.WriteFile(t, tempDir, "daem.lock.toml", originalLockfile)

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
			if exitCode != 1 {
				t.Fatalf("exitCode = %d, want 1; stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), "invalid manifest") || !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("stderr = %q, want invalid manifest and %q", stderr.String(), test.want)
			}
			testkit.AssertFileContent(t, lockfilePath, originalLockfile)
		})
	}
}

func TestRunUpdateCommandIsNotCarrierLifecycleAlias(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", claudeGlobalExtensionManifest())

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"update", "extension", "context7-global",
		"--manifest", manifestPath,
	}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2; stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), `unknown command "update"`) {
		t.Fatalf("stderr = %q, want unknown update command", stderr.String())
	}
	assertNoClaudePluginUpdateClaims(t, stderr.String())
	testkit.AssertPathMissing(t, lockfilePath)
	if _, err := os.Stat(filepath.Join(tempDir, ".daem")); !os.IsNotExist(err) {
		t.Fatalf("update command created .daem or stat failed unexpectedly: %v", err)
	}
}

func TestStatusAndApplyRejectInvalidCodexExtensionWithoutDiagnosticAuthority(t *testing.T) {
	invalidManifests := []struct {
		name     string
		manifest string
		wants    []string
	}{
		{
			name:     "wrong target",
			manifest: futureExtensionManifest("codex-plugin"),
			wants: []string{
				"invalid manifest",
				`extension carrier "codex-plugin" supports only target`,
				"codex",
				"claude-code",
			},
		},
		{
			name:     "project scope",
			manifest: codexProjectExtensionManifest(),
			wants: []string{
				"invalid manifest",
				`extension carrier "codex-plugin" does not support scope`,
				"project",
			},
		},
		{
			name:     "default global scope",
			manifest: codexDefaultGlobalExtensionManifest(),
			wants: []string{
				"invalid manifest",
				"requires explicit scope = \"global\"",
				"defaults.scope does not authorize this host mutation",
			},
		},
	}

	for _, command := range []struct {
		name string
		args func(manifestPath string, lockfilePath string) []string
	}{
		{
			name: "status",
			args: func(manifestPath string, lockfilePath string) []string {
				return []string{"status", "--manifest", manifestPath, "--json"}
			},
		},
		{
			name: "apply-dry-run",
			args: func(manifestPath string, lockfilePath string) []string {
				return []string{"apply", "--manifest", manifestPath, "--dry-run", "--json"}
			},
		},
	} {
		for _, invalidManifest := range invalidManifests {
			t.Run(command.name+"/"+invalidManifest.name, func(t *testing.T) {
				tempDir := t.TempDir()
				homeDir := filepath.Join(tempDir, "home")
				manifestPath := filepath.Join(tempDir, "daem.toml")
				lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
				originalLockfile := "version = 1\n# status/apply must not mutate on invalid Codex extension rows\n"
				testkit.WriteFile(t, tempDir, "daem.toml", invalidManifest.manifest)
				testkit.WriteFile(t, tempDir, "daem.lock.toml", originalLockfile)
				testkit.WriteFile(t, homeDir, filepath.Join(".codex", "config.toml"), `
[plugins."alpha@market"]
enabled = true
`)
				t.Setenv("HOME", homeDir)
				testkit.SetDefaultRootEnv(t, tempDir)

				var stdout bytes.Buffer
				var stderr bytes.Buffer
				exitCode := testkit.RunVerboseCLI(command.args(manifestPath, lockfilePath), &stdout, &stderr)
				if exitCode != 1 {
					t.Fatalf("exitCode = %d, want 1; stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
				}
				combined := stdout.String() + stderr.String()
				for _, want := range invalidManifest.wants {
					if !strings.Contains(combined, want) {
						t.Fatalf("combined output = %q, want %q", combined, want)
					}
				}
				assertNoPluginDiagnosticAuthority(t, combined)
				testkit.AssertFileContent(t, lockfilePath, originalLockfile)
			})
		}
	}
}

func TestAddRemoveRejectPluginResourcesWithoutManifestOrLockMutation(t *testing.T) {
	tests := []struct {
		name        string
		args        func(manifestPath string) []string
		wantStderr  string
		wantSubject string
	}{
		{
			name: "add plugin",
			args: func(manifestPath string) []string {
				return []string{"add", "plugin", "alpha@market", "--manifest", manifestPath, "--dry-run"}
			},
			wantStderr:  "unknown add resource",
			wantSubject: "plugin",
		},
		{
			name: "remove plugin",
			args: func(manifestPath string) []string {
				return []string{"remove", "plugin", "alpha@market", "--manifest", manifestPath, "--yes"}
			},
			wantStderr:  "unknown remove resource",
			wantSubject: "plugin",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tempDir := t.TempDir()
			manifestPath := filepath.Join(tempDir, "daem.toml")
			lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
			originalManifest := `
version = 1
targets = ["codex"]
`
			originalLockfile := "version = 1\n# not touched by unsupported resource routes\n"
			testkit.WriteFile(t, tempDir, "daem.toml", originalManifest)
			testkit.WriteFile(t, tempDir, "daem.lock.toml", originalLockfile)

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI(test.args(manifestPath), &stdout, &stderr)
			if exitCode != 2 {
				t.Fatalf("exitCode = %d, want 2; stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), test.wantStderr) || !strings.Contains(stderr.String(), test.wantSubject) {
				t.Fatalf("stderr = %q, want %q and %q", stderr.String(), test.wantStderr, test.wantSubject)
			}
			testkit.AssertFileContent(t, manifestPath, originalManifest)
			testkit.AssertFileContent(t, lockfilePath, originalLockfile)
		})
	}
}

func assertNoPluginDiagnosticAuthority(t *testing.T, output string) {
	t.Helper()

	for _, forbidden := range []string{
		"plugin_config",
		"plugin_config_entry",
		"lock_subject",
		"state_subject",
		"install",
		"uninstall",
		"remove carrier",
		"mutation authority",
		"managed",
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("output contains forbidden plugin diagnostic authority marker %q:\n%s", forbidden, output)
		}
	}
}
