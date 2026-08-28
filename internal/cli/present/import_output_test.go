package clipresent

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	adoptextension "github.com/isty2e/daem/internal/adopt/extension"
	"github.com/isty2e/daem/internal/target"
)

func TestImportPresentationDisclosesExactExtensions(t *testing.T) {
	plan := presentPiImportPlan(t, "npm:@acme/tools@1.2.3")

	presented := ImportPlanFromAdoption("import plan", plan, true)
	if presented.Summary.Extensions != 1 ||
		len(presented.Resources) != 1 ||
		!presented.Resources[0].Extension ||
		presented.Resources[0].Carrier != "pi-package" {
		t.Fatalf("presented extension = %#v", presented)
	}
	var human bytes.Buffer
	PrintImportPlanWithOptions(&human, presented, HumanOptions{Verbose: true})
	for _, expected := range []string{
		"extensions=1",
		`carrier="pi-package"`,
		`source="npm:@acme/tools@1.2.3"`,
	} {
		if !strings.Contains(human.String(), expected) {
			t.Fatalf("human output lacks %q:\n%s", expected, human.String())
		}
	}

	jsonOutput := ImportPlanJSONOutput("dry-run", plan)
	if len(jsonOutput.Changes) != 1 ||
		jsonOutput.Changes[0].Resource.Kind != "extension" ||
		jsonOutput.Changes[0].Carrier != "pi-package" ||
		jsonOutput.Changes[0].SourceRedacted ||
		len(jsonOutput.Summary) != 1 ||
		jsonOutput.Summary[0].Extensions != 1 {
		t.Fatalf("JSON output = %#v", jsonOutput)
	}
}

func TestImportPresentationProjectsPrivateExtensionSources(t *testing.T) {
	const source = "plugins/local.ts"
	plan := presentPiImportPlan(t, source)

	presented := ImportPlanFromAdoption("import plan", plan, true)
	if len(presented.Resources) != 1 ||
		!presented.Resources[0].SourceRedacted ||
		strings.Contains(presented.Resources[0].Source, source) ||
		!strings.HasPrefix(presented.Resources[0].Source, "redacted:sha256:") {
		t.Fatalf("presented extension = %#v", presented.Resources)
	}
	var verbose bytes.Buffer
	PrintImportPlanWithOptions(&verbose, presented, HumanOptions{Verbose: true})
	if !strings.Contains(verbose.String(), source) {
		t.Fatalf("verbose output = %q, want local source", verbose.String())
	}

	jsonOutput := ImportPlanJSONOutput("dry-run", plan)
	if len(jsonOutput.Changes) != 1 ||
		!jsonOutput.Changes[0].SourceRedacted ||
		strings.Contains(jsonOutput.Changes[0].Source, source) ||
		!strings.HasPrefix(jsonOutput.Changes[0].Source, "redacted:sha256:") {
		t.Fatalf("JSON output = %#v", jsonOutput)
	}
}

func presentPiImportPlan(t *testing.T, source string) adoptmodel.Plan {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	settingsPath := filepath.Join(root, ".pi", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		settingsPath,
		[]byte(`{"packages":[`+fmt.Sprintf("%q", source)+`]}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	result, err := adoptextension.Collect(t.Context(), adoptextension.Input{
		ManifestRoot: root,
		Targets:      []target.Target{target.TargetPi},
		Scopes:       []target.Scope{target.ScopeProject},
	}, func(adoptextension.Skip) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := adoptmodel.NewCandidateSet(adoptmodel.CandidateSetInput{
		Extensions: result,
	})
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "daem.toml")
	sourceDirectory, err := adoptmodel.NewSourceDirectory(
		output,
		filepath.Join(root, "daem.d"),
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := adoptmodel.NewRequest(
		[]target.Target{target.TargetPi},
		[]target.Scope{target.ScopeProject},
		output,
		sourceDirectory,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := adoptmodel.RenderManifestContent(
		nil,
		nil,
		nil,
		nil,
		candidates.Extensions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := adoptmodel.NewPlan(request, nil, manifest, candidates, nil)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
