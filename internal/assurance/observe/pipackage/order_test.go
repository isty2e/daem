package pipackage_test

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	observepipackage "github.com/isty2e/daem/internal/assurance/observe/pipackage"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

type orderSpec struct {
	id     string
	source string
}

func TestReadOrderBuildsLosslessFixedSlotCandidateAndRiskFacts(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".pi", "settings.json")
	content := []byte("{\r\n" +
		"  \"packages\": [\r\n" +
		"    { \"source\": \"npm:b@1\", \"autoload\": false },\r\n" +
		"    \"npm:foreign@1\",\r\n" +
		"    \"npm:a@1\"\r\n" +
		"  ],\r\n" +
		"  \"label\": \"그대로\"\r\n" +
		"}\r\n")
	writeSettingsBytes(t, path, content)
	input := mustOrderInput(
		t,
		observepipackage.SettingsInput{
			ProjectRoot: root,
			Scope:       target.ScopeProject,
		},
		root,
		target.ScopeProject,
		[]orderSpec{{id: "a", source: "npm:a@1"}, {id: "b", source: "npm:b@1"}},
	)

	observation, err := observepipackage.ReadOrder(input)
	if err != nil {
		t.Fatalf("ReadOrder: %v", err)
	}
	if err := observation.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !observation.Changed() {
		t.Fatal("ReadOrder reported no order change")
	}
	candidate, exists := observation.Candidate()
	if !exists {
		t.Fatal("candidate lost existing settings evidence")
	}
	want := []byte("{\r\n" +
		"  \"packages\": [\r\n" +
		"    \"npm:a@1\",\r\n" +
		"    \"npm:foreign@1\",\r\n" +
		"    { \"source\": \"npm:b@1\", \"autoload\": false }\r\n" +
		"  ],\r\n" +
		"  \"label\": \"그대로\"\r\n" +
		"}\r\n")
	if !slices.Equal(candidate, want) {
		t.Fatalf("candidate = %q, want %q", candidate, want)
	}
	if err := observation.VerifyBaseline(content, true); err != nil {
		t.Fatalf("VerifyBaseline: %v", err)
	}
	if err := observation.VerifyPostContent(candidate, true); err != nil {
		t.Fatalf("VerifyPostContent: %v", err)
	}
	if err := observation.VerifyPostContent(content, true); err == nil {
		t.Fatal("VerifyPostContent accepted the stale pre-mutation order")
	}

	changes := observation.PrecedenceChanges()
	if len(changes) != 2 {
		t.Fatalf("precedence changes = %#v, want 2", changes)
	}
	if changes[0].ManagedWasBefore() == changes[0].ManagedWillBeBefore() ||
		changes[1].ManagedWasBefore() == changes[1].ManagedWillBeBefore() ||
		changes[0].ForeignIdentity() != "npm:foreign" ||
		changes[1].ForeignIdentity() != "npm:foreign" {
		t.Fatalf("precedence changes = %#v", changes)
	}
}

func TestReadOrderTreatsMissingSettingsAsNonCreatingEmptyEvidence(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			root := t.TempDir()
			settings := observepipackage.SettingsInput{
				ConfigRoot:  filepath.Join(root, "agent"),
				WorkDir:     root,
				ProjectRoot: filepath.Join(root, "project"),
				Scope:       scope,
			}
			input := mustOrderInput(
				t,
				settings,
				root,
				scope,
				[]orderSpec{{id: "a", source: "npm:a@1"}},
			)
			observation, err := observepipackage.ReadOrder(input)
			if err != nil {
				t.Fatalf("ReadOrder: %v", err)
			}
			candidate, exists := observation.Candidate()
			if observation.Changed() || exists || len(candidate) != 0 ||
				len(observation.Sequence().OrderedRows()) != 0 {
				t.Fatalf(
					"missing observation = changed:%t exists:%t candidate:%q rows:%d",
					observation.Changed(),
					exists,
					candidate,
					len(observation.Sequence().OrderedRows()),
				)
			}
			if err := observation.VerifyBaseline(nil, false); err != nil {
				t.Fatalf("VerifyBaseline: %v", err)
			}
			if err := observation.VerifyPostContent(nil, false); err != nil {
				t.Fatalf("VerifyPostContent: %v", err)
			}
			if err := observation.VerifyBaseline([]byte(`{}`), true); err == nil {
				t.Fatal("VerifyBaseline accepted concurrent file creation")
			}
		})
	}
}

func TestReadOrderRejectsDuplicateAndEquivalentLoadIdentities(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "duplicate exact", content: `{"packages":["npm:a@1","npm:a@1"]}`},
		{name: "exact plus equivalent", content: `{"packages":["npm:a@1","npm:a@2"]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeSettings(t, filepath.Join(root, ".pi", "settings.json"), test.content)
			input := mustOrderInput(
				t,
				observepipackage.SettingsInput{
					ProjectRoot: root,
					Scope:       target.ScopeProject,
				},
				root,
				target.ScopeProject,
				[]orderSpec{{id: "a", source: "npm:a@1"}},
			)
			_, err := observepipackage.ReadOrder(input)
			if err == nil || !strings.Contains(err.Error(), "appears more than once") {
				t.Fatalf("ReadOrder error = %v", err)
			}
		})
	}
}

func TestReadOrderSupportsNPMGitAndLocalObjectRows(t *testing.T) {
	root := t.TempDir()
	localSource := filepath.Join(root, "plugins", "local")
	storedLocal, err := filepath.Rel(filepath.Join(root, ".pi"), localSource)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".pi", "settings.json")
	writeSettings(t, path, `{
  "packages": [
    "npm:@acme/tool@1",
    {"source":"github:acme/tool#v1","autoload":true},
    `+quoted(storedLocal)+`
  ]
}`)
	input := mustOrderInput(
		t,
		observepipackage.SettingsInput{
			ProjectRoot: root,
			Scope:       target.ScopeProject,
		},
		root,
		target.ScopeProject,
		[]orderSpec{
			{id: "local", source: localSource},
			{id: "git", source: "github:acme/tool#v1"},
			{id: "npm", source: "npm:@acme/tool@1"},
		},
	)

	observation, err := observepipackage.ReadOrder(input)
	if err != nil {
		t.Fatalf("ReadOrder: %v", err)
	}
	candidate, _ := observation.Candidate()
	want := `{
  "packages": [
    ` + quoted(storedLocal) + `,
    {"source":"github:acme/tool#v1","autoload":true},
    "npm:@acme/tool@1"
  ]
}`
	if string(candidate) != want {
		t.Fatalf("candidate = %q, want %q", candidate, want)
	}
	if len(observation.ExpectedSequence().OrderedRows()) != 3 {
		t.Fatalf("expected rows = %#v", observation.ExpectedSequence().OrderedRows())
	}
}

func TestReadOrderProjectsConstraintToOnlyPresentManagedMembers(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".pi", "settings.json")
	writeSettings(t, path, `{"packages":["npm:c@1","npm:foreign@1","npm:a@1"]}`)
	input := mustOrderInput(
		t,
		observepipackage.SettingsInput{
			ProjectRoot: root,
			Scope:       target.ScopeProject,
		},
		root,
		target.ScopeProject,
		[]orderSpec{
			{id: "a", source: "npm:a@1"},
			{id: "b", source: "npm:b@1"},
			{id: "c", source: "npm:c@1"},
		},
	)

	observation, err := observepipackage.ReadOrder(input)
	if err != nil {
		t.Fatalf("ReadOrder: %v", err)
	}
	candidate, _ := observation.Candidate()
	if got := decodePackageStrings(t, candidate); !slices.Equal(
		got,
		[]string{"npm:a@1", "npm:foreign@1", "npm:c@1"},
	) {
		t.Fatalf("projected packages = %v", got)
	}
}

func TestReadOrderKeepsZeroAndSingleManagedSequencesStable(t *testing.T) {
	tests := []struct {
		name     string
		packages string
	}{
		{name: "empty", packages: `[]`},
		{name: "one managed among foreign", packages: `["npm:foreign@1","npm:a@1"]`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			content := `{"packages":` + test.packages + `,"retained":-0}`
			writeSettings(t, filepath.Join(root, ".pi", "settings.json"), content)
			input := mustOrderInput(
				t,
				observepipackage.SettingsInput{
					ProjectRoot: root,
					Scope:       target.ScopeProject,
				},
				root,
				target.ScopeProject,
				[]orderSpec{{id: "a", source: "npm:a@1"}},
			)

			observation, err := observepipackage.ReadOrder(input)
			if err != nil {
				t.Fatalf("ReadOrder: %v", err)
			}
			candidate, _ := observation.Candidate()
			if observation.Changed() || string(candidate) != content ||
				len(observation.PrecedenceChanges()) != 0 {
				t.Fatalf(
					"stable order = changed:%t candidate:%q risks:%v",
					observation.Changed(),
					candidate,
					observation.PrecedenceChanges(),
				)
			}
		})
	}
}

func TestReadOrderRejectsIncoherentConstraintAndRelationSelection(t *testing.T) {
	root := t.TempDir()
	writeSettings(t, filepath.Join(root, ".pi", "settings.json"), `{"packages":["npm:a@1"]}`)
	valid := mustOrderInput(
		t,
		observepipackage.SettingsInput{
			ProjectRoot: root,
			Scope:       target.ScopeProject,
		},
		root,
		target.ScopeProject,
		[]orderSpec{{id: "a", source: "npm:a@1"}},
	)
	other := mustOrderInput(
		t,
		valid.Settings,
		root,
		target.ScopeProject,
		[]orderSpec{{id: "b", source: "npm:b@1"}},
	)

	missing := valid
	missing.Relations = nil
	duplicate := valid
	duplicate.Relations = append(
		append([]observepipackage.ScopedRelation(nil), valid.Relations...),
		valid.Relations[0],
	)
	extra := valid
	extra.Relations = append(
		append([]observepipackage.ScopedRelation(nil), valid.Relations...),
		other.Relations[0],
	)
	mismatched := valid
	wrongIdentity, err := hostrelation.NewHostLoadIdentity("npm:other")
	if err != nil {
		t.Fatal(err)
	}
	wrongMember, err := hostrelation.NewRelationOrderMember(
		valid.Constraint.Members()[0].Subject(),
		wrongIdentity,
	)
	if err != nil {
		t.Fatal(err)
	}
	mismatched.Constraint, err = hostrelation.NewRelationOrderConstraint(
		valid.Constraint.ClassID(),
		valid.Constraint.MemberIdentityContract(),
		valid.Constraint.RuntimeMeaning(),
		[]hostrelation.RelationOrderMember{wrongMember},
	)
	if err != nil {
		t.Fatal(err)
	}
	wrongClass := valid
	classID, err := hostrelation.NewOrderClassID("extension:pi:forged:packages")
	if err != nil {
		t.Fatal(err)
	}
	wrongClass.Constraint, err = hostrelation.NewRelationOrderConstraint(
		classID,
		valid.Constraint.MemberIdentityContract(),
		valid.Constraint.RuntimeMeaning(),
		valid.Constraint.Members(),
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name  string
		input observepipackage.OrderInput
	}{
		{name: "missing relation", input: missing},
		{name: "duplicate relation", input: duplicate},
		{name: "extra relation", input: extra},
		{name: "mismatched load identity", input: mismatched},
		{name: "wrong profile class", input: wrongClass},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := observepipackage.ReadOrder(test.input); err == nil {
				t.Fatal("ReadOrder accepted incoherent order input")
			}
		})
	}
}

func TestReadOrderExhaustiveSmallSequencesPreserveFixedSlotsAndIdempotency(t *testing.T) {
	const (
		a = "npm:a@1"
		b = "npm:b@1"
		x = "npm:x@1"
		y = "npm:y@1"
	)
	root := t.TempDir()
	settingsPath := filepath.Join(root, ".pi", "settings.json")
	for _, physical := range permutations([]string{a, b, x, y}) {
		for _, desired := range [][]orderSpec{
			{{id: "a", source: a}, {id: "b", source: b}},
			{{id: "b", source: b}, {id: "a", source: a}},
		} {
			content, err := json.Marshal(map[string]any{
				"packages": physical,
				"unknown":  []string{"retained"},
			})
			if err != nil {
				t.Fatal(err)
			}
			writeSettingsBytes(t, settingsPath, content)
			input := mustOrderInput(
				t,
				observepipackage.SettingsInput{
					ProjectRoot: root,
					Scope:       target.ScopeProject,
				},
				root,
				target.ScopeProject,
				desired,
			)
			observation, err := observepipackage.ReadOrder(input)
			if err != nil {
				t.Fatalf("ReadOrder physical=%v desired=%v: %v", physical, desired, err)
			}
			candidate, _ := observation.Candidate()
			candidatePackages := decodePackageStrings(t, candidate)
			for index, source := range physical {
				if source == x || source == y {
					if candidatePackages[index] != source {
						t.Fatalf(
							"foreign row moved: physical=%v candidate=%v index=%d",
							physical,
							candidatePackages,
							index,
						)
					}
				}
			}
			managed := make([]string, 0, 2)
			for _, source := range candidatePackages {
				if source == a || source == b {
					managed = append(managed, source)
				}
			}
			if want := []string{desired[0].source, desired[1].source}; !slices.Equal(managed, want) {
				t.Fatalf("managed order = %v, want %v", managed, want)
			}

			firstRisks := precedenceKeys(observation.PrecedenceChanges())
			sameBaseline, err := observepipackage.ReadOrder(input)
			if err != nil {
				t.Fatalf("same-baseline ReadOrder: %v", err)
			}
			if sameRisks := precedenceKeys(sameBaseline.PrecedenceChanges()); !slices.Equal(
				sameRisks,
				firstRisks,
			) {
				t.Fatalf("risk ordering = %v, want %v", sameRisks, firstRisks)
			}
			writeSettingsBytes(t, settingsPath, candidate)
			repeated, err := observepipackage.ReadOrder(input)
			if err != nil {
				t.Fatalf("repeated ReadOrder: %v", err)
			}
			if repeated.Changed() {
				t.Fatalf("candidate is not idempotent: %v", candidatePackages)
			}
			if len(repeated.PrecedenceChanges()) != 0 {
				t.Fatalf("converged order retained risk facts: %v", firstRisks)
			}
		}
	}
}

func mustOrderInput(
	t *testing.T,
	settings observepipackage.SettingsInput,
	commandRoot string,
	scope target.Scope,
	specs []orderSpec,
) observepipackage.OrderInput {
	t.Helper()
	capability, admitted := profile.Profile(target.TargetPi).ExtensionOrder(
		desiredextension.CarrierPiPackage,
		scope,
	)
	if !admitted {
		t.Fatalf("Pi %s order capability is absent", scope)
	}
	members := make([]hostrelation.RelationOrderMember, 0, len(specs))
	relations := make([]observepipackage.ScopedRelation, 0, len(specs))
	for _, spec := range specs {
		subject, err := topology.NewSubjectID(
			topology.SubjectHostRelation,
			"pi.package-carrier",
			spec.id,
		)
		if err != nil {
			t.Fatal(err)
		}
		subjectKey, err := hostrelation.NewSubjectKey(spec.source)
		if err != nil {
			t.Fatal(err)
		}
		managedKey, err := hostrelation.NewManagedInstanceKey("host-relation:v1:" + spec.id)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := hostrelation.NewExpectedRelation(subjectKey, managedKey)
		if err != nil {
			t.Fatal(err)
		}
		key, err := observerelation.NewCorrelationKey(subject, expected)
		if err != nil {
			t.Fatal(err)
		}
		relation, err := observepipackage.NewScopedRelation(key, scope, commandRoot)
		if err != nil {
			t.Fatal(err)
		}
		identity, err := observepipackage.HostLoadIdentityForInput(
			spec.source,
			commandRoot,
			scope,
		)
		if err != nil {
			t.Fatal(err)
		}
		loadIdentity, err := hostrelation.NewHostLoadIdentity(identity)
		if err != nil {
			t.Fatal(err)
		}
		member, err := hostrelation.NewRelationOrderMember(subject, loadIdentity)
		if err != nil {
			t.Fatal(err)
		}
		relations = append(relations, relation)
		members = append(members, member)
	}
	constraint, err := hostrelation.NewRelationOrderConstraint(
		capability.ClassID(),
		capability.MemberIdentityContract(),
		capability.RuntimeMeaning(),
		members,
	)
	if err != nil {
		t.Fatal(err)
	}
	return observepipackage.OrderInput{
		Settings:   settings,
		Constraint: constraint,
		Relations:  relations,
	}
}

func decodePackageStrings(t *testing.T, content []byte) []string {
	t.Helper()
	var document struct {
		Packages []string `json:"packages"`
	}
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	return document.Packages
}

func precedenceKeys(changes []observepipackage.PrecedenceChange) []string {
	result := make([]string, 0, len(changes))
	for _, change := range changes {
		result = append(
			result,
			change.ManagedSubject().String()+"|"+
				string(change.ForeignIdentity())+"|"+
				string(rune('0'+boolIndex(change.ManagedWasBefore())))+"|"+
				string(rune('0'+boolIndex(change.ManagedWillBeBefore()))),
		)
	}
	return result
}

func boolIndex(value bool) int {
	if value {
		return 1
	}
	return 0
}

func permutations(values []string) [][]string {
	if len(values) == 0 {
		return [][]string{{}}
	}
	result := make([][]string, 0)
	for index, value := range values {
		rest := append([]string(nil), values[:index]...)
		rest = append(rest, values[index+1:]...)
		for _, suffix := range permutations(rest) {
			result = append(result, append([]string{value}, suffix...))
		}
	}
	return result
}
