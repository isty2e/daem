package cli_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestNativeKeyGroupBlockedPlan(t *testing.T) {
	fixture := nativePresentationFixture(t, "a=true\nb=true\nc=true\n", "a=false\nb=false\nc=false\n")
	before := testkit.ReadFile(t, fixture.destination)
	for _, args := range [][]string{{"status", "--check"}, {"apply", "--dry-run"}, {"apply", "--yes"}} {
		args = append(args, "--manifest", fixture.manifest)
		code, stdout, stderr := runOwnershipCLI(args...)
		text := stdout + stderr
		if code != 1 || !bytes.Equal(before, testkit.ReadFile(t, fixture.destination)) {
			t.Fatalf("blocked group changed effects: %d %s", code, text)
		}
		if strings.Count(text, `resource="host_config/preferences"`) != 1 || strings.Count(text, `destination=".codex/config.toml"`) != 1 {
			t.Fatalf("native declaration context was not grouped: %s", text)
		}
		if !strings.Contains(text, "3 output entries (current=0 pending=0 blocked=3)") || strings.Count(text, "document: blocked") != 1 {
			t.Fatalf("atomic counts or document refusal were lost: %s", text)
		}
		for _, key := range []string{"a", "b", "c"} {
			if !strings.Contains(text, "key: "+key+": blocked (unmanaged output exists)") {
				t.Fatalf("blocked key %s lost its own reason: %s", key, text)
			}
		}
		if strings.Contains(text, `blocked by "host_config/preferences";`) {
			t.Fatalf("a declaration-only cause replaced key identity: %s", text)
		}
	}
}

func TestNativeKeyGroupRetainsCurrentSiblings(t *testing.T) {
	fixture := nativePresentationFixture(t, "a=true\nb=true\nc=true\n", "a=false\nb=false\nc=false\n")
	fingerprint := nativeReplacementFingerprint(t, fixture)
	runNativeRecoveryCommand(t, fixture, "none", "replace", "host-config", "preferences", "--yes", "--expect-plan", fingerprint)
	testkit.WriteFile(t, filepath.Dir(fixture.destination), filepath.Base(fixture.destination), "a=false\nb=true\nc=true\n")
	code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture, "none", "status", "--check")
	text := stdout + stderr
	if code != 1 || !strings.Contains(text, "current=2 pending=0 blocked=1") || strings.Count(text, `resource="host_config/preferences"`) != 1 {
		t.Fatalf("grouping changed status counts or context: %d %s", code, text)
	}
	for _, row := range []string{"key: a: blocked (drifted output)", "key: b: up to date", "key: c: up to date", "document: blocked by key a"} {
		if !strings.Contains(text, row) {
			t.Fatalf("key or document distinction %q was lost: %s", row, text)
		}
	}
}

func TestNativeKeyGroupAppliedResult(t *testing.T) {
	fixture := nativePresentationFixture(t, "a=true\nb=true\nc=true\n", "")
	code, stdout, stderr := runOwnershipCLI("apply", "--manifest", fixture.manifest, "--yes")
	if code != 0 {
		t.Fatalf("native Apply failed: %d %s %s", code, stdout, stderr)
	}
	_, result, present := strings.Cut(stdout, "output results: 3 output entries")
	if !present || strings.Count(result, `resource="host_config/preferences"`) != 1 || strings.Count(result, `destination=".codex/config.toml"`) != 1 {
		t.Fatalf("confirmed result did not group atomic output entries: %s", stdout)
	}
	for _, key := range []string{"a", "b", "c"} {
		if !strings.Contains(result, "key: "+key+": add managed output") {
			t.Fatalf("applied key %s was omitted: %s", key, result)
		}
	}
	code, stdout, stderr = runOwnershipCLI("status", "--manifest", fixture.manifest, "--check")
	if code != 0 || strings.Contains(stdout, `resource="host_config/preferences"`) || !strings.Contains(stdout, "current=3 pending=0 blocked=0") {
		t.Fatalf("healthy current groups must retain normal omission: %d %s %s", code, stdout, stderr)
	}
}

func TestNativeKeyGroupRetainsLiteralKeysAndJSONCounts(t *testing.T) {
	fixture := nativePresentationFixture(t, "\"a.b\"=true\n\"\"=true\n[ext]\n\"a/b\"=true\n", "")
	code, text, stderr := runOwnershipCLI("apply", "--manifest", fixture.manifest, "--dry-run")
	if code != 0 || strings.Count(text, `resource="host_config/preferences"`) != 1 {
		t.Fatalf("literal native keys were not grouped: %d %s %s", code, text, stderr)
	}
	for _, key := range []string{`"a.b"`, `""`, `ext."a/b"`} {
		if !strings.Contains(text, "key: "+key+": add managed output") {
			t.Fatalf("literal key identity %s was changed: %s", key, text)
		}
	}
	jsonText := requireNativeJSONCommand(t, "apply", "--manifest", fixture.manifest, "--dry-run", "--json")
	plan := clijson.DecodePlan(t, []byte(jsonText))
	if len(plan.Actions) != 3 {
		t.Fatalf("grouping changed machine action granularity: %s", jsonText)
	}
	code, verbose, stderr := runOwnershipCLI("apply", "--manifest", fixture.manifest, "--dry-run", "--verbose")
	if code != 0 || strings.Count(verbose, "\n  resource=") != 3 || strings.Count(verbose, "content_path=") != 3 {
		t.Fatalf("verbose key-level detail was compacted: %d %s %s", code, verbose, stderr)
	}
}

func TestNativeKeyGroupSharedDocumentRetainsOtherOwners(t *testing.T) {
	fixture := nativePresentationFixture(t, "a=true\nb=true\n", "a=false\nb=false\nc=false\n[mcp_servers.search]\ncommand='other'\n")
	workspace := filepath.Dir(fixture.manifest)
	manifest := append(testkit.ReadFile(t, fixture.manifest), []byte(`
[[host_config]]
id = "other"
path = "config.toml"
source = "./other.toml"
ownership = "keys"
[[mcp_server]]
name = "search"
transport = "stdio"
command = "node"
`)...)
	testkit.WriteFile(t, workspace, "daem.toml", string(manifest))
	testkit.WriteFile(t, workspace, "other.toml", "c=true\n")
	runNativeLifecycleCommand(t, "lock", "--manifest", fixture.manifest)
	before := testkit.ReadFile(t, fixture.destination)
	code, stdout, stderr := runOwnershipCLI("apply", "--manifest", fixture.manifest, "--dry-run")
	text := stdout + stderr
	if code != 1 || !bytes.Equal(before, testkit.ReadFile(t, fixture.destination)) {
		t.Fatalf("shared-owner preview changed effects: %d %s", code, text)
	}
	for _, reference := range []string{`resource="host_config/preferences"`, `resource="host_config/other"`, `resource="mcp-server/search"`} {
		if strings.Count(text, reference) != 1 {
			t.Fatalf("owner context %s was lost or duplicated: %s", reference, text)
		}
	}
	for _, cause := range []string{`"host_config/other" key c`, `"mcp-server/search"`, `"host_config/preferences" key a`, `"host_config/preferences" key b`} {
		if !strings.Contains(text, cause) {
			t.Fatalf("cross-owner blocker %s was hidden: %s", cause, text)
		}
	}
}

func TestNativeKeyGroupRemovalResult(t *testing.T) {
	fixture := nativePresentationFixture(t, "a=true\nb=true\n", "foreign='untouched'\n")
	runNativeLifecycleCommand(t, "apply", "--manifest", fixture.manifest, "--yes")
	testkit.WriteFile(t, filepath.Dir(fixture.manifest), filepath.Base(fixture.manifest), "version=1\ntargets=['codex']\n")
	runNativeLifecycleCommand(t, "lock", "--manifest", fixture.manifest)

	for _, args := range [][]string{{"apply", "--dry-run"}, {"apply", "--yes"}} {
		args = append(args, "--manifest", fixture.manifest)
		code, stdout, stderr := runOwnershipCLI(args...)
		if code != 0 || strings.Count(stdout, `resource="host_config/preferences"`) != 1 || !strings.Contains(stdout, "2 output entries") {
			t.Fatalf("removal lost declaration grouping or counts: %d %s %s", code, stdout, stderr)
		}
		for _, key := range []string{"a", "b"} {
			if !strings.Contains(stdout, "key: "+key+": remove managed output") {
				t.Fatalf("removed key %s was lost: %s", key, stdout)
			}
		}
	}
	if actual := string(testkit.ReadFile(t, fixture.destination)); actual != "foreign='untouched'\n" {
		t.Fatalf("removal changed unrelated host content: %s", actual)
	}
}
