package execute

import (
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestRecoveryOrdersProviderRestoreBeforeProjectionSwitchAndPublishedPathDelete(t *testing.T) {
	contract := aggregate.ProjectionContract{}
	actions := []recoveryHostAction{
		{Kind: recovery.ActionKindRestoreWrite, Destination: ".codex/hooks.json", ContentPath: aggregate.HooksContentPath, AggregateContract: &contract},
		{Kind: recovery.ActionKindRestoreDelete, Destination: ".daem/hook-assets/guard/new/asset"},
		{Kind: recovery.ActionKindRestoreWrite, Destination: ".daem/hook-assets/guard/old/asset"},
		{Kind: recovery.ActionKindRestoreDelete, Destination: ".claude/settings.json", ContentPath: aggregate.HooksContentPath, AggregateContract: &contract},
	}

	ordered := orderRecoveryHostActions(actions)
	got := make([]string, 0, len(ordered))
	for _, action := range ordered {
		got = append(got, action.Destination)
	}
	want := []string{
		".daem/hook-assets/guard/old/asset",
		".codex/hooks.json",
		".claude/settings.json",
		".daem/hook-assets/guard/new/asset",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("recovery order = %#v, want %#v", got, want)
	}
}
