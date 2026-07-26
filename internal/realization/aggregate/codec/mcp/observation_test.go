package mcpcodec

import (
	"bytes"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestMCPPlacementOperationsObserveSelectedCanonicalEntries(t *testing.T) {
	for _, test := range mcpProjectionMutationCases() {
		t.Run(test.name, func(t *testing.T) {
			operations, ok := ImplementedMCPPlacementOperationsForID(test.placement)
			if !ok {
				t.Fatalf("placement operations %q missing", test.placement)
			}
			canonical := mustMutationCanonical(t, test, "context7", "npx")
			existing, err := operations.MergeCanonicalEntry(test.initial, "context7", canonical)
			if err != nil {
				t.Fatalf("seed canonical entry: %v", err)
			}

			observation, err := operations.ObserveCanonicalEntries(existing, []string{"context7", "missing"})
			if err != nil {
				t.Fatalf("ObserveCanonicalEntries: %v", err)
			}
			if !observation.ParentPresent() {
				t.Fatal("managed aggregate parent reported absent")
			}
			observed, present, err := observation.CanonicalEntry("context7")
			if err != nil || !present || !bytes.Equal(observed, canonical) {
				t.Fatalf("context7 observation = %q, present=%t, err=%v", observed, present, err)
			}
			if missing, present, err := observation.CanonicalEntry("missing"); err != nil || present || missing != nil {
				t.Fatalf("missing observation = %q, present=%t, err=%v", missing, present, err)
			}
			if _, _, err := observation.CanonicalEntry("unselected"); err == nil || !strings.Contains(err.Error(), "did not select") {
				t.Fatalf("unselected lookup error = %v", err)
			}

			for index := range observed {
				observed[index] = '!'
			}
			again, present, err := observation.CanonicalEntry("context7")
			if err != nil || !present || !bytes.Equal(again, canonical) {
				t.Fatalf("caller mutation changed observation = %q, present=%t, err=%v", again, present, err)
			}
		})
	}
}

func TestMCPPlacementOperationsRejectInvalidObservationSelection(t *testing.T) {
	operations, ok := ImplementedMCPPlacementOperationsForID(aggregate.MCPPlacementClaudeProject)
	if !ok {
		t.Fatal("Claude project placement operations missing")
	}
	for _, test := range []struct {
		name      string
		serverIDs []string
		want      string
	}{
		{name: "empty", want: "selection is empty"},
		{name: "duplicate", serverIDs: []string{"context7", "context7"}, want: "repeats server id"},
		{name: "invalid", serverIDs: []string{"bad/id"}, want: "stable token"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := operations.ObserveCanonicalEntries(nil, test.serverIDs)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}
