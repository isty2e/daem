package profile_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

func nativeOperationSelection(t *testing.T, selected target.Target, scope target.Scope, path string, syntax hostconfig.Syntax, key string) aggregate.Selection {
	t.Helper()
	placement, err := profile.ResolveHostConfigPlacement(profile.HostConfigPlacementInput{Target: selected, Scope: scope, Path: path, Ownership: hostconfig.OwnershipKeys, Syntax: syntax})
	if err != nil {
		t.Fatal(err)
	}
	keyPath, err := output.NewKeyPath(key)
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := placement.ProjectionContract(footprint)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	return selection
}

func TestNativeAggregateOperationBindsSelectedProfileAndCodec(t *testing.T) {
	for _, test := range []struct {
		name    string
		target  target.Target
		path    string
		syntax  hostconfig.Syntax
		content string
	}{
		{"json", target.TargetPi, "settings.json", hostconfig.SyntaxJSON, `{"theme":"night"}`},
		{"jsonc", target.TargetPi, "extension-settings/custom.jsonc", hostconfig.SyntaxJSONC, "// note\n{\"theme\":\"night\",}"},
		{"line-comments", target.TargetPi, "models.json", hostconfig.SyntaxJSONLineComments, "// note\n{\"theme\":\"night\"}"},
		{"toml", target.TargetCodex, "config.toml", hostconfig.SyntaxTOML, "theme = \"night\"\n"},
	} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			t.Run(test.name+"/"+string(scope), func(t *testing.T) {
				selection := nativeOperationSelection(t, test.target, scope, test.path, test.syntax, "theme")
				limits := configdocument.Limits{MaximumBytes: 1024, MaximumDepth: 32}
				catalog, err := aggregatecodec.ForOperation(t.Context(), []aggregate.CodecContractID{selection.CodecContractID()}, limits, limits)
				if err != nil {
					t.Fatal(err)
				}
				operation, err := profile.NewNativeAggregateOperationContract(catalog, selection)
				if err != nil || operation.ValidateSelection(selection) != nil || operation.Placement().Target() != test.target || operation.Placement().Scope() != scope {
					t.Fatal("selected native profile was not retained", err)
				}
				expected := []profile.HostConfigObservation{profile.HostConfigObserveReferentAliases, profile.HostConfigObserveInstalledClaims, profile.HostConfigObservePiLoader}
				if test.target == target.TargetCodex {
					expected[2] = profile.HostConfigObserveCodexRuntimeLocations
				}
				if !slices.Equal(operation.RequiredObservations(), expected) {
					t.Fatal("static admission lost required physical observations")
				}
				copied := operation.RequiredObservations()
				copied[0] = "changed"
				contracts := operation.Selection().Contracts()
				contracts[0] = aggregate.ProjectionContract{}
				if !slices.Equal(operation.RequiredObservations(), expected) || operation.ValidateSelection(selection) != nil {
					t.Fatal("caller mutation changed the static contract")
				}
				for _, kind := range []profile.Operation{profile.OperationWrite, profile.OperationRemove} {
					if _, err := operation.OperationRoute(kind); err != nil {
						t.Fatal(err)
					}
				}
				snapshot, failure := operation.Codec().Read(aggregate.ExistingDocument([]byte(test.content)), operation.Selection())
				if failure != nil || len(snapshot.States()) != 1 || !snapshot.States()[0].Present() {
					t.Fatal("bound concrete codec cannot read its selected grammar", failure)
				}
				if _, err := profile.NewNativeAggregateOperationContract(aggregatecodec.Catalog(), selection); err == nil {
					t.Fatal("ordinary catalog admitted a native operation")
				}
				other := nativeOperationSelection(t, test.target, scope, test.path, test.syntax, "other")
				if operation.ValidateSelection(other) == nil {
					t.Fatal("static operation was rebound to another key selection")
				}
			})
		}
	}
}

func TestNativeAggregateOperationRejectsReservedSelection(t *testing.T) {
	selected := nativeOperationSelection(t, target.TargetPi, target.ScopeGlobal, "settings.json", hostconfig.SyntaxJSON, "theme")
	key, _ := output.NewKeyPath("packages")
	footprint, _ := output.NewKeyFootprint(key)
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeGlobal, selected.DocumentAddress().AggregateRoot(), footprint, selected.CodecContractID())
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	limits := configdocument.Limits{MaximumBytes: 1024, MaximumDepth: 32}
	catalog, err := aggregatecodec.ForOperation(t.Context(), []aggregate.CodecContractID{selected.CodecContractID()}, limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	if operation, err := profile.NewNativeAggregateOperationContract(catalog, reserved); err == nil || operation.Validate() == nil {
		t.Fatal("generic native contract bypassed profile field reservations")
	}
	if (profile.NativeAggregateOperationContract{}).Validate() == nil {
		t.Fatal("zero static contract was usable")
	}
}

func TestNativeAggregateOperationRetainsPortContextAndLimits(t *testing.T) {
	selection := nativeOperationSelection(t, target.TargetPi, target.ScopeGlobal, "settings.json", hostconfig.SyntaxJSON, "theme")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	limits := configdocument.Limits{MaximumBytes: 32, MaximumDepth: 8}
	catalog, err := aggregatecodec.ForOperation(ctx, []aggregate.CodecContractID{selection.CodecContractID()}, limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := profile.NewNativeAggregateOperationContract(catalog, selection)
	if err != nil {
		t.Fatal(err)
	}
	if _, failure := operation.Codec().Read(aggregate.ExistingDocument([]byte(strings.Repeat(" ", 40)+"{}")), selection); failure == nil {
		t.Fatal("static contract reconstructed a codec with a different byte budget")
	}
	cancel()
	if err := operation.Validate(); err != nil {
		t.Fatal("static validity was confused with current operation cancellation", err)
	}
	if _, failure := operation.Codec().Read(aggregate.ExistingDocument([]byte(`{"theme":"night"}`)), selection); failure == nil {
		t.Fatal("static contract lost the actual operation-bound codec context")
	}
}
