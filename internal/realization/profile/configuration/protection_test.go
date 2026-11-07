package configuration

import (
	"errors"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

func TestKnownPhysicalDocumentsRetainTheirCanonicalRootAndRole(t *testing.T) {
	project, err := ProjectRootReference()
	if err != nil {
		t.Fatal(err)
	}
	pi, err := RootReferenceForScope(target.TargetPi, target.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	codex, err := RootReferenceForScope(target.TargetCodex, target.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		reference output.RootReference
		names     []string
		selected  target.Target
		scope     target.Scope
		relative  string
	}{
		{project, nil, target.TargetPi, target.ScopeProject, ".pi/settings.json"},
		{pi, nil, target.TargetPi, target.ScopeGlobal, "settings.json"},
		{codex, []string{"work.config.toml", "work.config.toml", "unrelated.toml"}, target.TargetCodex, target.ScopeGlobal, "work.config.toml"},
	} {
		documents, err := KnownRootDocuments(scenario.reference, scenario.names)
		if err != nil {
			t.Fatal(err)
		}
		matched := 0
		for _, document := range documents {
			if document.Target() == scenario.selected && document.Scope() == scenario.scope && document.RootRelativePath() == scenario.relative {
				matched++
				if document.RootReference() != scenario.reference || document.Class() != SharedConfiguration {
					t.Fatal("physical document lost its constructor-checked role", document)
				}
			}
			if document.RootRelativePath() == "unrelated.toml" {
				t.Fatal("directory enumeration invented a known role for a generic file")
			}
		}
		if matched != 1 {
			t.Fatal("known document was lost or duplicated", scenario.relative, matched)
		}
	}
}

func TestPhysicalDocumentAdmitsOnlyCompatibleWriterLanguages(t *testing.T) {
	for _, scenario := range []struct {
		host    target.Target
		path    string
		writers []Grammar
	}{
		{target.TargetPi, "settings.json", []Grammar{JSON}},
		{target.TargetPi, "models.json", []Grammar{JSON, JSONLineComments}},
		{target.TargetOpenCode, "opencode.json", []Grammar{JSON, JSONLineComments, JSONC}},
		{target.TargetCodex, "config.toml", []Grammar{TOML}},
	} {
		spec, err := NewDocumentSpec(DocumentInput{Target: scenario.host, Scope: target.ScopeGlobal, Path: scenario.path})
		if err != nil {
			t.Fatal(err)
		}
		for _, writer := range []Grammar{"", Opaque, JSON, JSONLineComments, JSONC, TOML} {
			err := spec.RequireWriterGrammar(writer)
			if slices.Contains(scenario.writers, writer) != (err == nil) || err != nil && !errors.Is(err, ErrGrammar) {
				t.Fatalf("%s %q writer %q disagrees with its physical grammar: %v", scenario.host, scenario.path, writer, err)
			}
		}
	}
}

func TestReservationDescendantPossibilityUsesTheEntireStablePrefix(t *testing.T) {
	policies, err := NativeExclusionPolicies()
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range policies {
		if policy.Coverage() != ExclusionAudited {
			continue
		}
		facets, err := policy.Facets()
		if err != nil {
			t.Fatal(err)
		}
		for _, facet := range facets {
			for _, prefix := range []string{"extension-settings", "extension-settings/user", "knowné"} {
				if facet.CouldReserveBelow(prefix) {
					t.Fatal("stable ordinary prefix became an unknown reservation", policy.Target(), prefix)
				}
			}
			if policy.Target() == target.TargetPi {
				if !facet.CouldReserveBelow("extensions/pkg") || facet.CouldReserveBelow("extensions/pkg/source") {
					t.Fatal("module entrypoint possibility lost its admitted depth")
				}
			} else if policy.Target() == target.TargetCodex {
				if !facet.CouldReserveBelow("app-server-daemon") || facet.CouldReserveBelow("app-server-daemon/user") {
					t.Fatal("runtime metadata possibility lost its fixed-name depth")
				}
			}
		}
	}
}

func TestPhysicalExclusionProjectionUsesTheCanonicalGlobalRules(t *testing.T) {
	policies, err := NativeExclusionPolicies()
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range policies {
		if policy.Coverage() == ExclusionUnknown {
			if _, err := policy.Facets(); err == nil {
				t.Fatal("unaudited host supplied checked-empty facets")
			}
			continue
		}
		facets, err := policy.Facets()
		if err != nil {
			t.Fatal(err)
		}
		for _, facet := range facets {
			for _, path := range []string{"auth.json", "settings.json", "sessions/x", "extensions/pkg/index.ts", "extensions/pkg/notes.json", "state_5.sqlite-wal", "unrelated.json"} {
				reservation, matched := facet.ReservationAt(path)
				err := facet.fact.requirePath(target.ScopeGlobal, path)
				if matched != errors.Is(err, ErrReserved) || matched && (reservation.Path() == "" || reservation.Owner() == "") {
					t.Errorf("%s physical %q disagrees with its owner: %v", policy.Target(), path, err)
				}
			}
			reservations := facet.FixedReservations()
			if len(reservations) != 0 {
				reservations[0] = PathReservation{}
				if facet.FixedReservations()[0].Path() == "" {
					t.Fatal("reservation projection exposes mutable catalog storage")
				}
			}
		}
	}
}

func TestNativeEndpointAnchorsDoNotBecomeWholeDirectoryEnvelopes(t *testing.T) {
	contracts, err := NativeRootContracts()
	if err != nil {
		t.Fatal(err)
	}
	for _, alias := range dedicatedDocumentAliases {
		if alias.scope != target.ScopeGlobal {
			continue
		}
		reference, err := alias.root.reference()
		if err != nil {
			t.Fatal(err)
		}
		index := slices.IndexFunc(contracts, func(contract NativeRootContract) bool { return contract.Reference() == reference })
		if index < 0 {
			t.Fatal("dedicated native alias lost its root contract")
		}
		contract := contracts[index]
		if alias.root == claudeAccount || alias.root == antigravityShared {
			if contract.Envelope() != NativeEndpoints || !slices.Contains(contract.Endpoints(), alias.relative) {
				t.Fatal("outside-directory native alias received an ambient-directory envelope")
			}
			endpoints := contract.Endpoints()
			endpoints[0] = "modified"
			if slices.Contains(contract.Endpoints(), "modified") {
				t.Fatal("endpoint envelope exposes mutable catalog storage")
			}
		}
	}
}
