package pipackage_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	observepipackage "github.com/isty2e/daem/internal/assurance/observe/pipackage"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/realization/aggregate"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestLoadedPiOrderMatchesLiveObservationAndKeepsContextThroughVerification(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			root := t.TempDir()
			commandRoot, home := filepath.Join(root, "project"), filepath.Join(root, "captured-home")
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			settingsBase := filepath.Join(commandRoot, ".pi")
			if scope == target.ScopeGlobal {
				settingsBase = filepath.Join(root, "agent")
			}
			storedA, err := filepath.Rel(settingsBase, filepath.Join(commandRoot, "packages", "a"))
			if err != nil {
				t.Fatal(err)
			}
			storedB, err := filepath.Rel(settingsBase, filepath.Join(home, "b"))
			if err != nil {
				t.Fatal(err)
			}
			content := []byte(`{"packages":[ {"source":` + quoted(storedB) + `,"opaque":{"enabled":true}}, "npm:foreign@1", ` + quoted(storedA) + ` ],"theme":"day"}`)
			loaded, liveInput := loadedOrderFixture(t, scope, settingsBase, commandRoot, home,
				[]orderSpec{{id: "a", source: "./packages/a"}, {id: "b", source: "~/b"}}, aggregate.ExistingDocument(content))
			writeSettingsBytes(t, filepath.Join(settingsBase, "settings.json"), content)
			live, err := observepipackage.ReadOrder(liveInput)
			if err != nil {
				t.Fatal(err)
			}
			observation, err := observepipackage.ReconstructOrder(loaded)
			if err != nil {
				t.Fatal(err)
			}
			candidate, exists := observation.Candidate()
			liveCandidate, liveExists := live.Candidate()
			if !exists || !liveExists || !observation.Changed() || !bytes.Equal(candidate, liveCandidate) ||
				!observation.Sequence().SameOrdering(live.Sequence()) || observation.Sequence().Revision() != live.Sequence().Revision() ||
				!observation.ExpectedSequence().SameOrdering(live.ExpectedSequence()) || observation.ExpectedSequence().Revision() != live.ExpectedSequence().Revision() ||
				observation.SettingsPath() != live.SettingsPath() {
				t.Fatal("loaded reconstruction differs from the actual live fixed-slot observation")
			}
			if err := observation.VerifyBaseline(content, true); err != nil {
				t.Fatal(err)
			}
			for _, liveHome := range []string{filepath.Join(root, "changed-home"), ""} {
				t.Setenv("HOME", liveHome)
				t.Setenv("USERPROFILE", liveHome)
				rebuilt, err := observepipackage.ReconstructOrder(loaded)
				if err != nil {
					t.Fatalf("reconstruction used ambient home: %v", err)
				}
				rebuiltCandidate, _ := rebuilt.Candidate()
				if !bytes.Equal(rebuiltCandidate, candidate) {
					t.Fatal("reconstructed candidate changed with ambient home")
				}
				if err := observation.Validate(); err != nil {
					t.Fatalf("canonical validation lost selected context: %v", err)
				}
				if err := observation.VerifyPostContent(candidate, true); err != nil {
					t.Fatalf("whole post-observation lost selected context: %v", err)
				}
				combined := bytes.Replace(candidate, []byte(`"theme":"day"`), []byte(`"theme":"night"`), 1)
				if bytes.Equal(combined, candidate) {
					t.Fatal("fixture did not change the independent native root value")
				}
				if err := observation.VerifyPackageContent(combined, true); err != nil {
					t.Fatalf("package-only post-observation lost context or consumed another root value: %v", err)
				}
				if err := observation.VerifyPostContent(combined, true); err == nil {
					t.Fatal("whole candidate verification accepted an independent native edit")
				}
			}
			loaded.Relations[0] = observepipackage.ScopedRelation{}
			if err := observation.Validate(); err != nil {
				t.Fatal("caller relation-slice mutation changed immutable observation")
			}
		})
	}
}

func TestLoadedPiOrderPreservesDocumentAndPackagePresenceWithoutReadingAFile(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	for _, test := range []struct {
		name     string
		document aggregate.Document
		array    bool
	}{
		{"absent document", aggregate.AbsentDocument(), false},
		{"absent array", aggregate.ExistingDocument([]byte(`{"theme":"day"}`)), false},
		{"empty array", aggregate.ExistingDocument([]byte(`{"packages":[],"theme":"day"}`)), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			input, _ := loadedOrderFixture(t, target.ScopeProject, filepath.Join(root, "never-created", ".pi"), root, home,
				[]orderSpec{{id: "a", source: "npm:a@1"}, {id: "b", source: "npm:b@1"}}, test.document)
			t.Setenv("HOME", "")
			t.Setenv("USERPROFILE", "")
			observation, err := observepipackage.ReconstructOrder(input)
			if err != nil {
				t.Fatal(err)
			}
			candidate, exists := observation.Candidate()
			if exists != test.document.Exists() || !bytes.Equal(candidate, test.document.Content()) || observation.Changed() {
				t.Fatal("missing/empty order reconstruction created or rewrote a document")
			}
			if err := observation.Validate(); err != nil {
				t.Fatal(err)
			}
			if err := observation.VerifyPackageContent([]byte(`{"theme":"new"}`), true); (err == nil) == test.array {
				t.Fatalf("package absence and empty array were collapsed: %v", err)
			}
		})
	}
}

func TestLoadedPiOrderRejectsAmbientIncompleteAndUnboundedInput(t *testing.T) {
	root := t.TempDir()
	input, live := loadedOrderFixture(t, target.ScopeProject, filepath.Join(root, ".pi"), root, root,
		[]orderSpec{{id: "a", source: "npm:a@1"}, {id: "b", source: "npm:b@1"}}, aggregate.ExistingDocument([]byte(`{"packages":["npm:b@1","npm:a@1"]}`)))
	badContext := input
	badContext.SettingsContext = extensiontopology.LocalSourceContext{}
	liveRelation := input
	liveRelation.Relations = live.Relations
	wrongScope := input
	wrongScope.Scope = target.ScopeGlobal
	for _, bad := range []observepipackage.LoadedOrderInput{badContext, liveRelation, wrongScope} {
		if observation, err := observepipackage.ReconstructOrder(bad); err == nil || observation.Validate() == nil {
			t.Fatal("incomplete or ambient input returned a valid observation")
		}
	}
	for _, content := range []string{
		`{"packages":["npm:a@1","npm:a@2"]}`, `{"packages":[],"packages":[]}`, `[]`,
		`{"packages":["npm:a@1",]}`, `{"packages":[]}//comment`,
		`{"deep":` + strings.Repeat("[", 33) + "0" + strings.Repeat("]", 33) + "}",
		`{"packages":[],"padding":"` + strings.Repeat("x", observepipackage.MaximumSettingsBytes) + `"}`,
	} {
		bad := input
		bad.Document = aggregate.ExistingDocument([]byte(content))
		if observation, err := observepipackage.ReconstructOrder(bad); err == nil || observation.Validate() == nil {
			t.Fatal("invalid or oversized loaded settings returned a valid observation")
		}
	}
	observation, err := observepipackage.ReconstructOrder(input)
	if err != nil {
		t.Fatal(err)
	}
	oversized := []byte(`{"packages":["npm:a@1","npm:b@1"],"padding":"` + strings.Repeat("x", observepipackage.MaximumSettingsBytes) + `"}`)
	if err := observation.VerifyPackageContent(oversized, true); err == nil {
		t.Fatal("post-observation bypassed the existing settings byte bound")
	}
	if _, err := observepipackage.NewScopedRelationWithContext(mustCorrelationKey(t, "npm:a@1"), target.ScopeProject, extensiontopology.LocalSourceContext{}); err == nil {
		t.Fatal("zero lexical context became a bound relation")
	}
}

func TestContextBoundPiCorrelationUsesTheSameSelectedCommandContext(t *testing.T) {
	root := t.TempDir()
	commandRoot, settingsBase, home := filepath.Join(root, "project"), filepath.Join(root, "project", ".pi"), filepath.Join(root, "captured-home")
	context, err := extensiontopology.NewLocalSourceContext(commandRoot, home)
	if err != nil {
		t.Fatal(err)
	}
	relation, err := observepipackage.NewScopedRelationWithContext(mustCorrelationKey(t, "~/plugin"), target.ScopeProject, context)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := filepath.Rel(settingsBase, filepath.Join(home, "plugin"))
	if err != nil {
		t.Fatal(err)
	}
	writeSettingsBytes(t, filepath.Join(settingsBase, "settings.json"), []byte(`{"packages":[`+quoted(stored)+`]}`))
	inventory := mustReadSettings(t, observepipackage.SettingsInput{Scope: target.ScopeProject, ProjectRoot: commandRoot})
	t.Setenv("HOME", filepath.Join(root, "different-home"))
	t.Setenv("USERPROFILE", filepath.Join(root, "different-home"))
	result, err := observepipackage.Correlate(relation, inventory)
	if err != nil {
		t.Fatal(err)
	}
	assertCorrelationState(t, result, observerelation.StateExactCorrelation)
}

func loadedOrderFixture(t *testing.T, scope target.Scope, settingsBase, commandRoot, home string, specs []orderSpec, document aggregate.Document) (observepipackage.LoadedOrderInput, observepipackage.OrderInput) {
	t.Helper()
	live := mustOrderInput(t, observepipackage.SettingsInput{
		Scope: scope, ConfigRoot: settingsBase, ProjectRoot: filepath.Dir(settingsBase), WorkDir: commandRoot,
	}, commandRoot, scope, specs)
	settingsContext, err := extensiontopology.NewLocalSourceContext(settingsBase, home)
	if err != nil {
		t.Fatal(err)
	}
	commandContext, err := extensiontopology.NewLocalSourceContext(commandRoot, home)
	if err != nil {
		t.Fatal(err)
	}
	var relations []observepipackage.ScopedRelation
	for _, spec := range specs {
		subject, err := topology.NewSubjectID(topology.SubjectHostRelation, "pi.package-carrier", spec.id)
		if err != nil {
			t.Fatal(err)
		}
		source, err := hostrelation.NewSubjectKey(spec.source)
		if err != nil {
			t.Fatal(err)
		}
		managed, err := hostrelation.NewManagedInstanceKey("host-relation:v1:" + spec.id)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := hostrelation.NewExpectedRelation(source, managed)
		if err != nil {
			t.Fatal(err)
		}
		key, err := observerelation.NewCorrelationKey(subject, expected)
		if err != nil {
			t.Fatal(err)
		}
		relation, err := observepipackage.NewScopedRelationWithContext(key, scope, commandContext)
		if err != nil {
			t.Fatal(err)
		}
		relations = append(relations, relation)
	}
	return observepipackage.LoadedOrderInput{
		Scope: scope, SettingsContext: settingsContext, Document: document, Constraint: live.Constraint, Relations: relations,
	}, live
}
