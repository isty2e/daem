package profile_test

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	opencodeconfig "github.com/isty2e/daem/internal/realization/configrelation/opencode"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func TestDocumentSpecsAdmitDedicatedMCPAliasesFromTheirOwner(t *testing.T) {
	for _, placement := range aggregate.ImplementedMCPPlacements() {
		spec, err := configuration.DocumentSpecForAlias(configuration.AliasInput{
			Target: placement.Target(), Scope: placement.Scope(), Destination: placement.ConfigPath(),
		})
		if err != nil || spec.Validate() != nil || spec.Destination() != placement.ConfigPath() || spec.Class() != configuration.SharedConfiguration {
			t.Fatalf("dedicated MCP document %s lost its contract: %v", placement.ID(), err)
		}
	}
}

func TestOrdinaryWritersMayUseANativeAliasSpelling(t *testing.T) {
	claude, present := aggregate.HookPlacementFor(target.TargetClaudeCode, target.ScopeGlobal)
	if !present {
		t.Fatal("Claude's shared hook placement is required")
	}
	openCode, present := aggregate.ImplementedMCPPlacement(target.TargetOpenCode, target.ScopeGlobal)
	if !present {
		t.Fatal("OpenCode's shared MCP placement is required")
	}
	for _, input := range []configuration.AliasInput{
		{Target: claude.Target(), Scope: claude.Scope(), Destination: claude.AggregateRoot()},
		{Target: openCode.Target(), Scope: openCode.Scope(), Destination: openCode.ConfigPath()},
	} {
		spec, err := configuration.DocumentSpecForAlias(input)
		if err != nil {
			t.Fatal(err)
		}
		nativePath, native := spec.NativePlacementPath()
		relative, directory := spec.Coordinate().DirectoryChild()
		if !native || !directory || spec.Destination().RootRole() != output.RootHome || relative == spec.Destination().RelativePath() {
			t.Fatal("an ordinary spelling was mistaken for a native-only resolution contract")
		}
		fromNative, err := configuration.NewDocumentSpec(configuration.DocumentInput{
			Target: input.Target, Scope: input.Scope, Path: nativePath,
		})
		if err != nil || fromNative != spec {
			t.Fatalf("the shared spelling lost its explicit native binding: %v", err)
		}
	}
}

func TestDocumentSpecsFollowOpenCodeCandidateSelection(t *testing.T) {
	manifestRoot, homeRoot := t.TempDir(), t.TempDir()
	globalRoot, err := opencodeconfig.DefaultGlobalConfigRoot("", homeRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range target.SupportedScopes() {
		directory, err := opencodeconfig.ConfigDirectory(manifestRoot, globalRoot, scope)
		if err != nil {
			t.Fatal(err)
		}
		base, role := manifestRoot, output.RootProject
		if scope == target.ScopeGlobal {
			base, role = homeRoot, output.RootHome
		}
		relativeDirectory, err := filepath.Rel(base, directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, kind := range []opencodeconfig.ConfigKind{opencodeconfig.ConfigServer, opencodeconfig.ConfigTUI} {
			names, err := opencodeconfig.CandidateNames(kind)
			if err != nil || len(names) != 2 {
				t.Fatalf("candidate family no longer has its paired contract: %v", err)
			}
			for index, name := range names {
				alias, err := output.NewDestination(role, filepath.ToSlash(filepath.Join(relativeDirectory, name)))
				if err != nil {
					t.Fatal(err)
				}
				spec, err := configuration.DocumentSpecForAlias(configuration.AliasInput{Target: target.TargetOpenCode, Scope: scope, Destination: alias})
				if err != nil || spec.Class() != configuration.SharedConfiguration || spec.Grammar() != configuration.JSONC {
					t.Fatalf("dedicated %s candidate lost native sharing: %v", kind, err)
				}
				alternate, found := spec.AlternateFile()
				expected, err := output.NewDestination(role, filepath.ToSlash(filepath.Join(relativeDirectory, names[1-index])))
				if err != nil || !found || alternate != expected {
					t.Fatalf("dedicated candidate relation drifted: %v", err)
				}
			}
		}
	}
}

func nativeOperationSelection(t *testing.T, selected target.Target, scope target.Scope, path string, syntax hostconfig.Syntax, key string) aggregate.Selection {
	t.Helper()
	placement, err := profile.ResolveHostConfigPlacement(configuration.PlacementInput{Target: selected, Scope: scope, Path: path, Ownership: hostconfig.OwnershipKeys, Syntax: syntax})
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
	contract, err := profile.HostConfigProjectionContract(placement, footprint)
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
				expected := []configuration.Observation{configuration.ReferentAliases, configuration.InstalledClaims, configuration.PiLoader}
				if test.target == target.TargetCodex {
					expected[2] = configuration.CodexRuntimeLocations
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
					if _, err := profile.HostConfigOperationRoute(operation.Placement(), kind); err != nil {
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
