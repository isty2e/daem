package clipresent

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/output"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

func TestNativeReplacementResultDistinguishesEffects(t *testing.T) {
	key, err := output.NewKeyPath("model")
	if err != nil {
		t.Fatal(err)
	}
	for action, outcome := range map[string]string{
		"create":   "Applied declared values to 1 host configuration key: model",
		"recreate": "Applied declared values to 1 host configuration key: model",
		"takeover": "Applied declared values to 1 host configuration key: model",
		"replace":  "Applied declared values to 1 host configuration key: model",
		"adopt":    "Adopted 1 host configuration key without changing its value: model",
		"record":   "Updated management records for 1 host configuration key without changing host values: model",
		"current":  "No changes were needed",
	} {
		t.Run(action, func(t *testing.T) {
			text, err := NativeReplacementResultText(applyworkflow.NativeReplacementDisclosure{
				Keys: []applyworkflow.NativeReplacementKey{{Path: key, Action: action, Current: "private-before", Desired: "private-after"}},
			})
			if err != nil || !strings.Contains(string(text), outcome) || strings.Contains(string(text), "private-") {
				t.Fatalf("result did not describe its effect without values: %s %v", text, err)
			}
		})
	}
	if _, err := NativeReplacementResultText(applyworkflow.NativeReplacementDisclosure{
		Keys: []applyworkflow.NativeReplacementKey{{Path: key, Action: "unknown"}},
	}); err == nil {
		t.Fatal("an unrenderable result must be refused during output preparation")
	}
}
