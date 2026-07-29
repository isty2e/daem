package lock

import (
	"bytes"
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunLockPersistsHostIdentitiesAndDetectsManifestReorder(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(tempDir, "data"))
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeWorkflowTestFile(t, tempDir, "daem.toml", orderedExtensionManifest(false))

	written, err := RunLock(context.Background(), LockInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	constraints := written.Lockfile.Locked.OrderConstraints()
	if len(constraints) != 2 {
		t.Fatalf("order constraints = %#v, want OpenCode and Pi", constraints)
	}
	if got := string(constraints[0].ClassID()); got != "extension:opencode:project:plugins" {
		t.Fatalf("constraint[0].ClassID = %q", got)
	}
	if got := string(constraints[1].ClassID()); got != "extension:pi:project:packages" {
		t.Fatalf("constraint[1].ClassID = %q", got)
	}
	openCodeMembers := constraints[0].Members()
	if got := string(openCodeMembers[0].HostLoadIdentity()); got != "@acme/open-second" {
		t.Fatalf("OpenCode member[0] identity = %q", got)
	}
	if got := string(openCodeMembers[1].HostLoadIdentity()); got != "@acme/open-first" {
		t.Fatalf("OpenCode member[1] identity = %q", got)
	}
	piMembers := constraints[1].Members()
	if got := string(piMembers[0].HostLoadIdentity()); got != "npm:@acme/pi-second" {
		t.Fatalf("Pi member[0] identity = %q", got)
	}
	if got := string(piMembers[1].HostLoadIdentity()); got != "npm:@acme/pi-first" {
		t.Fatalf("Pi member[1] identity = %q", got)
	}

	firstContent, err := os.ReadFile(filepath.Join(tempDir, "daem.lock.toml"))
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := RunLock(context.Background(), LockInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("repeated RunLock returned error: %v", err)
	}
	secondContent, err := os.ReadFile(filepath.Join(tempDir, "daem.lock.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstContent, secondContent) {
		t.Fatalf(
			"repeated lock changed canonical bytes:\nfirst:\n%s\nsecond:\n%s",
			firstContent,
			secondContent,
		)
	}
	if repeated.Delta.HasChanges() {
		t.Fatalf(
			"repeated lock delta = subjects %#v, order %#v",
			repeated.Delta.Counts(),
			repeated.Delta.OrderCounts(),
		)
	}

	writeWorkflowTestFile(t, tempDir, "daem.toml", orderedExtensionManifest(true))
	preview, err := RunLock(context.Background(), LockInput{
		ManifestPath: manifestPath,
		DryRun:       true,
	})
	if err != nil {
		t.Fatalf("reordered dry-run returned error: %v", err)
	}
	if got := preview.Delta.Counts(); got.Unchanged != 4 ||
		got.Added != 0 ||
		got.Changed != 0 ||
		got.Removed != 0 {
		t.Fatalf("reordered subject counts = %#v, want four unchanged", got)
	}
	if got := preview.Delta.OrderCounts(); got.Changed != 1 ||
		got.Unchanged != 1 ||
		got.Added != 0 ||
		got.Removed != 0 {
		t.Fatalf("reordered order counts = %#v, want one changed and one unchanged", got)
	}
}

func TestRunLockRepairsOpenCodeGlobalIdentityAfterConfigRootChanges(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(tempDir, "data"))
	firstConfigRoot := filepath.Join(tempDir, "config-first")
	secondConfigRoot := filepath.Join(tempDir, "config-second")
	t.Setenv("XDG_CONFIG_HOME", firstConfigRoot)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	writeWorkflowTestFile(t, tempDir, "daem.toml", `version = 1
targets = ["opencode"]

[[extension]]
id = "first"
carrier = "opencode-plugin"
targets = ["opencode"]
scope = "global"
source = { host_source = "./plugins/first.mjs" }

[[extension]]
id = "second"
carrier = "opencode-plugin"
targets = ["opencode"]
scope = "global"
source = { host_source = "./plugins/second.mjs" }
`)

	if _, err := RunLock(context.Background(), LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("initial RunLock returned error: %v", err)
	}
	firstIdentity := (&url.URL{
		Scheme: "file",
		Path: filepath.ToSlash(filepath.Join(
			firstConfigRoot,
			"opencode",
			"plugins",
			"first.mjs",
		)),
	}).String()
	firstContent, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(firstContent), firstIdentity) {
		t.Fatalf("initial lockfile lacks first config-root identity %q", firstIdentity)
	}

	t.Setenv("XDG_CONFIG_HOME", secondConfigRoot)
	preview, err := RunOutdated(context.Background(), OutdatedInput{
		ManifestPath: manifestPath,
	})
	if err != nil {
		t.Fatalf("RunOutdated rejected repairable config-root drift: %v", err)
	}
	if got := preview.Delta.OrderCounts(); got.Changed != 1 {
		t.Fatalf("order counts = %#v, want one changed constraint", got)
	}
	if _, err := RunLock(context.Background(), LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("RunLock rejected repairable config-root drift: %v", err)
	}
	secondIdentity := (&url.URL{
		Scheme: "file",
		Path: filepath.ToSlash(filepath.Join(
			secondConfigRoot,
			"opencode",
			"plugins",
			"first.mjs",
		)),
	}).String()
	secondContent, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(secondContent), firstIdentity) ||
		!strings.Contains(string(secondContent), secondIdentity) {
		t.Fatalf(
			"relocked identities did not follow selected config root:\n%s",
			secondContent,
		)
	}
}

func TestRunLockRegeneratesContextuallyInvalidPriorOrderIdentity(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(tempDir, "data"))
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	writeWorkflowTestFile(t, tempDir, "daem.toml", orderedExtensionManifest(false))
	if _, err := RunLock(context.Background(), LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("initial RunLock returned error: %v", err)
	}

	content, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(
		string(content),
		`host_load_identity = "@acme/open-second"`,
		`host_load_identity = "@acme/forged"`,
		1,
	)
	if tampered == string(content) {
		t.Fatal("lockfile identity fixture was not found")
	}
	if err := os.WriteFile(lockfilePath, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}

	preview, err := RunOutdated(context.Background(), OutdatedInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("RunOutdated rejected regenerable prior identity: %v", err)
	}
	if got := preview.Delta.OrderCounts(); got.Changed != 1 {
		t.Fatalf("order counts = %#v, want one changed constraint", got)
	}
	if _, err := RunLock(context.Background(), LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("RunLock rejected regenerable prior identity: %v", err)
	}
	regenerated, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(regenerated), "@acme/forged") {
		t.Fatalf("regenerated lockfile retained forged identity:\n%s", regenerated)
	}
}

func orderedExtensionManifest(reversePi bool) string {
	piRows := `
[[extension]]
id = "pi-second"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "npm:@acme/pi-second@2.0.0" }

[[extension]]
id = "pi-first"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "npm:@acme/pi-first@1.0.0" }
`
	if reversePi {
		piRows = `
[[extension]]
id = "pi-first"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "npm:@acme/pi-first@1.0.0" }

[[extension]]
id = "pi-second"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "npm:@acme/pi-second@2.0.0" }
`
	}
	return `version = 1
targets = ["opencode", "pi"]

[[extension]]
id = "open-second"
carrier = "opencode-plugin"
targets = ["opencode"]
scope = "project"
source = { host_source = "@acme/open-second@2.0.0" }

[[extension]]
id = "open-first"
carrier = "opencode-plugin"
targets = ["opencode"]
scope = "project"
source = { host_source = "@acme/open-first@1.0.0" }
` + piRows
}
