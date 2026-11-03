package cli_test

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativeKeyReplacementInteractiveConsent(t *testing.T) {
	for _, approve := range []bool{true, false} {
		name := "decline"
		if approve {
			name = "approve"
		}
		t.Run(name, func(t *testing.T) {
			fixture := nativeReplacementRecoveryFixture(t, "managed")
			before := nativefixture.SnapshotRecoveryEvidence(t, fixture)
			var stdout, stderr bytes.Buffer
			prompts := 0
			code := clipkg.RunWithOptions([]string{"replace", "host-config", "preferences", "--manifest", fixture.Manifest}, clipkg.RunOptions{
				Context: t.Context(), Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr,
				StdinIsTerminal: true, StdoutIsTerminal: true, StderrIsTerminal: true,
				ReadConfirmationLine: func(context.Context, io.Reader, int) (string, error) {
					prompts++
					if approve {
						return "yes", nil
					}
					return "no", nil
				},
			})
			if code != 0 || prompts != 1 {
				t.Fatalf("replacement consent: exit=%d prompts=%d stdout=%s stderr=%s", code, prompts, stdout.String(), stderr.String())
			}
			if approve {
				nativefixture.RequireSharedValues(t, configdocument.TOML, testkit.ReadFile(t, fixture.Destination), "model='after-model'\n", true)
			} else if !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryEvidence(t, fixture)) {
				t.Fatal("declined replacement changed evidence")
			}
		})
	}
}
