package merge

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	adoptextension "github.com/isty2e/daem/internal/adopt/extension"
	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/target"
)

func TestIntoManifestMergesExactExtensionsWithoutRewritingExistingBytes(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	writeMergeExtensionFixture(
		t,
		filepath.Join(root, ".pi", "settings.json"),
		`{"packages":["npm:new","npm:existing"]}`,
	)
	existing := mustMergeExtension(
		t,
		"kept-id",
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		target.ScopeProject,
		desiredextension.SourceKindHostSource,
		"npm:existing",
	)
	original := []byte(`version = 1
targets = ["pi"]

# existing extension comment
[[extension]]
id = "kept-id"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "npm:existing" }

[defaults]
scope = "project"
`)
	result, err := adoptextension.Collect(adoptextension.Input{
		ManifestRoot: root,
		Targets:      []target.Target{target.TargetPi},
		Scopes:       []target.Scope{target.ScopeProject},
		Existing:     []desiredextension.Extension{existing},
	})
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := adoptmodel.NewCandidateSet(adoptmodel.CandidateSetInput{
		Extensions: result,
	})
	if err != nil {
		t.Fatal(err)
	}
	sourceDirectory, err := adoptmodel.NewSourceDirectory(
		filepath.Join(root, "daem.toml"),
		filepath.Join(root, "daem.d"),
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := adoptmodel.NewRequest(
		[]target.Target{target.TargetPi},
		[]target.Scope{target.ScopeProject},
		filepath.Join(root, "daem.toml"),
		sourceDirectory,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := IntoManifest(request, original, candidates, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Extensions()) != 1 ||
		plan.Extensions()[0].Source().Ref() != "npm:new" {
		t.Fatalf("writable extensions = %#v", plan.Extensions())
	}
	rendered := plan.ManifestContent()
	if !bytes.Contains(rendered, []byte("# existing extension comment")) ||
		!bytes.Contains(rendered, []byte("[defaults]\nscope = \"project\"")) {
		t.Fatalf("merge rewrote retained bytes:\n%s", rendered)
	}
	newIndex := bytes.Index(rendered, []byte(`source = { host_source = "npm:new" }`))
	commentIndex := bytes.Index(rendered, []byte("# existing extension comment"))
	existingIndex := bytes.Index(rendered, []byte(`source = { host_source = "npm:existing" }`))
	if newIndex < 0 || commentIndex < 0 || existingIndex < 0 ||
		!(newIndex < commentIndex && commentIndex < existingIndex) {
		t.Fatalf("merged extension order does not match Pi order:\n%s", rendered)
	}
	if len(plan.MergeResults()) != 2 ||
		plan.MergeResults()[0].Status != adoptmodel.MergeStatusAdd ||
		plan.MergeResults()[1].Status != adoptmodel.MergeStatusNoop {
		t.Fatalf("merge results = %#v", plan.MergeResults())
	}
}

func TestInsertImportedExtensionsPreservesCRLFAndRelativeOrder(t *testing.T) {
	existing := mustMergeExtension(
		t,
		"middle",
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		target.ScopeProject,
		desiredextension.SourceKindHostSource,
		"npm:middle",
	)
	before := mustMergeExtension(
		t,
		"before",
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		target.ScopeProject,
		desiredextension.SourceKindHostSource,
		"npm:before",
	)
	after := mustMergeExtension(
		t,
		"after",
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		target.ScopeProject,
		desiredextension.SourceKindHostSource,
		"npm:after",
	)
	original := []byte(strings.ReplaceAll(`# keep
[[extension]]
id = "middle"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "npm:middle" }
`, "\n", "\r\n"))

	merged, err := insertImportedExtensions(
		original,
		[]desiredextension.Extension{before, existing, after},
		[]desiredextension.Extension{before, after},
	)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(bytes.ReplaceAll(merged, []byte("\r\n"), nil), []byte("\n")) {
		t.Fatalf("merge introduced bare LF into CRLF document: %q", merged)
	}
	beforeIndex := bytes.Index(merged, []byte(`id = "before"`))
	middleIndex := bytes.Index(merged, []byte(`id = "middle"`))
	afterIndex := bytes.Index(merged, []byte(`id = "after"`))
	if beforeIndex < 0 || middleIndex < 0 || afterIndex < 0 ||
		!(beforeIndex < middleIndex && middleIndex < afterIndex) {
		t.Fatalf("extension order =\n%s", merged)
	}
	if !bytes.Contains(merged, []byte("# keep\r\n")) {
		t.Fatalf("existing comment changed:\n%s", merged)
	}
}

func TestClassifyImportExtensionMergeRejectsSameIDDifferentRelation(t *testing.T) {
	imported := mustMergeExtension(
		t,
		"collision",
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		target.ScopeProject,
		desiredextension.SourceKindHostSource,
		"npm:new",
	)
	result := classifyImportExtensionMerge(existingDeclarations{
		Extensions: []declarationcodec.ExtensionBlock{{
			Extension: declaration.Extension{
				ID:      "collision",
				Carrier: "pi-package",
				Targets: []string{"pi"},
				Scope:   "project",
				Source:  declaration.ExtensionSource{HostSource: "npm:old"},
			},
		}},
	}, imported)
	if result.Status != adoptmodel.MergeStatusConflict {
		t.Fatalf("merge result = %#v", result)
	}
}

func TestClassifyImportExtensionMergeRejectsSameRelationDifferentID(t *testing.T) {
	imported := mustMergeExtension(
		t,
		"incoming-id",
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		target.ScopeProject,
		desiredextension.SourceKindHostSource,
		"npm:existing",
	)
	result := classifyImportExtensionMerge(existingDeclarations{
		Extensions: []declarationcodec.ExtensionBlock{{
			Extension: declaration.Extension{
				ID:      "existing-id",
				Carrier: "pi-package",
				Targets: []string{"pi"},
				Scope:   "project",
				Source:  declaration.ExtensionSource{HostSource: "npm:existing"},
			},
		}},
	}, imported)
	if result.Status != adoptmodel.MergeStatusConflict ||
		!strings.Contains(result.Detail, "different id") {
		t.Fatalf("merge result = %#v", result)
	}
}

func TestCollectRejectsNativeOrderContradictingExistingManifestOrder(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	writeMergeExtensionFixture(
		t,
		filepath.Join(root, ".pi", "settings.json"),
		`{"packages":["npm:first","npm:second"]}`,
	)
	first := mustMergeExtension(
		t,
		"first",
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		target.ScopeProject,
		desiredextension.SourceKindHostSource,
		"npm:first",
	)
	second := mustMergeExtension(
		t,
		"second",
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		target.ScopeProject,
		desiredextension.SourceKindHostSource,
		"npm:second",
	)
	_, err := adoptextension.Collect(adoptextension.Input{
		ManifestRoot: root,
		Targets:      []target.Target{target.TargetPi},
		Scopes:       []target.Scope{target.ScopeProject},
		Existing:     []desiredextension.Extension{second, first},
	})
	if err == nil || !strings.Contains(err.Error(), "contradicts") {
		t.Fatalf("Collect error = %v, want existing/native order contradiction", err)
	}
}

func mustMergeExtension(
	t *testing.T,
	id string,
	carrier desiredextension.Carrier,
	selectedTarget target.Target,
	scope target.Scope,
	sourceKind desiredextension.SourceKind,
	sourceValue string,
) desiredextension.Extension {
	t.Helper()
	source, err := desiredextension.NewSourceRef(sourceKind, sourceValue)
	if err != nil {
		t.Fatal(err)
	}
	extension, err := desiredextension.New(desiredextension.Spec{
		Name:    id,
		Carrier: carrier,
		Target:  selectedTarget,
		Scope:   scope,
		Source:  source,
	})
	if err != nil {
		t.Fatal(err)
	}
	return extension
}

func writeMergeExtensionFixture(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
