package adopt

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
)

func TestInitialHostConfigCaptureUsesSelectedLeavesAndSharedExecution(t *testing.T) {
	for _, merge := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "merge"}[merge], func(t *testing.T) {
			fixture := newCaptureWorkflowFixture(t, "keys", []byte(`/* not copied */ {"nested":{"a":4,"empty":{}},"unselected":"host-only"}`))
			if !merge {
				if err := os.Remove(fixture.paths.ManifestPath); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(fixture.paths.ManifestRoot, "captured.jsonc")
			local, err := source.NewLocalSource(path, source.LocalSourceModeVendor)
			if err != nil {
				t.Fatal(err)
			}
			config, err := hostconfig.New(hostconfig.Spec{Name: "new", Source: local, Target: target.TargetCodex, Scope: target.ScopeProject, Path: "capture.jsonc"})
			if err != nil {
				t.Fatal(err)
			}
			input := HostConfigInitialInput{Paths: fixture.paths, Config: config, Keys: []output.KeyPath{captureKey(t, "/nested")}, Merge: merge}
			plan, err := BuildHostConfigInitialPlan(t.Context(), input)
			if err != nil {
				t.Fatal(err)
			}
			defer plan.Close()
			if len(plan.SelectedKeys()) != 2 || !plan.Initial() || bytes.Contains(plan.SourceAfter(), []byte("not copied")) || bytes.Contains(plan.SourceAfter(), []byte("host-only")) {
				t.Fatal("initial selection leaked unselected data or comments")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("preview wrote source", err)
			}
			if _, err := ExecuteHostConfigCapture(t.Context(), plan, plan.ApprovalPath()); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(fixture.paths.ManifestPath)
			if err != nil {
				t.Fatal(err)
			}
			environment, err := declarationmanifest.Decode(after)
			if err != nil {
				t.Fatal(err)
			}
			count := 1
			if merge {
				count = 3
				if !bytes.HasPrefix(after, fixture.manifest) {
					t.Fatal("merge rewrote existing declarations")
				}
			}
			if len(environment.HostConfigs()) != count {
				t.Fatal("wrong declaration count")
			}
			stored, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			document, err := configdocument.Parse(t.Context(), stored, configdocument.JSONC, configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32})
			if err != nil {
				t.Fatal(err)
			}
			if _, present, err := document.Lookup(captureKey(t, "/nested/a")); err != nil || !present {
				t.Fatal("selected value missing", err)
			}
			assertCaptureArtifact(t, fixture.host, []byte(`/* not copied */ {"nested":{"a":4,"empty":{}},"unselected":"host-only"}`))
		})
	}
}

func TestInitialHostConfigCaptureRefusesMissingSelectionExistingSourceAndWholeAbsence(t *testing.T) {
	for _, scenario := range []string{"missing-key", "existing-source", "missing-whole", "no-selection", "merge-required", "duplicate"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newCaptureWorkflowFixture(t, "keys", []byte(`{"nested":{"a":1}}`))
			path := filepath.Join(fixture.paths.ManifestRoot, "new.jsonc")
			name, ownership, keys := "new", hostconfig.OwnershipKeys, []output.KeyPath{captureKey(t, "/nested/a")}
			switch scenario {
			case "missing-key":
				keys = []output.KeyPath{captureKey(t, "/missing")}
			case "existing-source":
				path = fixture.source
			case "missing-whole":
				ownership, keys = hostconfig.OwnershipDocument, nil
				if err := os.Remove(fixture.host); err != nil {
					t.Fatal(err)
				}
			case "no-selection":
				keys = nil
			case "duplicate":
				name = "chosen"
			}
			local, err := source.NewLocalSource(path, source.LocalSourceModeVendor)
			if err != nil {
				t.Fatal(err)
			}
			config, err := hostconfig.New(hostconfig.Spec{Name: name, Source: local, Target: target.TargetCodex, Scope: target.ScopeProject, Path: "capture.jsonc", Ownership: ownership})
			if err != nil {
				t.Fatal(err)
			}
			plan, err := BuildHostConfigInitialPlan(t.Context(), HostConfigInitialInput{Paths: fixture.paths, Config: config, Keys: keys, Merge: scenario != "merge-required"})
			if plan != nil {
				defer plan.Close()
			}
			if err == nil {
				t.Fatal("invalid initial capture admitted")
			}
			assertCaptureArtifact(t, fixture.paths.ManifestPath, fixture.manifest)
		})
	}
}

func TestCaptureApprovalPreservesRawPathSpellingAndFieldBoundaries(t *testing.T) {
	fixture := newCaptureWorkflowFixture(t, "keys", []byte(`{"nested":{"a":1}}`))
	plan, err := BuildHostConfigUpdatePlan(t.Context(), HostConfigUpdateInput{Paths: fixture.paths, Name: "chosen"})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	plan.input.Paths.ManifestPath = filepath.Join(fixture.paths.ManifestRoot, "manifest-\xff.toml")
	first := plan.Fingerprint()
	plan.input.Paths.ManifestPath = filepath.Join(fixture.paths.ManifestRoot, "manifest-\xfe.toml")
	if first == plan.Fingerprint() {
		t.Fatal("approval collapsed distinct raw path spellings")
	}
	if captureFingerprint([]byte("a"), []byte("bc")) == captureFingerprint([]byte("ab"), []byte("c")) {
		t.Fatal("approval lost field boundaries")
	}
}
