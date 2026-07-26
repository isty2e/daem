package cli_test

import (
	"bytes"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestExtensionAuthoredAndDirectRowsHaveIdenticalRoutePlans(t *testing.T) {
	tests := []struct {
		name         string
		id           string
		target       string
		carrier      string
		scope        string
		sourceKind   string
		sourceField  string
		sourceRef    string
		relationKey  string
		omitTarget   bool
		omitScope    bool
		wantKind     string
		wantEvidence string
		wantState    string
		wantReason   string
		wantRouteID  string
	}{
		{name: "claude project", id: "claude", target: "claude-code", carrier: "claude-code-plugin", scope: "project", sourceKind: "marketplace", sourceField: "marketplace", sourceRef: "plugin@market", omitTarget: true, omitScope: true, wantKind: "create", wantEvidence: "supported", wantState: "missing", wantRouteID: "claude-code.plugin-carrier.install"},
		{name: "codex global", id: "codex", target: "codex", carrier: "codex-plugin", scope: "global", sourceKind: "marketplace", sourceField: "marketplace", sourceRef: "plugin@market", wantKind: "create", wantEvidence: "supported", wantState: "missing", wantRouteID: "codex.plugin-carrier.install"},
		{name: "opencode project", id: "opencode", target: "opencode", carrier: "opencode-plugin", scope: "project", sourceKind: "host-source", sourceField: "host_source", sourceRef: "@acme/plugin", omitScope: true, wantKind: "create", wantEvidence: "supported", wantState: "missing", wantRouteID: "opencode.plugin-carrier.install"},
		{name: "pi global", id: "pi", target: "pi", carrier: "pi-package", scope: "global", sourceKind: "host-source", sourceField: "host_source", sourceRef: "git:github.com/acme/plugin", wantKind: "create", wantEvidence: "supported", wantState: "missing", wantRouteID: "pi.package-carrier.install"},
		{name: "antigravity global", id: "antigravity", target: "antigravity-cli", carrier: "antigravity-cli-plugin", scope: "global", sourceKind: "host-source", sourceField: "host_source", sourceRef: "plugin@publisher", relationKey: "plugin", wantKind: "create", wantEvidence: "supported", wantState: "missing", wantRouteID: "antigravity-cli.plugin-carrier.install"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
			t.Setenv("CODEX_HOME", t.TempDir())
			t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
			authoredRoot := t.TempDir()
			directRoot := t.TempDir()
			authoredManifest := filepath.Join(authoredRoot, "daem.toml")
			authoredLockfile := filepath.Join(authoredRoot, "daem.lock.toml")
			directManifest := filepath.Join(directRoot, "daem.toml")
			directLockfile := filepath.Join(directRoot, "daem.lock.toml")
			testkit.WriteFile(t, authoredRoot, "daem.toml", "version = 1\ntargets = [\""+test.target+"\"]\n")
			testkit.WriteFile(t, directRoot, "daem.toml", extensionAuthoringRowManifest(test.id, test.target, test.carrier, test.scope, test.sourceField, test.sourceRef))

			addArgs := []string{
				"add", "extension", test.id, test.sourceRef,
				"--manifest", authoredManifest,
			}
			if !test.omitTarget {
				addArgs = append(addArgs, "--target", test.target)
			}
			if !test.omitScope {
				addArgs = append(addArgs, "--scope", test.scope)
			}
			runExtensionAuthoringCLI(t, addArgs...)
			runExtensionAuthoringLock(t, directManifest, directLockfile)

			if !bytes.Equal(testkit.ReadFile(t, authoredLockfile), testkit.ReadFile(t, directLockfile)) {
				t.Fatal("authored and direct lockfiles differ")
			}

			commands := []struct {
				name string
				args func(string, string) []string
			}{
				{
					name: "status",
					args: func(manifest string, lockfile string) []string {
						return []string{"status", "--manifest", manifest, "--json"}
					},
				},
				{
					name: "apply dry-run",
					args: func(manifest string, lockfile string) []string {
						return []string{"apply", "--manifest", manifest, "--dry-run", "--json"}
					},
				},
			}
			for _, command := range commands {
				t.Run(command.name, func(t *testing.T) {
					authoredPayload, authoredOutput := runExtensionRoutePlan(t, command.args(authoredManifest, authoredLockfile))
					directPayload, directOutput := runExtensionRoutePlan(t, command.args(directManifest, directLockfile))
					if !reflect.DeepEqual(authoredPayload.RelationActions, directPayload.RelationActions) {
						t.Fatalf("authored relation actions = %#v, direct = %#v", authoredPayload.RelationActions, directPayload.RelationActions)
					}
					if len(authoredPayload.RelationActions) != 1 {
						t.Fatalf("relation actions = %#v, want one", authoredPayload.RelationActions)
					}
					action := authoredPayload.RelationActions[0]
					wantRelationKey := test.relationKey
					if wantRelationKey == "" {
						wantRelationKey = test.sourceRef
					}
					if action.Kind != test.wantKind || action.Target != test.target || action.Scope != test.scope || action.SourceKind != test.sourceKind ||
						action.SourceRef != test.sourceRef || action.RelationSubjectKey != wantRelationKey ||
						action.EvidenceAvailability != test.wantEvidence || action.CorrelationState != test.wantState ||
						action.Reason != test.wantReason || action.RouteID != test.wantRouteID ||
						!action.InvokesHostRoute || !action.AllowsHostRouteInvocation || action.BlocksOrdinaryApply {
						t.Fatalf("relation action = %#v, want admitted %s/%s %s route", action, test.target, test.scope, test.sourceKind)
					}
					if len(action.NonClaims) == 0 || len(authoredPayload.HostRouteAttempts) != 0 || len(directPayload.HostRouteAttempts) != 0 {
						t.Fatalf("action/output = %#v/%#v/%#v, want non-claims and no dry-run attempt", action, authoredPayload.HostRouteAttempts, directPayload.HostRouteAttempts)
					}
					assertNoCarrierInstallConvergenceClaims(t, authoredOutput)
					assertNoCarrierInstallConvergenceClaims(t, directOutput)
				})
			}
		})
	}
}

func runExtensionRoutePlan(t *testing.T, args []string) (clijson.Plan, string) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("%v exitCode=%d stdout=%q stderr=%q", args, exitCode, stdout.String(), stderr.String())
	}
	return clijson.DecodePlan(t, stdout.Bytes()), stdout.String()
}

func runExtensionAuthoringCLI(t *testing.T, args ...string) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("%v exitCode=%d stdout=%q stderr=%q", args, exitCode, stdout.String(), stderr.String())
	}
}
