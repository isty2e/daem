package pipackage_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	observepipackage "github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestPiOrderAndNativeSettingsShareOneCandidateWithoutWeakeningRootChecks(t *testing.T) {
	content := []byte("{\r\n" +
		`  "packages": [ {"source":"npm:b@1","opaque":{"n":1e999}}, "npm:foreign@1", "npm:\u0061@1" ],` + "\r\n" +
		`  "theme": "day", "unknown": {"label":"그대로"}` + "\r\n}")
	observation := readCompositionOrder(t, content, true)
	ordered, exists := observation.Candidate()
	if !exists || !observation.Changed() {
		t.Fatal("fixture did not produce a real package permutation")
	}
	original := aggregate.ExistingDocument(content)
	candidate := composeTheme(t, original, aggregate.ExistingDocument(ordered))
	if err := observation.VerifyPackageContent(candidate.Content(), candidate.Exists()); err != nil {
		t.Fatalf("combined settings lost exact package evidence: %v", err)
	}
	if err := observation.VerifyPostContent(candidate.Content(), candidate.Exists()); err == nil {
		t.Fatal("standalone verification accepted a different whole-document candidate")
	}
	if err := observation.VerifyBaseline(candidate.Content(), candidate.Exists()); err == nil {
		t.Fatal("standalone baseline accepted a different root revision")
	}
	if err := observation.VerifyPostContent(ordered, true); err != nil {
		t.Fatalf("original standalone postcondition changed: %v", err)
	}
	if err := observation.VerifyPackageContent(content, true); err == nil {
		t.Fatal("package-only verification accepted the old order")
	}
	if !bytes.Contains(candidate.Content(), []byte(`"unknown": {"label":"그대로"}`)) {
		t.Fatal("combined edit changed an unselected root value")
	}
}

func TestPackageVerificationRejectsLiteralChangesEvenWhenLoadOrderIsEqual(t *testing.T) {
	content := []byte(`{"packages":["npm:b@1", {"source":"npm:foreign@1","opaque":true}, "npm:\u0061@1"],"theme":"day"}`)
	observation := readCompositionOrder(t, content, true)
	candidate, _ := observation.Candidate()
	for _, test := range []struct{ name, from, to string }{
		{"foreign options", `"opaque":true`, `"opaque":false`},
		{"source spelling", `npm:\u0061@1`, `npm:a@1`},
		{"equivalent source identity", `npm:b@1`, `npm:b@2`},
		{"slot whitespace", `, `, `,`},
		{"new foreign row", `"packages":[`, `"packages":["npm:new@1",`},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := bytes.ReplaceAll(candidate, []byte(test.from), []byte(test.to))
			if bytes.Equal(changed, candidate) {
				t.Fatal("fixture did not change the intended literal package value")
			}
			if err := observation.VerifyPackageContent(changed, true); err == nil {
				t.Fatal("package verification accepted unapproved array bytes")
			}
		})
	}
	for _, invalid := range []string{
		`{}`, `{"packages":[]}`, `{"packages":null}`, `{"packages":{}}`, `[]`,
		`{"packages":[],"packages":[]}`, `{"packages":[/*comment*/"npm:a@1"]}`,
		`{"packages":["npm:a@1",]}`, `{"packages":["npm:a@1"]} {}`,
	} {
		if err := observation.VerifyPackageContent([]byte(invalid), true); err == nil {
			t.Fatalf("package verification accepted %q", invalid)
		}
	}
	tooDeep := bytes.TrimSuffix(candidate, []byte("}"))
	tooDeep = append(tooDeep, []byte(`,"deep":`+strings.Repeat("[", 33)+"0"+strings.Repeat("]", 33)+"}")...)
	if err := observation.VerifyPackageContent(tooDeep, true); err == nil {
		t.Fatal("scoped verification bypassed the Pi document grammar/depth gate")
	}
}

func TestPackageVerificationDistinguishesMissingArrayFromEmptyAndMissingFile(t *testing.T) {
	for _, baseline := range []struct {
		name, content string
		exists, array bool
	}{
		{"missing document", "", false, false},
		{"missing array", `{"theme":"day"}`, true, false},
		{"empty array", `{"packages":[],"theme":"day"}`, true, true},
	} {
		t.Run(baseline.name, func(t *testing.T) {
			observation := readCompositionOrder(t, []byte(baseline.content), baseline.exists)
			for _, post := range []struct {
				content       string
				exists, array bool
			}{
				{"", false, false},
				{`{"theme":"review"}`, true, false},
				{`{"packages":[],"theme":"review"}`, true, true},
			} {
				wantEqual := baseline.array == post.array
				err := observation.VerifyPackageContent([]byte(post.content), post.exists)
				if (err == nil) != wantEqual {
					t.Fatalf("post=%q exists=%t: err=%v, equal=%t", post.content, post.exists, err, wantEqual)
				}
			}
			if err := observation.VerifyPackageContent([]byte(`{}`), false); err == nil {
				t.Fatal("absent-file evidence carried document bytes")
			}
			if !baseline.exists {
				candidate := composeTheme(t, aggregate.AbsentDocument(), aggregate.AbsentDocument())
				if err := observation.VerifyPackageContent(candidate.Content(), true); err != nil {
					t.Fatalf("native-only creation fabricated package presence: %v", err)
				}
				if err := observation.VerifyPostContent(candidate.Content(), true); err == nil {
					t.Fatal("standalone non-creating no-op accepted a created root")
				}
			}
		})
	}
	var zero observepipackage.OrderObservation
	if err := zero.VerifyPackageContent(nil, false); err == nil {
		t.Fatal("uninitialized order observation became valid scoped evidence")
	}
}

func readCompositionOrder(t *testing.T, content []byte, exists bool) observepipackage.OrderObservation {
	t.Helper()
	root := t.TempDir()
	if exists {
		writeSettingsBytes(t, filepath.Join(root, ".pi", "settings.json"), content)
	}
	input := mustOrderInput(t, observepipackage.SettingsInput{ProjectRoot: root, Scope: target.ScopeProject}, root, target.ScopeProject,
		[]orderSpec{{id: "a", source: "npm:a@1"}, {id: "b", source: "npm:b@1"}})
	observation, err := observepipackage.ReadOrder(input)
	if err != nil {
		t.Fatal(err)
	}
	return observation
}

func composeTheme(t *testing.T, original, ordered aggregate.Document) aggregate.Document {
	t.Helper()
	limits := configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}
	codec, err := nativecodec.New(t.Context(), "native-json-v1", limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	key, err := output.NewKeyPath("theme")
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse(".pi/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeProject, destination, footprint, codec.ContractID())
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := configdocument.Parse(t.Context(), []byte(`{"theme":"review"}`), configdocument.JSON, limits)
	if err != nil {
		t.Fatal(err)
	}
	value, _, err := parsed.Lookup(key)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := value.CanonicalContext(t.Context(), limits)
	if err != nil {
		t.Fatal(err)
	}
	body, err := aggregate.NewManagedContribution(contract, canonical)
	if err != nil {
		t.Fatal(err)
	}
	id, err := entity.New(entity.KindHostConfig, "review-theme")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.HostConfigKeySubject(id, target.TargetPi, target.ScopeProject, key)
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := aggregate.NewSubjectContribution(subject, body)
	if err != nil {
		t.Fatal(err)
	}
	set, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{contribution})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	before, failure := codec.Read(original, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	state, present := before.State(contract)
	if !present {
		t.Fatal("native theme baseline is incomplete")
	}
	intent, err := aggregate.NewProjectionIntent(state, &set)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := aggregate.NewPlan(before, []aggregate.ProjectionIntent{intent})
	if err != nil {
		t.Fatal(err)
	}
	edit, err := compose.NewProjectionEdit(codec, plan, configdocument.JSON, []output.KeyPath{key})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := compose.Render(t.Context(), ordered, limits, []compose.ProjectionEdit{edit})
	if err != nil {
		t.Fatal(err)
	}
	after, failure := codec.Read(candidate.Document(), selection)
	if failure != nil || len(candidate.Expected()) != 1 || !after.Equal(candidate.Expected()[0]) {
		t.Fatalf("combined candidate lacks actual native postcondition evidence: %v", failure)
	}
	actual, present := after.State(contract)
	if !present || !actual.Present() || actual.CanonicalProjection() != canonical {
		t.Fatal("combined candidate did not set the selected native theme")
	}
	return candidate.Document()
}
