package configuration

import (
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/hostsurface"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

func placementFacts(t *testing.T, selected target.Target, scope target.Scope) DedicatedReservations {
	t.Helper()
	key, err := output.NewKeyPath("dedicated")
	if err != nil {
		t.Fatal(err)
	}
	facts, err := NewDedicatedReservations(DedicatedReservationsInput{
		Target: selected, Scope: scope, MCP: []output.KeyPath{key},
		Hooks: []output.KeyPath{key}, Extensions: []output.KeyPath{key},
	})
	if err != nil {
		t.Fatal(err)
	}
	return facts
}

func TestPlacementRetainsIntentWithoutDuplicatingSelectedKeys(t *testing.T) {
	input := PlacementInput{Target: target.TargetPi, Scope: target.ScopeGlobal, Path: "custom.json"}
	facts := placementFacts(t, input.Target, input.Scope)
	automatic, err := NewPlacement(input, facts)
	if err != nil {
		t.Fatal(err)
	}
	input.Syntax = hostconfig.SyntaxJSON
	explicit, err := NewPlacement(input, facts)
	if err != nil {
		t.Fatal(err)
	}
	if automatic.Grammar() != explicit.Grammar() || automatic == explicit {
		t.Fatal("resolved grammar erased declaration intent")
	}
	for _, placement := range []Placement{automatic, explicit} {
		if !placement.UsesKeyOwnership() {
			t.Fatal("key declaration lost its selected ownership policy")
		}
		if err := placement.Validate(); err != nil {
			t.Fatal(err)
		}
		key := placement.SurfaceKey()
		if key.Kind() != entity.KindHostConfig || key.Variant() != hostsurface.VariantDefault || key.Target() != input.Target || key.Scope() != input.Scope {
			t.Fatalf("native placement lost stable surface identity: %+v", key)
		}
		if placement.NativePath() != input.Path || placement.Document().Destination() != placement.Destination() {
			t.Fatal("native path/destination differs from the document reference")
		}
	}
	input.Ownership = hostconfig.OwnershipDocument
	document, err := NewPlacement(input, facts)
	if err != nil {
		t.Fatal(err)
	}
	if document.UsesKeyOwnership() || (Placement{}).UsesKeyOwnership() {
		t.Fatal("document or zero placement acquired key ownership intent")
	}
}

func TestPlacementValidateRejectsZeroAndInconsistentOwnedFacts(t *testing.T) {
	if (Placement{}).Validate() == nil {
		t.Fatal("zero placement was admitted")
	}
	input := PlacementInput{Target: target.TargetPi, Scope: target.ScopeGlobal, Path: "custom.json"}
	placement, err := NewPlacement(input, placementFacts(t, input.Target, input.Scope))
	if err != nil {
		t.Fatal(err)
	}
	forged := placement
	forged.document, err = NewDocumentSpec(DocumentInput{
		Target: input.Target, Scope: input.Scope, Path: input.Path, Syntax: hostconfig.SyntaxJSONC,
	})
	if err != nil {
		t.Fatal(err)
	}
	if forged.Validate() == nil {
		t.Fatal("grammar not implied by retained intent was accepted")
	}

	forged = placement
	forged.surface, err = hostsurface.NewSurfaceKey(input.Target, input.Scope, entity.KindSkill, hostsurface.VariantDefault)
	if err != nil {
		t.Fatal(err)
	}
	if forged.Validate() == nil {
		t.Fatal("another resource kind became a native placement")
	}
	forged.surface, err = hostsurface.NewSurfaceKey(input.Target, input.Scope, entity.KindHostConfig, "alternate")
	if err != nil {
		t.Fatal(err)
	}
	if forged.Validate() == nil {
		t.Fatal("an unrecorded native surface variant was accepted")
	}
	forged.surface, err = hostsurface.NewSurfaceKey(input.Target, target.ScopeProject, entity.KindHostConfig, hostsurface.VariantDefault)
	if err != nil {
		t.Fatal(err)
	}
	if forged.Validate() == nil {
		t.Fatal("surface/document scope disagreement was accepted")
	}

	forged = placement
	forged.document, err = NewDocumentSpec(DocumentInput{Target: input.Target, Scope: input.Scope, Path: "agents/reviewer.md"})
	if err != nil {
		t.Fatal(err)
	}
	if forged.Validate() == nil {
		t.Fatal("key ownership accepted an incompatible document reference")
	}
}

func TestPlacementRequiresAnAuditedNativePathPolicy(t *testing.T) {
	for _, selected := range []target.Target{target.TargetClaudeCode, target.TargetOpenCode, target.TargetAntigravityCLI} {
		if _, err := NewPlacement(PlacementInput{
			Target: selected, Scope: target.ScopeGlobal, Path: "custom.json",
		}, placementFacts(t, selected, target.ScopeGlobal)); err == nil {
			t.Fatalf("%s inherited another host's native path policy", selected)
		}
	}
}

func TestPlacementConstructorRetainsNativeExclusionsAndScope(t *testing.T) {
	for _, test := range []struct {
		selected target.Target
		scope    target.Scope
		path     string
		reserved bool
	}{
		{target.TargetPi, target.ScopeGlobal, "auth.json", true},
		{target.TargetPi, target.ScopeProject, "auth.json", false},
		{target.TargetPi, target.ScopeProject, "extensions/reviewer/index.ts", true},
		{target.TargetPi, target.ScopeProject, "extensions/reviewer/config.json", false},
		{target.TargetCodex, target.ScopeGlobal, "sessions/nested.json", true},
		{target.TargetCodex, target.ScopeProject, "sessions/nested.json", false},
		{target.TargetCodex, target.ScopeGlobal, "state_5.sqlite-wal", true},
		{target.TargetCodex, target.ScopeProject, "state_5.sqlite-wal", false},
	} {
		t.Run(string(test.selected)+"/"+string(test.scope)+"/"+test.path, func(t *testing.T) {
			placement, err := NewPlacement(PlacementInput{
				Target: test.selected, Scope: test.scope, Path: test.path, Syntax: hostconfig.SyntaxJSON,
			}, placementFacts(t, test.selected, test.scope))
			if test.reserved {
				if !errors.Is(err, ErrReserved) {
					t.Fatalf("native exclusion disappeared: %v", err)
				}
			} else if err != nil || placement.Validate() != nil {
				t.Fatalf("global or loader exclusion widened: %v", err)
			}
		})
	}
	if _, err := NewPlacement(PlacementInput{
		Target: target.TargetPi, Scope: target.ScopeGlobal, Path: "custom.json",
	}, placementFacts(t, target.TargetCodex, target.ScopeGlobal)); err == nil {
		t.Fatal("another cell's reservation facts were accepted")
	}
}
