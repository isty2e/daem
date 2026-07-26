package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunUnmanageExtensionDryRunDiffIsHostPreservingAndNonMutating(t *testing.T) {
	root, manifestPath, manifestBefore, hostPath, hostBefore := writeCLIUnmanageFixture(t, "project")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithoutTerminal(
		[]string{
			"unmanage", "extension", "context7",
			"--manifest", manifestPath,
			"--dry-run",
			"--diff",
		},
		&stdout,
		&stderr,
	)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	for _, want := range []string{
		"unmanage: extension/context7",
		"manifest: would remove",
		"management: not present",
		"host: retained",
		"manifest diff:",
		"-[[extension]]",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	assertCLIUnmanageFile(t, manifestPath, manifestBefore)
	assertCLIUnmanageFile(t, hostPath, hostBefore)
	if _, err := os.Stat(filepath.Join(root, "project", "daem.lock.toml")); !os.IsNotExist(err) {
		t.Fatalf("dry-run lockfile stat error = %v, want absent", err)
	}
}

func TestRunUnmanageExtensionJSONUsesAuthoringEnvelopeAndGlobalCaveat(t *testing.T) {
	_, manifestPath, manifestBefore, _, _ := writeCLIUnmanageFixture(t, "global")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithoutTerminal(
		[]string{
			"unmanage", "extension", "context7",
			"--manifest", manifestPath,
			"--target", "claude-code",
			"--scope", "global",
			"--dry-run",
			"--json",
		},
		&stdout,
		&stderr,
	)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	var payload struct {
		SchemaVersion int `json:"schema_version"`
		Changes       []struct {
			ResourceID string `json:"resource_id"`
			ChangeKind string `json:"change_kind"`
			Status     string `json:"status"`
			Target     string `json:"target"`
			Scope      string `json:"scope"`
		} `json:"changes"`
		Management struct {
			Status string `json:"status"`
		} `json:"management"`
		Host struct {
			State            string `json:"state"`
			AmbientConsumers string `json:"ambient_consumers"`
		} `json:"host"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal: %v; output = %q", err, stdout.String())
	}
	if payload.SchemaVersion != 2 ||
		len(payload.Changes) != 1 ||
		payload.Changes[0].ResourceID != "extension/context7" ||
		payload.Changes[0].ChangeKind != "would_remove" ||
		payload.Changes[0].Status != "not_present" ||
		payload.Changes[0].Target != "claude-code" ||
		payload.Changes[0].Scope != "global" ||
		payload.Management.Status != "not_present" ||
		payload.Host.State != "retained" ||
		payload.Host.AmbientConsumers != "unobservable" {
		t.Fatalf("payload = %#v, want exact global unmanage disclosure", payload)
	}
	assertCLIUnmanageFile(t, manifestPath, manifestBefore)
}

func TestRunUnmanageExtensionWriteRemovesMetadataAndRetainsHostInventory(t *testing.T) {
	root, manifestPath, _, hostPath, hostBefore := writeCLIUnmanageFixture(t, "project")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithoutTerminal(
		[]string{"unmanage", "extension", "context7", "--manifest", manifestPath},
		&stdout,
		&stderr,
	)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if strings.Contains(string(readCLIUnmanageFile(t, manifestPath)), "[[extension]]") {
		t.Fatal("write retained extension declaration")
	}
	if _, err := os.Stat(filepath.Join(root, "project", "daem.lock.toml")); err != nil {
		t.Fatalf("lockfile stat error = %v, want present", err)
	}
	assertCLIUnmanageFile(t, hostPath, hostBefore)
	for _, want := range []string{
		"manifest: removed",
		"management: not present",
		"host: retained",
		"daem management ended",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}

	manifestAfter := readCLIUnmanageFile(t, manifestPath)
	lockPath := filepath.Join(root, "project", "daem.lock.toml")
	lockAfter := readCLIUnmanageFile(t, lockPath)
	stdout.Reset()
	stderr.Reset()
	exitCode = runWithoutTerminal(
		[]string{"unmanage", "extension", "context7", "--manifest", manifestPath},
		&stdout,
		&stderr,
	)
	if exitCode != 1 || !strings.Contains(stderr.String(), "extension management not found") {
		t.Fatalf("second exitCode = %d, stderr = %q, want already-absent failure", exitCode, stderr.String())
	}
	assertCLIUnmanageFile(t, manifestPath, manifestAfter)
	assertCLIUnmanageFile(t, lockPath, lockAfter)
	assertCLIUnmanageFile(t, hostPath, hostBefore)
}

func TestRunUnmanageExtensionSelectorsAreSafetyFilters(t *testing.T) {
	_, manifestPath, manifestBefore, _, _ := writeCLIUnmanageFixture(t, "project")
	tests := []struct {
		name     string
		args     []string
		exitCode int
		want     string
	}{
		{
			name:     "mismatched target does not redirect",
			args:     []string{"--target", "codex", "--dry-run"},
			exitCode: 1,
			want:     "extension management not found",
		},
		{
			name:     "mismatched scope does not redirect",
			args:     []string{"--scope", "global", "--dry-run"},
			exitCode: 1,
			want:     "extension management not found",
		},
		{
			name:     "two targets are not a bulk selector",
			args:     []string{"--target", "claude-code", "--target", "codex", "--dry-run"},
			exitCode: 2,
			want:     "--target accepts at most one distinct target",
		},
		{
			name:     "host authorization flag is absent",
			args:     []string{"--yes"},
			exitCode: 2,
			want:     "flag provided but not defined: -yes",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			args := append(
				[]string{"unmanage", "extension", "context7", "--manifest", manifestPath},
				test.args...,
			)
			if got := runWithoutTerminal(args, &stdout, &stderr); got != test.exitCode {
				t.Fatalf("exitCode = %d, want %d; stderr = %q", got, test.exitCode, stderr.String())
			}
			if !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), test.want)
			}
			assertCLIUnmanageFile(t, manifestPath, manifestBefore)
		})
	}
}

func TestRunUnmanageHelpStatesHostPreservationContract(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithoutTerminal(
		[]string{"help", "unmanage", "extension"},
		&stdout,
		&stderr,
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	for _, want := range []string{
		"daem unmanage extension",
		"never invokes a host route",
		"always retains host state",
		"manual consumers remain unobservable",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func writeCLIUnmanageFixture(
	t *testing.T,
	scope string,
) (root string, manifestPath string, manifest []byte, hostPath string, host []byte) {
	t.Helper()
	root = t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))

	manifestPath = filepath.Join(root, "project", "daem.toml")
	manifest = []byte(`version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
targets = ["claude-code"]
scope = "` + scope + `"
source = { marketplace = "context7@official" }
`)
	writeCLIUnmanageFile(t, manifestPath, manifest)
	hostPath = filepath.Join(root, "home", ".claude", "plugins", "installed_plugins.json")
	host = []byte(`{"context7":"retained"}` + "\n")
	writeCLIUnmanageFile(t, hostPath, host)
	return root, manifestPath, manifest, hostPath, host
}

func writeCLIUnmanageFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readCLIUnmanageFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func assertCLIUnmanageFile(t *testing.T, path string, want []byte) {
	t.Helper()
	if got := readCLIUnmanageFile(t, path); !bytes.Equal(got, want) {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}
