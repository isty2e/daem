package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestInstalledHelpUsesCanonicalDocumentNamesInsteadOfCheckoutPaths(t *testing.T) {
	for _, test := range []struct {
		topic         []string
		wantReference string
	}{
		{topic: []string{"status"}, wantReference: "Reference: CLI Reference"},
		{topic: []string{"list", "outputs"}, wantReference: "Reference: CLI Reference"},
		{topic: []string{"add"}, wantReference: "Reference: Manifest Reference"},
		{topic: []string{"add", "skill"}, wantReference: "Reference: Manifest Reference"},
	} {
		t.Run(strings.Join(test.topic, " "), func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunCLI(append([]string{"help"}, test.topic...), &stdout, &stderr)
			if exitCode != 0 || stderr.Len() != 0 {
				t.Fatalf("exitCode = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
			}
			if !strings.Contains(stdout.String(), test.wantReference) {
				t.Fatalf("stdout = %q, want %q", stdout.String(), test.wantReference)
			}
			if strings.Contains(stdout.String(), "docs/") || strings.Contains(stdout.String(), ".md") {
				t.Fatalf("stdout = %q, contains checkout-relative documentation path", stdout.String())
			}
		})
	}
}
