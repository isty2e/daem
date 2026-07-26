package build

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/hookasset"
	"github.com/isty2e/daem/internal/desired/instructions"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

func TestBuildWithOptionsSequentialHookAssetFailureIsNotMaskedByUnreachedInstructions(t *testing.T) {
	hookSource := sourcetest.Local(t, "hooks/guard.sh", source.LocalSourceModeVendor)
	input := desired.Spec{
		HookAssets: []hookasset.HookAsset{
			desiredtest.HookAsset(t, hookasset.Spec{
				Name: "guard", Source: hookSource, ArtifactKind: hookasset.ArtifactKindFile,
				Scope: target.ScopeProject,
			}),
		},
		Instructions: []instructions.Instructions{
			projectInstructions(
				t,
				"project",
				sourcetest.Local(t, "instructions/AGENTS.md", source.LocalSourceModeVendor),
				[]target.Target{target.TargetCodex},
			),
		},
	}
	resolver := failingSequentialResolver{errBySourceID: map[string]error{
		"local:hooks/guard.sh?mode=vendor": errors.New("guard failed"),
	}}
	lockEvents := newLockEventRecorder()

	_, err := buildWithTestOptions(context.Background(), lockEnvironment(t, input), resolver, Options{Events: lockEvents.sink})
	if err == nil || !strings.Contains(err.Error(), `resolve hook_asset "guard" source: guard failed`) {
		t.Fatalf("BuildWithOptions error = %q, want original hook asset source failure", err)
	}
	if strings.Contains(err.Error(), "lock assembly got") {
		t.Fatalf("BuildWithOptions error = %q, want no downstream length mismatch", err)
	}

	events := lockEvents.snapshot()
	if got := lockEventKinds(events); !reflect.DeepEqual(got, []EventKind{EventResourceResolveStarted, EventResourceResolveFailed}) {
		t.Fatalf("events = %#v, want only hook asset start/fail", events)
	}
	for _, event := range events {
		if event.EntityID != mustEntityID(t, entity.KindHookAsset, "guard") {
			t.Fatalf("event = %#v, want no event for unreached instructions", event)
		}
	}
}
