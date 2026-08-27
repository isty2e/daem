package recover

import (
	"strings"
	"testing"
)

func TestPlanRejectsNilContextWithoutPanic(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("Plan panicked for nil context: %v", recovered)
		}
	}()

	_, err := Plan(nil, PlanInput{ManifestPath: "daem.toml"})
	if err == nil || !strings.Contains(err.Error(), "recovery context is required") {
		t.Fatalf("Plan error = %v, want context-required rejection", err)
	}
}
