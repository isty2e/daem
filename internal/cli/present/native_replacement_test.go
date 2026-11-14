package clipresent

import (
	"bytes"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/output"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

func TestNativeReplacementDisclosureMasksSensitiveValues(t *testing.T) {
	key, err := output.NewKeyPath("api_key")
	if err != nil {
		t.Fatal(err)
	}
	disclosure := applyworkflow.NativeReplacementDisclosure{Fingerprint: "v1:opaque", Declaration: "host_config:preferences", Keys: []applyworkflow.NativeReplacementKey{{Path: key, File: "/file", Action: "replace", CurrentPresent: true, Current: `"private-current-value"`, Desired: `"private-desired-value"`}}}
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
