package lockfile

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/encoding/hookdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	commandhook "github.com/isty2e/daem/internal/realization/aggregate/hook"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/target"
	topologyhook "github.com/isty2e/daem/internal/topology/hook"
)

func TestMarshalRejectsMalformedMCPContributionsAcrossPlacements(t *testing.T) {
	tests := []struct {
		placement aggregate.MCPPlacementID
		canonical string
		reason    string
	}{
		{aggregate.MCPPlacementClaudeProject, `{"type":"stdio","command":"npx","env":{"API_TOKEN":"SECRET_CANARY"}}`, "secret_literal_forbidden"},
		{aggregate.MCPPlacementClaudeGlobal, `{"type":"stdio","command":"npx","env":{"API_TOKEN":"SECRET_CANARY"}}`, "secret_literal_forbidden"},
		{aggregate.MCPPlacementAntigravityGlobal, `{"type":"stdio","command":"npx","args":[]}`, "unsupported_managed_field"},
		{aggregate.MCPPlacementOpenCodeProject, `{"type":"local","command":["npx"],"environment":{"TOKEN":"SECRET_CANARY"}}`, "unsupported_managed_field"},
		{aggregate.MCPPlacementOpenCodeGlobal, `{"type":"local","command":["npx"],"environment":{"TOKEN":"SECRET_CANARY"}}`, "secret_literal_forbidden"},
		{aggregate.MCPPlacementCodexProject, "command = \"npx\"\nenv = { TOKEN = \"SECRET_CANARY\" }\n", "unsupported_managed_field"},
		{aggregate.MCPPlacementCodexGlobal, "command = \"npx\"\nenv = { TOKEN = \"SECRET_CANARY\" }\n", "secret_literal_forbidden"},
	}

	for _, test := range tests {
		t.Run(string(test.placement), func(t *testing.T) {
			contract := snapshottest.MCPProjection(t, snapshottest.MCPProjectionInput{
				PlacementID:         test.placement,
				ServerID:            "context7",
				LauncherCommand:     "npx",
				CanonicalProjection: test.canonical,
			})
			file := snapshottest.File(t, contract)

			_, err := Marshal(file)
			if err == nil || !strings.Contains(err.Error(), "aggregate codec "+test.reason) {
				t.Fatalf("Marshal error = %v, want canonical %s rejection", err, test.reason)
			}
			if strings.Contains(err.Error(), "SECRET_CANARY") {
				t.Fatalf("Marshal leaked secret canary: %q", err)
			}
		})
	}
}

func TestMarshalRejectsHookSetBeyondProjectionCardinality(t *testing.T) {
	placement, ok := aggregate.HookPlacementFor(target.TargetClaudeCode, target.ScopeProject)
	if !ok {
		t.Fatal("Claude Code project Hook placement is missing")
	}
	contracts := make([]lock.LockedSubjectContract, 0, hookdocument.MaximumEvents+1)
	for index := range hookdocument.MaximumEvents + 1 {
		name := fmt.Sprintf("event-%03d", index)
		id, err := entity.New(entity.KindHook, name)
		if err != nil {
			t.Fatal(err)
		}
		subject, err := topologyhook.ProjectionSubjectID(
			id,
			target.TargetClaudeCode,
			target.ScopeProject,
		)
		if err != nil {
			t.Fatal(err)
		}
		canonical, err := hookcodec.CanonicalHookContribution(commandhook.ContributionInput{
			Event: fmt.Sprintf("Event%03d", index), Type: "command", Command: "true",
		})
		if err != nil {
			t.Fatal(err)
		}
		contribution, err := placement.Contribution(canonical)
		if err != nil {
			t.Fatal(err)
		}
		contract, err := lock.NewHookContributionSubjectContract(id, subject, contribution, placement)
		if err != nil {
			t.Fatal(err)
		}
		contracts = append(contracts, contract)
	}

	_, err := Marshal(snapshottest.File(t, contracts...))
	if !errors.Is(err, hookdocument.ErrStructuralBudgetExceeded) {
		t.Fatalf("Marshal error = %v, want Hook structural budget error", err)
	}
}
