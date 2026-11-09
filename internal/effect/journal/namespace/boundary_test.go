package namespace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func TestEndpointAnchorsCannotAuthorizeUnrelatedAliasGroups(t *testing.T) {
	contracts, err := configuration.NativeRootContracts()
	if err != nil {
		t.Fatal(err)
	}
	for _, contract := range contracts {
		if contract.Envelope() != configuration.NativeEndpoints {
			continue
		}
		for _, mode := range []string{"unbound endpoint", "pinned endpoint", "independent manifest", "unknown directory", "coincident pinned directory"} {
			t.Run(contract.Reference().Identity()+"/"+mode, func(t *testing.T) {
				base := t.TempDir()
				manifestPath := filepath.Join(base, "manifest")
				if mode == "independent manifest" {
					manifestPath = base
				}
				manifest := nativeNamespaceRoot(t, manifestPath)
				var independent RootSelection
				if mode == "independent manifest" {
					independent = manifest
				}
				anchor := nativeNamespaceRoot(t, filepath.Join(base, "anchor"))
				writerRelative := "anchor/sibling.json"
				referent := filepath.Join(anchor.SelectedPath, "sibling.json")
				if err := os.WriteFile(referent, []byte(`{"fixture":true}`), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Join(manifest.SelectedPath, ".pi"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(referent, filepath.Join(manifest.SelectedPath, ".pi", "alias.json")); err != nil {
					t.Skipf("symlinks are unavailable: %v", err)
				}

				roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: manifest})
				roots, err := roots.With(contract.Reference(), anchor)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "coincident pinned directory" {
					independent = anchor
					writerRelative = "sibling.json"
					reference, err := configuration.RootReferenceForScope(target.TargetCodex, target.ScopeGlobal)
					if err != nil {
						t.Fatal(err)
					}
					roots, err = roots.With(reference, independent)
					if err != nil {
						t.Fatal(err)
					}
				}
				if mode == "unknown directory" {
					independent = nativeNamespaceRoot(t, base)
					reference, err := output.NewRootReference("explicit-directory", "caller-selected-v1")
					if err != nil {
						t.Fatal(err)
					}
					roots, err = roots.With(reference, independent)
					if err != nil {
						t.Fatal(err)
					}
				}

				aliases := []aggregate.DocumentAddress{nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/alias.json")}
				reader := &nativeNamespaceReader{RootedReader: journalTestFilesystem()}
				budget := imageBudget(t)
				metadata, err := ObserveAlias(t.Context(), aliases[0], roots, budget)
				if err != nil || metadata.Validate() != nil {
					t.Fatal("alias grouping attempted premature writer qualification", err)
				}
				var value DocumentNamespace
				var observationErr error
				if mode == "pinned endpoint" || mode == "coincident pinned directory" {
					authority, err := anchor.Root.AuthorityBounded(budget)
					if err != nil {
						t.Fatal(err)
					}
					provenance, err := authority.Provenance()
					if err != nil {
						t.Fatal(err)
					}
					relative, err := rootedpath.NewRelativeDestination("sibling.json")
					if err != nil {
						t.Fatal(err)
					}
					pin, err := NewWriterPin(provenance, relative)
					if err != nil {
						t.Fatal(err)
					}
					value, observationErr = Reobserve(t.Context(), aliases, roots, fixtureProtection(t), reader, budget, pin)
				} else {
					value, observationErr = Observe(t.Context(), aliases, roots, fixtureProtection(t), reader, budget)
				}

				if independent.Root != nil {
					if observationErr != nil {
						t.Fatal("an independently supplied directory lost its containing grant", observationErr)
					}
					authority, err := independent.Root.AuthorityBounded(budget)
					if err != nil {
						t.Fatal(err)
					}
					provenance, err := authority.Provenance()
					if err != nil {
						t.Fatal(err)
					}
					if value.WriterPin().Provenance() != provenance || value.WriterPin().Relative().Path() != writerRelative {
						t.Fatal("a deeper unbound endpoint displaced the independently supplied directory", value.WriterPin())
					}
					image, _, _, err := value.ReadReferent(t.Context(), 1024, budget)
					if err != nil || string(image.Content()) != `{"fixture":true}` {
						t.Fatal("the directory-granted referent could not be read", err)
					}
				} else if !errors.Is(observationErr, ErrNoTrustedWriterRoot) || value.Observation().Validate() == nil || len(reader.capabilities) != 0 {
					t.Fatal("an unrelated endpoint anchor became a containing writer grant", observationErr)
				}
			})
		}
	}
}

func TestAliasRouteUsesItsAdmittedRootRelativeDestination(t *testing.T) {
	selection := nativeNamespaceRoot(t, t.TempDir())
	if err := os.Mkdir(filepath.Join(selection.SelectedPath, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"settings.json", ".claude/settings.json"} {
		if err := os.WriteFile(filepath.Join(selection.SelectedPath, filepath.FromSlash(path)), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	reference, err := output.NewRootReference("claude-preferences", "claude-preferences-location-v1")
	if err != nil {
		t.Fatal(err)
	}
	roots, err := (Roots{}).With(reference, selection)
	if err != nil {
		t.Fatal(err)
	}
	budget := imageBudget(t)
	qualified, err := roots.Qualify(t.Context(), budget)
	if err != nil {
		t.Fatal(err)
	}
	relative, err := rootedpath.NewRelativeDestination("settings.json")
	if err != nil {
		t.Fatal(err)
	}
	alias := Alias{
		address: nativeNamespaceAddress(t, target.TargetClaudeCode, target.ScopeGlobal, "~/.claude/settings.json"),
		root:    qualified.grants[reference], relative: relative,
	}
	observed, err := alias.ObserveRoute(t.Context(), budget)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := mutation.ObserveDirectoryEntryAuthorityBounded(filepath.Join(selection.SelectedPath, "settings.json"), physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil || !observed.Equal(expected) {
		t.Fatalf("alias route followed its portable prefix instead of its binding: %v", err)
	}
	if err := selection.Root.Close(); err != nil {
		t.Fatal(err)
	}
	if observed, err := alias.ObserveRoute(t.Context(), budget); err == nil || observed.Validate() == nil {
		t.Fatal("a closed alias root supplied route evidence")
	}
}

func TestWriterPinRetainsOnlyItsObservedSelectedRoot(t *testing.T) {
	root := nativeNamespaceRoot(t, t.TempDir())
	if err := os.MkdirAll(filepath.Join(root.SelectedPath, ".pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	alias := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/settings.json")
	roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: root})
	reader := &nativeNamespaceReader{RootedReader: journalTestFilesystem()}
	budget := imageBudget(t)
	original, err := Observe(t.Context(), []aggregate.DocumentAddress{alias}, roots, fixtureProtection(t), reader, budget)
	if err != nil {
		t.Fatal(err)
	}
	pin := original.WriterPin()
	pin, err = NewWriterPin(pin.Provenance(), pin.Relative())
	if err != nil {
		t.Fatal(err)
	}
	current, err := Reobserve(t.Context(), original.Addresses(), roots, fixtureProtection(t), reader, budget, pin)
	if err != nil || !original.Observation().Equal(current.Observation()) {
		t.Fatalf("canonical pin lost the selected namespace: %v", err)
	}
	aliases := current.Aliases()
	aliases[0] = Alias{}
	if current.Aliases()[0].Address() != alias {
		t.Fatal("namespace exposed its mutable alias slice")
	}
	qualified, err := current.QualifyRoots(t.Context(), budget)
	if err != nil {
		t.Fatal(err)
	}
	manifest, present := qualified.Manifest()
	if !present || manifest.Provenance() != pin.Provenance() || manifest.SelectedPath() != root.SelectedPath {
		t.Fatal("namespace lost its original root selection")
	}
	observation, err := manifest.ObserveRoute(t.Context(), alias.AggregateRoot(), budget)
	if err != nil {
		t.Fatal(err)
	}
	if !observation.Equal(original.Observation()) {
		t.Fatal("route query observed a different writer path")
	}
	other := nativeNamespaceRoot(t, t.TempDir())
	authority, err := other.Root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := authority.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := NewWriterPin(provenance, pin.Relative())
	if err != nil {
		t.Fatal(err)
	}
	if value, err := Reobserve(t.Context(), original.Addresses(), roots, fixtureProtection(t), reader, budget, foreign); err == nil || value.Observation().Validate() == nil || len(value.Addresses()) != 0 {
		t.Fatal("writer pin introduced a root not selected by the caller")
	}
}

func TestZeroCustodyValuesCannotAuthorizeWork(t *testing.T) {
	budget := imageBudget(t)
	if observed, err := ObserveAlias(t.Context(), aggregate.DocumentAddress{}, Roots{}, budget); err == nil || observed.Validate() == nil {
		t.Fatal("zero alias supplied grouping metadata")
	}
	if observed, err := (Alias{}).ObserveRoute(t.Context(), budget); err == nil || observed.Validate() == nil {
		t.Fatal("zero alias supplied route evidence")
	}
	var namespace DocumentNamespace
	if _, err := namespace.QualifyRoots(t.Context(), budget); err == nil {
		t.Fatal("zero namespace supplied qualified roots")
	}
	if value, err := namespace.AcquireReferent(t.Context(), budget); err == nil || value != nil {
		t.Fatal("zero namespace supplied a referent capability")
	}
	if _, err := namespace.ObserveRemovalNamespace(t.Context(), recovery.RemovalNamespaceAuthority{}, budget); err == nil {
		t.Fatal("zero namespace observed removal authority")
	}
	if _, err := namespace.WriterPath(); err == nil {
		t.Fatal("zero namespace supplied a writer path")
	}
	if _, present := (QualifiedRoots{}).Manifest(); present {
		t.Fatal("zero roots supplied a manifest grant")
	}
	if value, err := (RootGrant{}).ObserveRoute(t.Context(), nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/settings.json").AggregateRoot(), budget); err == nil || value.Validate() == nil {
		t.Fatal("zero grant supplied a route observation")
	}
	if _, err := NewWriterPin(rootedpath.AuthorityProvenance{}, rootedpath.RelativeDestination{}); err == nil {
		t.Fatal("zero writer pin was admitted")
	}
	reader := &nativeNamespaceReader{RootedReader: journalTestFilesystem()}
	if value, err := Reobserve(t.Context(), nil, Roots{}, fixtureProtection(t), reader, budget, WriterPin{}); err == nil || value.Observation().Validate() == nil || len(reader.capabilities) != 0 {
		t.Fatal("zero pin reached namespace observation")
	}
}
