package opencodeplugin_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	observeopencode "github.com/isty2e/daem/internal/assurance/observe/opencodeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	opencodeconfig "github.com/isty2e/daem/internal/realization/configrelation/opencode"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestReadOrderConvergesServerAndTUIIndependently(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			input, directory := openCodeOrderInventory(t, scope)
			writeOpenCodeOrderDocument(t, filepath.Join(directory, "opencode.jsonc"), `{
  // retained server sibling
  "plugin": [
    ["beta@2", {"server": true}],
    "foreign-server@1",
    "alpha@1",
  ],
  "theme": "dark",
}
`)
			writeOpenCodeOrderDocument(t, filepath.Join(directory, "tui.json"), `{
  "plugin": ["alpha@1", "foreign-tui@1", ["beta@2", {"tui": true}]],
  "keybinds": {"x": "y"}
}
`)

			observation := mustOpenCodeOrderObservation(
				t,
				input,
				directory,
				scope,
				[]openCodeOrderSpec{
					{id: "alpha", source: "alpha@1"},
					{id: "beta", source: "beta@2"},
				},
			)
			documents := observation.Documents()
			if len(documents) != 2 ||
				documents[0].Kind() != opencodeconfig.ConfigServer ||
				documents[1].Kind() != opencodeconfig.ConfigTUI {
				t.Fatalf("documents = %#v", documents)
			}
			if !documents[0].Changed() || documents[1].Changed() {
				t.Fatalf(
					"changed = server:%t tui:%t, want true/false",
					documents[0].Changed(),
					documents[1].Changed(),
				)
			}
			if documents[0].Sequence().SequenceID() == documents[1].Sequence().SequenceID() {
				t.Fatal("server and TUI physical sequence identities collapsed")
			}
			if documents[0].Sequence().ClassID() != documents[1].Sequence().ClassID() {
				t.Fatal("server and TUI lost their shared logical order class")
			}
			serverCandidate, _ := documents[0].Candidate()
			for _, retained := range []string{
				"// retained server sibling",
				`["beta@2", {"server": true}]`,
				`"theme": "dark"`,
			} {
				if !strings.Contains(string(serverCandidate), retained) {
					t.Fatalf("server candidate lost %q:\n%s", retained, serverCandidate)
				}
			}
			serverEntries, err := opencodeconfig.ParseAt(
				serverCandidate,
				documents[0].Path(),
			)
			if err != nil {
				t.Fatalf("ParseAt(server candidate): %v", err)
			}
			gotSources := entrySources(serverEntries.Entries())
			wantSources := []string{"alpha@1", "foreign-server@1", "beta@2"}
			if !slices.Equal(gotSources, wantSources) {
				t.Fatalf("server sources = %v, want %v", gotSources, wantSources)
			}
		})
	}
}

func TestReadOrderConvergesJSONAndJSONCAsIndependentSequences(t *testing.T) {
	input, directory := openCodeOrderInventory(t, target.ScopeProject)
	writeOpenCodeOrderDocument(
		t,
		filepath.Join(directory, "opencode.json"),
		`{"plugin":["alpha@1","beta@1"],"variant":"json"}`,
	)
	writeOpenCodeOrderDocument(
		t,
		filepath.Join(directory, "opencode.jsonc"),
		`{"plugin":["beta@1","foreign@1","alpha@1"],"variant":"jsonc"}`,
	)

	observation := mustOpenCodeOrderObservation(
		t,
		input,
		directory,
		target.ScopeProject,
		[]openCodeOrderSpec{
			{id: "alpha", source: "alpha@1"},
			{id: "beta", source: "beta@1"},
		},
	)
	documents := observation.Documents()
	if len(documents) != 3 ||
		documents[0].Sequence().SequenceID() != "opencode:project:server.json.plugins" ||
		documents[1].Sequence().SequenceID() != "opencode:project:server.jsonc.plugins" ||
		documents[2].Sequence().SequenceID() != "opencode:project:tui.json.plugins" {
		t.Fatalf("documents = %#v", documents)
	}
	if documents[0].Changed() || !documents[1].Changed() || documents[2].Changed() {
		t.Fatalf(
			"changed = json:%t jsonc:%t tui:%t",
			documents[0].Changed(),
			documents[1].Changed(),
			documents[2].Changed(),
		)
	}
	candidate, exists := documents[1].Candidate()
	if !exists {
		t.Fatal("JSONC candidate lost existence")
	}
	parsed, err := opencodeconfig.ParseAt(candidate, documents[1].Path())
	if err != nil {
		t.Fatal(err)
	}
	if got := entrySources(parsed.Entries()); !slices.Equal(
		got,
		[]string{"alpha@1", "foreign@1", "beta@1"},
	) {
		t.Fatalf("JSONC sources = %v", got)
	}
}

func TestReadOrderTreatsMissingOrAbsentPluginDocumentsAsEmptyNoops(t *testing.T) {
	input, directory := openCodeOrderInventory(t, target.ScopeProject)
	writeOpenCodeOrderDocument(
		t,
		filepath.Join(directory, "opencode.json"),
		`{"theme":"dark"}`,
	)
	observation := mustOpenCodeOrderObservation(
		t,
		input,
		directory,
		target.ScopeProject,
		[]openCodeOrderSpec{
			{id: "alpha", source: "alpha@1"},
			{id: "beta", source: "beta@1"},
		},
	)
	documents := observation.Documents()
	for _, document := range documents {
		if document.Changed() || len(document.Sequence().OrderedRows()) != 0 {
			t.Fatalf(
				"%s missing/absent document = changed:%t rows:%d",
				document.Kind(),
				document.Changed(),
				len(document.Sequence().OrderedRows()),
			)
		}
	}
	serverCandidate, serverExists := documents[0].Candidate()
	if !serverExists || string(serverCandidate) != `{"theme":"dark"}` {
		t.Fatalf("server candidate = %q exists:%t", serverCandidate, serverExists)
	}
	tuiCandidate, tuiExists := documents[1].Candidate()
	if tuiExists || len(tuiCandidate) != 0 {
		t.Fatalf("TUI candidate = %q exists:%t", tuiCandidate, tuiExists)
	}
}

func TestReadOrderConvergesTUIOnlyWithoutInsertingServerRows(t *testing.T) {
	input, directory := openCodeOrderInventory(t, target.ScopeProject)
	path := filepath.Join(directory, "tui.json")
	writeOpenCodeOrderDocument(
		t,
		path,
		`{"plugin":["beta@1","foreign@1","alpha@1"],"retained":true}`,
	)
	observation := mustOpenCodeOrderObservation(
		t,
		input,
		directory,
		target.ScopeProject,
		[]openCodeOrderSpec{
			{id: "alpha", source: "alpha@1"},
			{id: "beta", source: "beta@1"},
		},
	)
	documents := observation.Documents()
	if documents[0].Changed() || !documents[1].Changed() {
		t.Fatalf(
			"changed = server:%t tui:%t, want false/true",
			documents[0].Changed(),
			documents[1].Changed(),
		)
	}
	serverCandidate, serverExists := documents[0].Candidate()
	if serverExists || len(serverCandidate) != 0 {
		t.Fatalf("server candidate = %q exists:%t", serverCandidate, serverExists)
	}
	tuiCandidate, _ := documents[1].Candidate()
	parsed, err := opencodeconfig.ParseAt(tuiCandidate, path)
	if err != nil {
		t.Fatal(err)
	}
	if got := entrySources(parsed.Entries()); !slices.Equal(
		got,
		[]string{"alpha@1", "foreign@1", "beta@1"},
	) {
		t.Fatalf("TUI sources = %v", got)
	}
}

func TestReadOrderRejectsDuplicateExactAndEquivalentLoadIdentities(t *testing.T) {
	tests := map[string]struct {
		content string
		want    string
	}{
		"duplicate exact": {
			content: `{"plugin":["alpha@1",["alpha@1",{}],"beta@1"]}`,
			want:    "2 exact plugin rows",
		},
		"equivalent spelling": {
			content: `{"plugin":["alpha@1","alpha@2","beta@1"]}`,
			want:    "host load identity",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			input, directory := openCodeOrderInventory(t, target.ScopeProject)
			writeOpenCodeOrderDocument(
				t,
				filepath.Join(directory, "opencode.json"),
				test.content,
			)
			_, err := openCodeOrderObservation(
				input,
				directory,
				target.ScopeProject,
				[]openCodeOrderSpec{
					{id: "alpha", source: "alpha@1"},
					{id: "beta", source: "beta@1"},
				},
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ReadOrder error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestReadOrderFixedSlotProjectionIsExhaustiveForSmallSequences(t *testing.T) {
	values := []string{"alpha@1", "beta@1", "foreign-one@1", "foreign-two@1"}
	for index, permutation := range permutations(values) {
		t.Run(fmt.Sprintf("permutation-%02d", index), func(t *testing.T) {
			input, directory := openCodeOrderInventory(t, target.ScopeProject)
			path := filepath.Join(directory, "opencode.json")
			writeOpenCodeOrderDocument(
				t,
				path,
				`{"plugin":["`+strings.Join(permutation, `","`)+`"]}`,
			)
			observation := mustOpenCodeOrderObservation(
				t,
				input,
				directory,
				target.ScopeProject,
				[]openCodeOrderSpec{
					{id: "alpha", source: "alpha@1"},
					{id: "beta", source: "beta@1"},
				},
			)
			server := observation.Documents()[0]
			candidate, _ := server.Candidate()
			parsed, err := opencodeconfig.ParseAt(candidate, path)
			if err != nil {
				t.Fatal(err)
			}
			got := entrySources(parsed.Entries())
			managedSlots := managedIndexes(permutation)
			if got[managedSlots[0]] != "alpha@1" || got[managedSlots[1]] != "beta@1" {
				t.Fatalf("managed slots = %v, candidate = %v", managedSlots, got)
			}
			for position, source := range permutation {
				if strings.HasPrefix(source, "foreign-") && got[position] != source {
					t.Fatalf("foreign slot %d moved: got %q want %q", position, got[position], source)
				}
			}

			if err := os.WriteFile(path, candidate, 0o600); err != nil {
				t.Fatal(err)
			}
			repeated := mustOpenCodeOrderObservation(
				t,
				input,
				directory,
				target.ScopeProject,
				[]openCodeOrderSpec{
					{id: "alpha", source: "alpha@1"},
					{id: "beta", source: "beta@1"},
				},
			)
			if repeated.Documents()[0].Changed() {
				t.Fatal("repeated projection was not idempotent")
			}
		})
	}
}

func TestReadOrderRejectsUnsafeSelectedDocuments(t *testing.T) {
	tests := map[string]func(*testing.T, string){
		"symlink": func(t *testing.T, path string) {
			targetPath := filepath.Join(filepath.Dir(path), "target.json")
			writeOpenCodeOrderDocument(
				t,
				targetPath,
				`{"plugin":["alpha@1","beta@1"]}`,
			)
			if err := os.Symlink(targetPath, path); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
		},
		"directory": func(t *testing.T, path string) {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"oversized": func(t *testing.T, path string) {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			content := make([]byte, observeopencode.MaximumConfigBytes+1)
			for index := range content {
				content[index] = ' '
			}
			if err := os.WriteFile(path, content, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"malformed": func(t *testing.T, path string) {
			writeOpenCodeOrderDocument(t, path, `{"plugin":[`)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			input, directory := openCodeOrderInventory(t, target.ScopeProject)
			mutate(t, filepath.Join(directory, "opencode.json"))
			_, err := openCodeOrderObservation(
				input,
				directory,
				target.ScopeProject,
				[]openCodeOrderSpec{
					{id: "alpha", source: "alpha@1"},
					{id: "beta", source: "beta@1"},
				},
			)
			if err == nil {
				t.Fatal("ReadOrder accepted unsafe selected document")
			}
		})
	}
}

type openCodeOrderSpec struct {
	id     string
	source string
}

func openCodeOrderInventory(
	t *testing.T,
	scope target.Scope,
) (observeopencode.InventoryInput, string) {
	t.Helper()
	root := t.TempDir()
	input := observeopencode.InventoryInput{
		ManifestRoot: filepath.Join(root, "project"),
		ConfigRoot:   filepath.Join(root, "global"),
		Scope:        scope,
	}
	directory, err := opencodeconfig.ConfigDirectory(
		input.ManifestRoot,
		input.ConfigRoot,
		scope,
	)
	if err != nil {
		t.Fatalf("ConfigDirectory: %v", err)
	}
	return input, directory
}

func mustOpenCodeOrderObservation(
	t *testing.T,
	input observeopencode.InventoryInput,
	directory string,
	scope target.Scope,
	specs []openCodeOrderSpec,
) observeopencode.OrderObservation {
	t.Helper()
	observation, err := openCodeOrderObservation(input, directory, scope, specs)
	if err != nil {
		t.Fatalf("ReadOrder: %v", err)
	}
	return observation
}

func openCodeOrderObservation(
	input observeopencode.InventoryInput,
	directory string,
	scope target.Scope,
	specs []openCodeOrderSpec,
) (observeopencode.OrderObservation, error) {
	capability, admitted := profile.Profile(target.TargetOpenCode).ExtensionOrder(
		desiredextension.CarrierOpenCodePlugin,
		scope,
	)
	if !admitted {
		return observeopencode.OrderObservation{}, fmt.Errorf(
			"OpenCode %s order capability is absent",
			scope,
		)
	}
	members := make([]hostrelation.RelationOrderMember, 0, len(specs))
	relations := make([]observeopencode.ScopedRelation, 0, len(specs))
	for _, spec := range specs {
		subject, err := topology.NewSubjectID(
			topology.SubjectHostRelation,
			"opencode.plugin-carrier",
			spec.id,
		)
		if err != nil {
			return observeopencode.OrderObservation{}, err
		}
		subjectKey, err := hostrelation.NewSubjectKey(spec.source)
		if err != nil {
			return observeopencode.OrderObservation{}, err
		}
		managedKey, err := hostrelation.NewManagedInstanceKey("host-relation:v1:" + spec.id)
		if err != nil {
			return observeopencode.OrderObservation{}, err
		}
		expected, err := hostrelation.NewExpectedRelation(subjectKey, managedKey)
		if err != nil {
			return observeopencode.OrderObservation{}, err
		}
		key, err := observerelation.NewCorrelationKey(subject, expected)
		if err != nil {
			return observeopencode.OrderObservation{}, err
		}
		relation, err := observeopencode.NewScopedRelation(key, scope)
		if err != nil {
			return observeopencode.OrderObservation{}, err
		}
		identity, err := opencodeconfig.HostLoadIdentity(
			spec.source,
			filepath.Join(directory, "opencode.json"),
		)
		if err != nil {
			return observeopencode.OrderObservation{}, err
		}
		loadIdentity, err := hostrelation.NewHostLoadIdentity(identity)
		if err != nil {
			return observeopencode.OrderObservation{}, err
		}
		member, err := hostrelation.NewRelationOrderMember(subject, loadIdentity)
		if err != nil {
			return observeopencode.OrderObservation{}, err
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
		return observeopencode.OrderObservation{}, err
	}
	return observeopencode.ReadOrder(observeopencode.OrderInput{
		Inventory:  input,
		Constraint: constraint,
		Relations:  relations,
	})
}

func writeOpenCodeOrderDocument(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func entrySources(entries []opencodeconfig.Entry) []string {
	sources := make([]string, 0, len(entries))
	for _, entry := range entries {
		sources = append(sources, entry.Source())
	}
	return sources
}

func managedIndexes(sources []string) []int {
	indexes := make([]int, 0, 2)
	for index, source := range sources {
		if source == "alpha@1" || source == "beta@1" {
			indexes = append(indexes, index)
		}
	}
	return indexes
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
			row := []string{value}
			row = append(row, suffix...)
			result = append(result, row)
		}
	}
	return result
}
