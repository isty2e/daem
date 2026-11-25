package clipresent

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

func TestNativeReplacementDisclosureMasksSensitiveValues(t *testing.T) {
	key, err := output.NewKeyPath("api_key")
	if err != nil {
		t.Fatal(err)
	}
	id, err := entity.New(entity.KindAgentConfig, "preferences")
	if err != nil {
		t.Fatal(err)
	}
	disclosure := applyworkflow.NativeReplacementDisclosure{Fingerprint: "v1:opaque", Declaration: id, Keys: []applyworkflow.NativeReplacementKey{{Path: key, File: "/file", Action: "replace", CurrentPresent: true, Current: `"private-current-value"`, Desired: `"private-desired-value"`}}}
	encoded, err := NativeReplacementJSON(disclosure, "dry-run", 1)
	if err != nil {
		t.Fatal(err)
	}
	var human bytes.Buffer
	if err := PrintNativeReplacement(&human, disclosure); err != nil {
		t.Fatal(err)
	}
	for _, result := range []string{string(encoded), human.String()} {
		if strings.Contains(result, "private-current-value") || strings.Contains(result, "private-desired-value") || !strings.Contains(result, "REDACTED") {
			t.Fatal("replacement disclosed sensitive selected values", result)
		}
	}
}

func TestReplacementJSONDistinguishesUnencodableFileFromReplacementCharacter(t *testing.T) {
	row := applyworkflow.NativeReplacementKey{File: "/dir/\xff"}
	key, err := output.NewKeyPath("quoted\nkey")
	if err != nil {
		t.Fatal(err)
	}
	row.Path = key
	disclosure := applyworkflow.NativeReplacementDisclosure{Keys: []applyworkflow.NativeReplacementKey{row}}
	if data, err := NativeReplacementJSON(disclosure, "dry-run", 0); err == nil || data != nil || !strings.Contains(err.Error(), "keys[0].file") || strings.Contains(err.Error(), "/dir/") {
		t.Fatalf("invalid-byte route was lossy or disclosed: %q %v", data, err)
	}
	var human bytes.Buffer
	if err := PrintNativeReplacement(&human, disclosure); err != nil || !strings.Contains(human.String(), `/dir/\xff`) {
		t.Fatalf("human preview lost the invalid byte: %q %v", human.String(), err)
	}
	disclosure.Keys[0].File = "/dir/\uFFFD[REDACTED]"
	data, err := NativeReplacementJSON(disclosure, "dry-run", 0)
	var decoded struct{ Keys []nativeReplacementKeyJSON }
	if err != nil || json.Unmarshal(data, &decoded) != nil || len(decoded.Keys) != 1 || decoded.Keys[0].File != disclosure.Keys[0].File || len(decoded.Keys[0].KeyPath) != 1 || decoded.Keys[0].KeyPath[0] != "quoted\nkey" {
		t.Fatalf("valid identifier changed: %s %v", data, err)
	}

	disclosure.Keys[0].Path, err = output.NewKeyPath("https://u:synthetic-private-label@example.test/")
	if err != nil {
		t.Fatal(err)
	}
	if data, err := NativeReplacementJSON(disclosure, "executed", 1); err == nil || data != nil || !strings.Contains(err.Error(), "keys[0].key_path") || strings.Contains(err.Error(), "synthetic-private-label") {
		t.Fatalf("protected key disclosed: %q %v", data, err)
	}
}
