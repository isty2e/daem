package recovery_test

import (
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
)

func TestFileRestorationEffectsSeparateValuesExistenceAndMode(t *testing.T) {
	private, public := recovery.PermissionMode(0o600), recovery.PermissionMode(0o644)
	old := aggregate.ExistingDocument([]byte("old\n"))
	updated := aggregate.ExistingDocument([]byte("new\n"))
	for _, test := range []struct {
		name              string
		current, next     aggregate.Document
		mode, nextMode    *recovery.PermissionMode
		value, root, perm bool
	}{
		{"no-op", old, old, &private, &private, false, false, false},
		{"value", updated, old, &private, &private, true, false, false},
		{"mode", old, old, &public, &private, false, false, true},
		{"mixed", updated, old, &public, &private, true, false, true},
		{"create", aggregate.AbsentDocument(), old, nil, &private, true, true, false},
		{"delete", old, aggregate.AbsentDocument(), &private, nil, true, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			effects, err := recovery.NewFileRestorationEffects(test.current, test.mode, test.next, test.nextMode)
			if err != nil || effects.Validate() != nil {
				t.Fatal(err)
			}
			var values []output.Footprint
			if test.value {
				values = []output.Footprint{output.WholePathFootprint()}
			}
			if !slices.Equal(effects.ValueFootprints(), values) || effects.ChangesExistence() != test.root || effects.ChangesMode() != test.perm || effects.Changed() != (test.value || test.root || test.perm) {
				t.Fatalf("wrong actual file envelope: %+v", effects)
			}
		})
	}
	if _, err := recovery.NewFileRestorationEffects(old, nil, old, &private); err == nil {
		t.Fatal("existing file without exact mode was admitted")
	}
	if err := (recovery.RestorationEffects{}).Validate(); err == nil {
		t.Fatal("zero effects became qualified no-op evidence")
	}
	if _, err := recovery.NewWholePathRestorationEffects(false, false); err == nil {
		t.Fatal("absent-to-absent whole-path publication was admitted")
	}
}

func TestProjectionRestorationEffectsFollowActualRetainedRoot(t *testing.T) {
	placement, found := aggregate.MCPPlacementForID(aggregate.MCPPlacementClaudeProject)
	if !found {
		t.Fatal("Claude project MCP placement is unavailable")
	}
	codec, found := mcpcodec.For(placement.CodecContractID())
	if !found {
		t.Fatal("MCP codec is unavailable")
	}
	contract, err := placement.ProjectionContract("selected")
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	baseline := partialRead(t, codec, selection, aggregate.AbsentDocument())
	catalog, err := aggregate.NewCodecCatalog([]aggregate.Codec{codec})
	if err != nil {
		t.Fatal(err)
	}
	mode := recovery.PermissionMode(0o600)
	for _, test := range []struct {
		name, content string
		retained      bool
	}{
		{"last projection", `{"mcpServers":{"selected":{"type":"stdio","command":"old"}}}`, false},
		{"new foreign sibling", `{"mcpServers":{"selected":{"type":"stdio","command":"old"},"foreign":{"type":"stdio","command":"keep"}}}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			current := aggregate.ExistingDocument([]byte(test.content))
			rendered, effects, err := recovery.RestoreProjectionDocument(current, &mode, baseline, &mode, catalog)
			if err != nil || effects.Validate() != nil {
				t.Fatal(err)
			}
			if rendered.Document().Exists() != test.retained || effects.ChangesExistence() == test.retained || effects.ChangesMode() || !slices.Equal(effects.ValueFootprints(), []output.Footprint{contract.Address().Footprint()}) {
				t.Fatalf("historical root absence replaced actual candidate evidence: %+v", effects)
			}
			values := effects.ValueFootprints()
			values[0] = output.WholePathFootprint()
			if effects.ValueFootprints()[0] != contract.Address().Footprint() {
				t.Fatal("caller changed the selected effect envelope")
			}
			if test.retained {
				foreign, err := placement.ProjectionContract("foreign")
				if err != nil {
					t.Fatal(err)
				}
				foreignSelection, err := aggregate.NewSelection([]aggregate.ProjectionContract{foreign})
				if err != nil {
					t.Fatal(err)
				}
				actual := partialRead(t, codec, foreignSelection, rendered.Document())
				if !actual.States()[0].Present() {
					t.Fatal("restoration erased a foreign sibling")
				}
			}
		})
	}
	if _, _, err := recovery.RestoreProjectionDocument(aggregate.AbsentDocument(), nil, baseline, nil, aggregate.CodecCatalog{}); err == nil {
		t.Fatal("unknown codec produced effect evidence")
	}
}

func TestHookRestorationSeparatesRootExistenceFromSelectedValues(t *testing.T) {
	codec, selection := partialHookProjection(t)
	catalog, err := aggregate.NewCodecCatalog([]aggregate.Codec{codec})
	if err != nil {
		t.Fatal(err)
	}
	mode := recovery.PermissionMode(0o600)
	missing, empty := aggregate.AbsentDocument(), aggregate.ExistingDocument([]byte(`{}`))
	for _, scenario := range []struct {
		name              string
		current, baseline aggregate.Document
		currentMode       *recovery.PermissionMode
	}{
		{"remove empty root", empty, missing, &mode},
		{"restore empty root", missing, empty, nil},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			baseline := partialRead(t, codec, selection, scenario.baseline)
			rendered, effects, err := recovery.RestoreProjectionDocument(scenario.current, scenario.currentMode, baseline, &mode, catalog)
			if err != nil {
				t.Fatal(err)
			}
			if rendered.Document().Exists() != scenario.baseline.Exists() || !partialRead(t, codec, selection, rendered.Document()).Equal(baseline) {
				t.Fatal("Hook restoration did not preserve the exact baseline")
			}
			if effects.Validate() != nil || !effects.Changed() || !effects.ChangesExistence() || effects.ChangesMode() || len(effects.ValueFootprints()) != 0 {
				t.Fatalf("root-only restoration lost its existence effect or invented a value effect: %+v", effects)
			}
		})
	}
}
