package hostroute

import (
	"path/filepath"
	"slices"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
)

func TestRefreshAdapterMatrixPreservesExactHostRoutesAndOperationIdentity(t *testing.T) {
	type fixtureBuilder func(*testing.T, string) builtFixture
	tests := []struct {
		name             string
		build            fixtureBuilder
		wantCommand      string
		wantArgs         []string
		wantRoute        string
		wantContract     string
		wantInstallRoute string
	}{
		{
			name: "Claude Code project",
			build: func(t *testing.T, workDir string) builtFixture {
				return newHostRouteFixture(t, hostRouteFixture{
					sourceKind: desiredextension.SourceKindMarketplace,
					sourceRef:  "context7@official",
					subjectKey: "context7@official",
					scope:      target.ScopeProject,
					workDir:    workDir,
				})
			},
			wantCommand:      "claude",
			wantArgs:         []string{"plugin", "update", "context7@official", "--scope", "project"},
			wantRoute:        "claude-code.plugin-carrier.refresh",
			wantContract:     "claude-plugin-refresh-v1",
			wantInstallRoute: "claude-code.plugin-carrier.install",
		},
		{
			name: "Claude Code explicit global",
			build: func(t *testing.T, workDir string) builtFixture {
				return newHostRouteFixture(t, hostRouteFixture{
					sourceKind: desiredextension.SourceKindMarketplace,
					sourceRef:  "context7@official",
					subjectKey: "context7@official",
					scope:      target.ScopeGlobal,
					workDir:    workDir,
				})
			},
			wantCommand:      "claude",
			wantArgs:         []string{"plugin", "update", "context7@official", "--scope", "user"},
			wantRoute:        "claude-code.plugin-carrier.refresh",
			wantContract:     "claude-plugin-refresh-v1",
			wantInstallRoute: "claude-code.plugin-carrier.install",
		},
		{
			name: "Codex explicit global",
			build: func(t *testing.T, workDir string) builtFixture {
				return newCodexHostRouteFixture(t, codexHostRouteFixture{
					sourceRef:  "documents@openai-primary-runtime",
					subjectKey: "documents@openai-primary-runtime",
					scope:      target.ScopeGlobal,
					workDir:    workDir,
				})
			},
			wantCommand: "codex",
			wantArgs: []string{
				"plugin",
				"marketplace",
				"upgrade",
				"openai-primary-runtime",
				"--json",
			},
			wantRoute:        "codex.plugin-marketplace.refresh",
			wantContract:     "codex-plugin-marketplace-refresh-v1",
			wantInstallRoute: "codex.plugin-carrier.install",
		},
		{
			name: "OpenCode project",
			build: func(t *testing.T, workDir string) builtFixture {
				return newOpenCodeHostRouteFixture(t, hostSourceRouteFixture{
					sourceRef:  "@acme/opencode-formatter",
					subjectKey: "@acme/opencode-formatter",
					scope:      target.ScopeProject,
					workDir:    workDir,
				})
			},
			wantCommand:      "opencode",
			wantArgs:         []string{"plugin", "@acme/opencode-formatter", "--force"},
			wantRoute:        "opencode.plugin-carrier.refresh",
			wantContract:     "opencode-plugin-refresh-v1",
			wantInstallRoute: "opencode.plugin-carrier.install",
		},
		{
			name: "OpenCode explicit global",
			build: func(t *testing.T, workDir string) builtFixture {
				return newOpenCodeHostRouteFixture(t, hostSourceRouteFixture{
					sourceRef:  "@acme/opencode-formatter",
					subjectKey: "@acme/opencode-formatter",
					scope:      target.ScopeGlobal,
					workDir:    workDir,
				})
			},
			wantCommand: "opencode",
			wantArgs: []string{
				"plugin",
				"@acme/opencode-formatter",
				"--force",
				"--global",
			},
			wantRoute:        "opencode.plugin-carrier.refresh",
			wantContract:     "opencode-plugin-refresh-v1",
			wantInstallRoute: "opencode.plugin-carrier.install",
		},
		{
			name: "Pi project",
			build: func(t *testing.T, workDir string) builtFixture {
				return newPiHostRouteFixture(t, hostSourceRouteFixture{
					sourceRef:  "github:acme/pi-tools",
					subjectKey: "github:acme/pi-tools",
					scope:      target.ScopeProject,
					workDir:    workDir,
				})
			},
			wantCommand:      "pi",
			wantArgs:         []string{"update", "--extension", "github:acme/pi-tools"},
			wantRoute:        "pi.package-carrier.refresh",
			wantContract:     "pi-package-refresh-v1",
			wantInstallRoute: "pi.package-carrier.install",
		},
		{
			name: "Pi explicit global",
			build: func(t *testing.T, workDir string) builtFixture {
				return newPiHostRouteFixture(t, hostSourceRouteFixture{
					sourceRef:  "github:acme/pi-tools",
					subjectKey: "github:acme/pi-tools",
					scope:      target.ScopeGlobal,
					workDir:    workDir,
				})
			},
			wantCommand:      "pi",
			wantArgs:         []string{"update", "--extension", "github:acme/pi-tools"},
			wantRoute:        "pi.package-carrier.refresh",
			wantContract:     "pi-package-refresh-v1",
			wantInstallRoute: "pi.package-carrier.install",
		},
		{
			name: "Antigravity CLI explicit global",
			build: func(t *testing.T, workDir string) builtFixture {
				return newAntigravityCLIHostRouteFixture(t, hostSourceRouteFixture{
					sourceRef:  "modern-web-guidance@google",
					subjectKey: "modern-web-guidance@google",
					scope:      target.ScopeGlobal,
					workDir:    workDir,
				})
			},
			wantCommand:      "agy",
			wantArgs:         []string{"plugin", "install", "modern-web-guidance@google"},
			wantRoute:        "antigravity-cli.plugin-carrier.refresh",
			wantContract:     "antigravity-cli-plugin-refresh-v1",
			wantInstallRoute: "antigravity-cli.plugin-carrier.install",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workDir := filepath.Join(t.TempDir(), "selected project with spaces")
			fixture := test.build(t, workDir)
			refresh, err := BuildOperationCommand(OperationBuildInput{
				Contract:  fixture.record,
				Operation: lock.OperationRefresh,
				WorkDir:   workDir,
			})
			if err != nil {
				t.Fatalf("BuildOperationCommand returned error: %v", err)
			}
			attempt := refresh.AttemptRequest()
			if attempt.Command != test.wantCommand ||
				!slices.Equal(attempt.Args, test.wantArgs) ||
				attempt.WorkDir != workDir {
				t.Fatalf(
					"refresh attempt = %#v, want %q %#v in %q",
					attempt,
					test.wantCommand,
					test.wantArgs,
					workDir,
				)
			}

			refreshRoute := refresh.RouteRequest()
			if refreshRoute.RouteID() != test.wantRoute ||
				refreshRoute.ContractVersion() != test.wantContract {
				t.Fatalf(
					"refresh route = %q/%q, want %q/%q",
					refreshRoute.RouteID(),
					refreshRoute.ContractVersion(),
					test.wantRoute,
					test.wantContract,
				)
			}
			installRoute, err := lock.DelegatedOperationRequest(
				fixture.record,
				lock.OperationInstall,
			)
			if err != nil {
				t.Fatalf("DelegatedOperationRequest install returned error: %v", err)
			}
			if installRoute.RouteID() != test.wantInstallRoute ||
				installRoute.Equal(refreshRoute) ||
				installRoute.CanonicalRequestHash() == refreshRoute.CanonicalRequestHash() {
				t.Fatalf(
					"install/refresh route identities overlap: %#v / %#v",
					installRoute,
					refreshRoute,
				)
			}
			disclosure, ok := refresh.Disclosure()
			if !ok ||
				len(disclosure.EffectClasses()) == 0 ||
				len(disclosure.RetainedEffectClasses()) == 0 ||
				len(disclosure.NonClaims()) == 0 {
				t.Fatalf("refresh disclosure = %#v, present=%t", disclosure, ok)
			}
		})
	}
}
