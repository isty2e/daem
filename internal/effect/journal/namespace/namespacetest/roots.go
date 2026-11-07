// Package namespacetest adapts native16 root fixtures to selected binding identities.
package namespacetest

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func LegacyRoots(t testing.TB, selections map[output.RootRole]namespace.RootSelection) namespace.Roots {
	t.Helper()
	var roots namespace.Roots
	for role, selection := range selections {
		if selection == (namespace.RootSelection{}) {
			continue
		}
		var err error
		roots, err = roots.With(legacyReference(t, role), selection)
		if err != nil {
			t.Fatal(err)
		}
	}
	return roots
}

func LegacyRoot(t testing.TB, roots namespace.Roots, role output.RootRole) namespace.RootSelection {
	t.Helper()
	selection, _ := roots.Selection(legacyReference(t, role))
	return selection
}

func WithLegacyRoot(t testing.TB, roots namespace.Roots, role output.RootRole, selection namespace.RootSelection) namespace.Roots {
	t.Helper()
	if selection == (namespace.RootSelection{}) {
		selections := make(map[output.RootRole]namespace.RootSelection)
		for _, other := range []output.RootRole{output.RootProject, output.RootPiAgent, output.RootCodexHome} {
			if other != role {
				selections[other] = LegacyRoot(t, roots, other)
			}
		}
		return LegacyRoots(t, selections)
	}
	updated, err := roots.With(legacyReference(t, role), selection)
	if err != nil {
		t.Fatal(err)
	}
	return updated
}

func legacyReference(t testing.TB, role output.RootRole) output.RootReference {
	t.Helper()
	var reference output.RootReference
	var err error
	switch role {
	case output.RootProject:
		reference, err = configuration.ProjectRootReference()
	case output.RootPiAgent:
		reference, err = configuration.RootReferenceForScope(target.TargetPi, target.ScopeGlobal)
	case output.RootCodexHome:
		reference, err = configuration.RootReferenceForScope(target.TargetCodex, target.ScopeGlobal)
	default:
		t.Fatalf("unknown native16 fixture root role %q", role)
	}
	if err != nil {
		t.Fatal(err)
	}
	return reference
}
