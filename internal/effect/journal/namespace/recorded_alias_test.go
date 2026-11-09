package namespace

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func TestRecordedAliasFactsSurviveEveryNamespaceRequalification(t *testing.T) {
	for _, change := range []string{"root revision", "relative path"} {
		t.Run(change, func(t *testing.T) {
			selected := nativeNamespaceRoot(t, t.TempDir())
			reference, err := configuration.RootReferenceForScope(target.TargetPi, target.ScopeGlobal)
			if err != nil {
				t.Fatal(err)
			}
			relative := "retained.json"
			if change == "root revision" {
				reference, err = output.NewRootReference(reference.Identity(), "retained-fixture-v1")
				if err != nil {
					t.Fatal(err)
				}
				relative = "live.json"
			}
			path := filepath.Join(selected.SelectedPath, relative)
			before := []byte(`{"retained":true}`)
			if err := os.WriteFile(path, before, 0o600); err != nil {
				t.Fatal(err)
			}
			roots, err := (Roots{}).With(reference, selected)
			if err != nil {
				t.Fatal(err)
			}
			authority, err := selected.Root.Authority()
			if err != nil {
				t.Fatal(err)
			}
			provenance, err := authority.Provenance()
			if err != nil {
				t.Fatal(err)
			}
			writer, err := rootedpath.NewRelativeDestination(relative)
			if err != nil {
				t.Fatal(err)
			}
			pin, err := NewWriterPin(provenance, writer)
			if err != nil {
				t.Fatal(err)
			}
			location, err := configuration.NewAliasLocation(reference, relative)
			if err != nil {
				t.Fatal(err)
			}
			input := RecordedAlias{Address: nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/live.json"), Location: location}
			inputs := []RecordedAlias{input}
			namespace, err := Reobserve(t.Context(), inputs, roots, fixtureProtection(t), journalTestFilesystem(), imageBudget(t), pin)
			if err != nil {
				t.Fatal(err)
			}
			if aliases := namespace.Aliases(); len(aliases) != 1 || aliases[0].Location() != location {
				t.Fatal("replay substituted the current alias location")
			}
			if _, known := namespace.Aliases()[0].Binding(); known {
				t.Fatal("replay invented an unrecorded document policy")
			}
			if _, bound := namespace.WriterPin().RootReference(); bound {
				t.Fatal("replay invented a reference for a legacy writer pin")
			}
			for _, bad := range [][]RecordedAlias{{{Address: input.Address}}, {input, input}} {
				reader := &nativeNamespaceReader{RootedReader: journalTestFilesystem()}
				if result, err := Reobserve(t.Context(), bad, roots, fixtureProtection(t), reader, imageBudget(t), pin); err == nil || result.Observation().Validate() == nil || len(reader.capabilities) != 0 {
					t.Fatal("incomplete or duplicate recorded aliases reached capability observation", err)
				}
			}
			inputs[0] = RecordedAlias{}
			root, destination, err := namespace.WriterBinding(t.Context(), imageBudget(t))
			if err != nil || root != selected.Root || !destination.Root().Equal(authority) || !destination.Relative().Equal(writer) {
				t.Fatal("writer requalification lost the retained alias input", err)
			}
			capability, err := namespace.AcquireReferent(t.Context(), imageBudget(t))
			if err != nil {
				t.Fatal(err)
			}
			content, _, _, readErr := journalTestFilesystem().ReadRootedRegularFileUpTo(t.Context(), capability, 4096)
			closeErr := capability.Close()
			if readErr != nil || closeErr != nil || !bytes.Equal(content, before) {
				t.Fatal("referent acquisition reselected a current root or path", readErr, closeErr)
			}
		})
	}
}

func TestRecordedDirectoryAliasCannotUseAPointWitness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "point.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	selected, err := CaptureEndpointRoot(t.Context(), [2]string{path, path}, imageBudget(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = selected.Close() })
	reference, err := configuration.RootReferenceForScope(target.TargetPi, target.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	location, err := configuration.NewAliasLocation(reference, "point.json")
	if err != nil {
		t.Fatal(err)
	}
	roots, err := (Roots{}).With(reference, selected)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := selected.capturedRoot().Authority()
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := authority.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := rootedpath.NewRelativeDestination("point.json")
	if err != nil {
		t.Fatal(err)
	}
	pin, err := NewWriterPin(provenance, relative)
	if err != nil {
		t.Fatal(err)
	}
	reader := &nativeNamespaceReader{RootedReader: journalTestFilesystem()}
	input := RecordedAlias{Address: nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/point.json"), Location: location}
	if namespace, err := Reobserve(t.Context(), []RecordedAlias{input}, roots, fixtureProtection(t), reader, imageBudget(t), pin); err == nil || namespace.Observation().Validate() == nil || len(reader.capabilities) != 0 {
		t.Fatal("directory-relative alias was admitted through a selected file-point witness", err)
	}
}

func TestAliasInputRefusesCompetingLocationSources(t *testing.T) {
	address := nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/settings.json")
	inputs, err := currentAliasInputs(t.Context(), []aggregate.DocumentAddress{address})
	if err != nil {
		t.Fatal(err)
	}
	mixed := inputs[0]
	mixed.legacyLocation = mixed.binding.Location()
	if err := validateAliasInputs(t.Context(), []aliasInput{mixed}); err == nil {
		t.Fatal("a complete binding and policy-free location became one admitted input")
	}
}
