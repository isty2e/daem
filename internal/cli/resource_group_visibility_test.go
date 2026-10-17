package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBasicInventoryShowsKnownGroupsWithoutResolvingSources(t *testing.T) {
	root, cwd := manifestHintWorkspace(t)
	path := filepath.Join(cwd, "daem.toml")
	const manifest = `version=1
targets=["codex","claude-code"]
[[skill]]
id="standalone"
name="installed-name"
source={path="absent/direct",mode="vendor"}
[[skill_group]]
names=["alpha","beta"]
source={path="absent/group",mode="vendor"}
`
	if err := os.WriteFile(path, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	before := snapshotApplyInputFiles(t, root)
	var stdout, stderr bytes.Buffer
	if code := runWithoutTerminal([]string{"list", "resources", "--manifest", path}, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("list code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
	}
	grouped := 0
	for line := range strings.SplitSeq(stdout.String(), "\n") {
		if strings.Contains(line, `key="alpha"`) || strings.Contains(line, `key="beta"`) {
			if !strings.Contains(line, `kind=skill`) || !strings.Contains(line, `group="skill_group[0]"`) {
				t.Fatalf("group member lost its authoring identity or membership: %q", line)
			}
			grouped++
		}
		if strings.Contains(line, `key="standalone"`) && strings.Contains(line, "group=") {
			t.Fatalf("ungrouped row gained a group field: %q", line)
		}
	}
	if grouped != 2 || strings.Contains(stdout.String(), "source=") || strings.Contains(stdout.String(), "absent/") {
		t.Fatalf("basic inventory lost groups or expanded source disclosure: %q", &stdout)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWithoutTerminal([]string{"list", "resources", "--manifest", path, "--json"}, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("list JSON code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
	}
	var inventory struct {
		SchemaVersion int `json:"schema_version"`
		Resources     []struct {
			Kind  string
			Key   string
			Group string
		} `json:"resources"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &inventory); err != nil {
		t.Fatal(err)
	}
	if inventory.SchemaVersion != 3 || len(inventory.Resources) != 3 {
		t.Fatalf("group display changed the JSON envelope: %+v", inventory)
	}
	for _, row := range inventory.Resources {
		wantGroup := "skill_group[0]"
		if row.Key == "standalone" {
			wantGroup = "-"
		}
		if row.Kind != "skill" || row.Group != wantGroup {
			t.Fatalf("JSON group semantics changed: %+v", row)
		}
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWithoutTerminal([]string{"remove", "skill", "alpha", "--manifest", path, "--target", "codex", "--dry-run", "--json"}, &stdout, &stderr); code != 1 {
		t.Fatalf("partial group removal code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
	}
	diagnostic := refusalDiagnosticText(t, "remove skill", &stdout, &stderr, true)
	if !strings.Contains(diagnostic, "split the group before partial target removal") {
		t.Fatalf("listed shared membership did not retain its removal boundary: %q", diagnostic)
	}
	if !reflect.DeepEqual(before, snapshotApplyInputFiles(t, root)) {
		t.Fatal("list or refused group removal mutated fixture files")
	}
}
