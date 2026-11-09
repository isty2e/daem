package namespace

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

type pointAliasFixture struct {
	roots     Roots
	qualified QualifiedRoots
	reference output.RootReference
	selected  RootSelection
	input     RecordedAlias
	pin       WriterPin
}

func capturePointAliasFixture(t *testing.T, path string) pointAliasFixture {
	t.Helper()
	selected, err := CaptureEndpointRoot(t.Context(), [2]string{path, path}, imageBudget(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = selected.Close() })
	reference, err := output.NewRootReference("pi-agent", "point-fixture-v1")
	if err != nil {
		t.Fatal(err)
	}
	roots, err := (Roots{}).With(reference, selected)
	if err != nil {
		t.Fatal(err)
	}
	qualified, err := roots.Qualify(t.Context(), imageBudget(t))
	if err != nil {
		t.Fatal(err)
	}
	location, err := configuration.NewPointAliasLocation(reference)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := NewBoundWriterPin(reference, qualified.grants[reference].Provenance(), configuration.PointSelf())
	if err != nil {
		t.Fatal(err)
	}
	return pointAliasFixture{
		roots: roots, qualified: qualified, reference: reference, selected: selected, pin: pin,
		input: RecordedAlias{
			Address:  nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/point.json"),
			Location: location,
		},
	}
}

func TestPointSelfRetainsItsCoordinateThroughNamespaceRequalification(t *testing.T) {
	for _, linked := range []bool{false, true} {
		name := "regular file"
		if linked {
			name = "final alias"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "point.json")
			content := []byte(`{"selected":true}`)
			referent := path
			if linked {
				referent = filepath.Join(t.TempDir(), "target.json")
			}
			if err := os.WriteFile(referent, content, 0o600); err != nil {
				t.Fatal(err)
			}
			if linked {
				if err := os.Symlink(referent, path); err != nil {
					t.Skipf("symlinks are unavailable: %v", err)
				}
			}
			fixture := capturePointAliasFixture(t, path)
			namespace, err := Reobserve(t.Context(), []RecordedAlias{fixture.input}, fixture.roots, fixtureProtection(t), journalTestFilesystem(), imageBudget(t), fixture.pin)
			if err != nil {
				t.Fatal(err)
			}
			pin := namespace.WriterPin()
			if reference, bound := pin.RootReference(); !bound || reference != fixture.reference || pin.Coordinate() != configuration.PointSelf() {
				t.Fatal("point pin lost its selected reference or became a child coordinate", pin)
			}
			root, destination, err := namespace.WriterBinding(t.Context(), imageBudget(t))
			if err != nil || root != fixture.selected.capturedRoot() {
				t.Fatal("point writer did not retain its qualified witness", err)
			}
			physical, err := destination.LexicalPath()
			if err != nil || physical != fixture.selected.endpoint.physical {
				t.Fatal("writer did not derive the point's physical destination", physical, err)
			}
			capability, err := namespace.AcquireReferent(t.Context(), imageBudget(t))
			if err != nil {
				t.Fatal(err)
			}
			observed, _, _, readErr := journalTestFilesystem().ReadRootedRegularFileUpTo(t.Context(), capability, 4096)
			closeErr := capability.Close()
			if readErr != nil || closeErr != nil || !bytes.Equal(observed, content) {
				t.Fatal("point acquisition changed its referent", readErr, closeErr)
			}
			aliases := namespace.Aliases()
			route, err := aliases[0].ObserveRoute(t.Context(), imageBudget(t))
			if err != nil || linked == route.Equal(namespace.Observation()) {
				t.Fatal("route observation conflated a final alias with its referent", err)
			}
		})
	}
}

func TestPointPinsDoNotAuthorizeDirectoryChildrenOrDisplaceContainingWriters(t *testing.T) {
	parent := t.TempDir()
	path := filepath.Join(parent, "point.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture := capturePointAliasFixture(t, path)
	grant := fixture.qualified.grants[fixture.reference]
	boundRoots := map[output.RootReference]struct{}{fixture.reference: {}}
	for _, child := range []string{"point.json", "sibling.json"} {
		relative, err := rootedpath.NewRelativeDestination(child)
		if err != nil {
			t.Fatal(err)
		}
		coordinate, err := configuration.NewDirectoryChild(child)
		if err != nil {
			t.Fatal(err)
		}
		legacy, err := NewWriterPin(grant.Provenance(), relative)
		if err != nil {
			t.Fatal(err)
		}
		bound, err := NewBoundWriterPin(fixture.reference, grant.Provenance(), coordinate)
		if err != nil {
			t.Fatal(err)
		}
		for _, pin := range []WriterPin{legacy, bound} {
			if _, _, err := nativeDocumentWriterGrant(fixture.qualified.grants, boundRoots, fixture.selected.endpoint.physical, grant.endpoint, &pin); !errors.Is(err, ErrNoTrustedWriterRoot) {
				t.Fatal("point witness authorized a saved directory child", child, err)
			}
		}
	}

	directory := nativeNamespaceRoot(t, parent)
	directoryRoots, err := (Roots{}).With(fixture.reference, directory)
	if err != nil {
		t.Fatal(err)
	}
	qualified, err := directoryRoots.Qualify(t.Context(), imageBudget(t))
	if err != nil || qualified.grants[fixture.reference].Provenance() != grant.Provenance() {
		t.Fatal("coincident grant fixture has different provenance", err)
	}
	if _, _, err := nativeDocumentWriterGrant(qualified.grants, boundRoots, fixture.selected.endpoint.physical, grant.endpoint, &fixture.pin); !errors.Is(err, ErrNoTrustedWriterRoot) {
		t.Fatal("point-self pin was reinterpreted as a directory grant", err)
	}
	reader := &nativeNamespaceReader{RootedReader: journalTestFilesystem()}
	if value, err := Reobserve(t.Context(), []RecordedAlias{fixture.input}, directoryRoots, fixtureProtection(t), reader, imageBudget(t), fixture.pin); err == nil || value.Observation().Validate() == nil || len(reader.capabilities) != 0 {
		t.Fatal("point-self alias was admitted through a directory selection", err)
	}

	container, err := output.NewRootReference("independent-container", "directory-fixture-v1")
	if err != nil {
		t.Fatal(err)
	}
	roots, err := fixture.roots.With(container, directory)
	if err != nil {
		t.Fatal(err)
	}
	coordinate, err := configuration.NewDirectoryChild("point.json")
	if err != nil {
		t.Fatal(err)
	}
	pin, err := NewBoundWriterPin(container, grant.Provenance(), coordinate)
	if err != nil {
		t.Fatal(err)
	}
	value, err := Reobserve(t.Context(), []RecordedAlias{fixture.input}, roots, fixtureProtection(t), journalTestFilesystem(), imageBudget(t), pin)
	if err != nil || value.WriterPin().Coordinate() != coordinate || value.Aliases()[0].Location().Coordinate() != configuration.PointSelf() {
		t.Fatal("point alias excluded an independently selected containing writer", err)
	}
}

func TestDirectoryAliasCannotBorrowAPointParentThroughAnIndependentWriter(t *testing.T) {
	parent := t.TempDir()
	for _, name := range []string{"point.json", "sibling.json"} {
		if err := os.WriteFile(filepath.Join(parent, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fixture := capturePointAliasFixture(t, filepath.Join(parent, "point.json"))
	container, err := output.NewRootReference("independent-container", "directory-fixture-v1")
	if err != nil {
		t.Fatal(err)
	}
	roots, err := fixture.roots.With(container, nativeNamespaceRoot(t, parent))
	if err != nil {
		t.Fatal(err)
	}
	location, err := configuration.NewAliasLocation(fixture.reference, "sibling.json")
	if err != nil {
		t.Fatal(err)
	}
	coordinate, err := configuration.NewDirectoryChild("sibling.json")
	if err != nil {
		t.Fatal(err)
	}
	pin, err := NewBoundWriterPin(container, fixture.pin.Provenance(), coordinate)
	if err != nil {
		t.Fatal(err)
	}
	input := RecordedAlias{
		Address:  nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/sibling.json"),
		Location: location,
	}
	reader := &nativeNamespaceReader{RootedReader: journalTestFilesystem()}
	value, err := Reobserve(t.Context(), []RecordedAlias{input}, roots, fixtureProtection(t), reader, imageBudget(t), pin)
	if err == nil || value.Observation().Validate() == nil || len(value.Addresses()) != 0 || len(reader.capabilities) != 0 {
		t.Fatal("an independent writer made a point witness authorize a sibling alias", err)
	}
}

func TestPointLiteralRouteParticipatesInMembershipAndReservation(t *testing.T) {
	literalRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(literalRoot, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	literal := filepath.Join(literalRoot, "sessions", "point.json")
	referent := filepath.Join(t.TempDir(), "target.json")
	if err := os.WriteFile(referent, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(referent, literal); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	fixture := capturePointAliasFixture(t, literal)
	value, err := Reobserve(t.Context(), []RecordedAlias{fixture.input}, fixture.roots, fixtureProtection(t), journalTestFilesystem(), imageBudget(t), fixture.pin)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := configuration.RootReferenceForScope(target.TargetPi, target.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	selector, err := NewReservationSelector(reference, literalRoot)
	if err != nil {
		t.Fatal(err)
	}
	observer := newPhysicalObserver(ReservationSelectors{}, imageBudget(t))
	facts, err := observer.rootDocuments(t.Context(), value.Aliases(), selector, target.TargetPi, configuration.NativeDirectory)
	if err != nil || !slices.Contains(facts.global, reference) {
		t.Fatal("literal point alias disappeared from directory membership", facts.global, err)
	}
	policies, err := configuration.NativeExclusionPolicies()
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range policies {
		if policy.Target() != target.TargetPi {
			continue
		}
		facets, err := policy.Facets()
		if err != nil {
			t.Fatal(err)
		}
		for _, facet := range facets {
			if facet.Reference() != reference {
				continue
			}
			if _, reserved := facet.ReservationAt("sessions/point.json"); !reserved {
				t.Fatal("fixture route is not reserved")
			}
			if err := observer.requireFacet(t.Context(), value.Aliases(), target.TargetPi, facet, selector); !errors.Is(err, configuration.ErrReserved) {
				t.Fatal("point referent outside the reservation hid its reserved literal alias", err)
			}
			return
		}
	}
	t.Fatal("fixture has no Pi reservation facet")
}
