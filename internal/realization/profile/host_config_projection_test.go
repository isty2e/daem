package profile

import (
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	desiredhostconfig "github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestHostConfigProjectionUsesStaticAdmissionInBothDirections(t *testing.T) {
	id, err := entity.New(entity.KindHostConfig, "review")
	if err != nil {
		t.Fatal(err)
	}
	for _, selected := range []target.Target{target.TargetPi, target.TargetCodex} {
		for _, scope := range []target.Scope{target.ScopeGlobal, target.ScopeProject} {
			for _, syntax := range []desiredhostconfig.Syntax{desiredhostconfig.SyntaxJSON, desiredhostconfig.SyntaxJSONC, desiredhostconfig.SyntaxJSONLineComments, desiredhostconfig.SyntaxTOML} {
				placement := mustHostConfigPlacement(t, HostConfigPlacementInput{
					Target: selected, Scope: scope, Path: "extensions/subagent/config.data", Syntax: syntax,
				})
				footprint := mustHostConfigKey(t, "providers", "a/b~c", "", "\x00\n")
				contract, err := placement.ProjectionContract(footprint)
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := HostConfigPlacementForContract(contract)
				if err != nil || decoded.Target() != selected || decoded.Scope() != scope ||
					decoded.NativePath() != placement.NativePath() || decoded.Grammar() != placement.Grammar() {
					t.Fatalf("placement round trip: %v", err)
				}
				key, _ := footprint.Key()
				subject, err := topologyprojection.HostConfigKeySubject(id, selected, scope, key)
				if err != nil {
					t.Fatal(err)
				}
				if err := aggregate.ValidateSubjectContract(subject, contract); err != nil {
					t.Fatal(err)
				}
				if contract.Address().PlacementID() != placement.ID() || contract.Cardinality() != aggregate.ContributionExclusive ||
					contract.Equivalence() != aggregate.EquivalenceCanonicalSemantic ||
					contract.SiblingPreservation() != aggregate.PreserveSiblingsNativeValuesComments {
					t.Fatal("native contract lost its static guarantees")
				}
				if _, err := contract.Address().LegacyContentPath(); !errors.Is(err, output.ErrNotLegacyFootprint) {
					t.Fatalf("native names escaped legacy boundary: %v", err)
				}
			}
		}
	}
}

func TestHostConfigProjectionRefusesReservationsAndContractForgery(t *testing.T) {
	placement := mustHostConfigPlacement(t, hostConfigInput(target.TargetCodex, target.ScopeGlobal, "config.toml"))
	reserved := mustHostConfigKey(t, "mcp_servers", "example")
	if _, err := placement.ProjectionContract(reserved); !errors.Is(err, ErrHostConfigReserved) {
		t.Fatalf("authoring did not refuse reserved field: %v", err)
	}
	for _, tc := range []struct {
		path  string
		codec aggregate.CodecContractID
		key   output.Footprint
	}{
		{"@codex-home/config.toml", "native-toml-v1", reserved},
		{"@codex-home/config.toml", "native-json-v1", mustHostConfigKey(t, "model")},
		{"@codex-home/auth.json", "native-json-v1", mustHostConfigKey(t, "key")},
		{"@pi-agent/custom.json", "native-json-v1", mustHostConfigKey(t, "key")},
		{"~/arbitrary.json", "native-json-v1", mustHostConfigKey(t, "key")},
	} {
		root, err := output.Parse(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		forged, err := aggregate.NewHostConfigProjectionContract(target.TargetCodex, target.ScopeGlobal, root, tc.key, tc.codec)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := HostConfigPlacementForContract(forged); err == nil {
			t.Fatalf("decoded contract bypassed admission for %s", tc.path)
		}
	}
	if _, err := placement.ProjectionContract(output.WholePathFootprint()); err == nil {
		t.Fatal("whole path became native key projection")
	}
	document := mustHostConfigPlacement(t, HostConfigPlacementInput{
		Target: target.TargetPi, Scope: target.ScopeGlobal, Path: "custom.json", Ownership: desiredhostconfig.OwnershipDocument,
	})
	if _, err := document.ProjectionContract(mustHostConfigKey(t, "key")); err == nil {
		t.Fatal("whole-document declaration became a key declaration")
	}
}

func TestHostConfigKeyIdentityIsIndependentOfGrammarAndDocument(t *testing.T) {
	id, err := entity.New(entity.KindHostConfig, "review")
	if err != nil {
		t.Fatal(err)
	}
	key, _ := output.NewKeyPath("theme")
	first, err := topologyprojection.HostConfigKeySubject(id, target.TargetPi, target.ScopeGlobal, key)
	if err != nil {
		t.Fatal(err)
	}
	for _, nativePath := range []string{"a.json", "b.toml"} {
		placement := mustHostConfigPlacement(t, hostConfigInput(target.TargetPi, target.ScopeGlobal, nativePath))
		contract, err := placement.ProjectionContract(mustHostConfigKey(t, "theme"))
		if err != nil {
			t.Fatal(err)
		}
		if err := aggregate.ValidateSubjectContract(first, contract); err != nil {
			t.Fatal(err)
		}
	}
}
