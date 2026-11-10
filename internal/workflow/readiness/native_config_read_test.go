package readiness

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"

	"github.com/isty2e/daem/internal/filesnapshot"

	managedhostpath "github.com/isty2e/daem/internal/output/hostpath/managed"
	pihostpath "github.com/isty2e/daem/internal/output/hostpath/pi"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestNativeMCPReaderReusesObservedAliasesWithoutIO(t *testing.T) {
	fixture := newNativePlanningFixture(t, target.ScopeProject, "extension-settings/test.json")
	aliasPath := filepath.Join(fixture.paths.ManifestRoot, ".pi", "alias.json")
	if err := os.Symlink(fixture.hostPath, aliasPath); err != nil {
		t.Fatal(err)
	}
	alias, err := aggregate.NewDocumentAddress(target.TargetPi, target.ScopeProject, outputtest.Parse(t, ".pi/alias.json"))
	if err != nil {
		t.Fatal(err)
	}
	fixture.observation.Aliases = append(fixture.observation.Aliases, alias)
	documents, err := journalpublication.ObserveNativeDocuments(t.Context(), fixture.observation)
	if err != nil {
		t.Fatal(err)
	}
	native := []nativeAssessmentSources{{input: &NativeAssessmentInput{Documents: &documents}}}
	reader, err := nativeConfigReader(native)
	if err != nil {
		t.Fatal(err)
	}
	original, _, observed := documents.Evidence().Document(alias)
	if !observed {
		t.Fatal("fixture alias was not captured")
	}
	if err := os.WriteFile(fixture.hostPath, []byte(`{"changed":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{fixture.hostPath, aliasPath} {
		content, exists, err := reader(t.Context(), path, 1024)
		if err != nil || !exists || !bytes.Equal(content, original.Content()) {
			t.Fatal("MCP reader reread a retained alias", err)
		}
	}
	if _, _, err := reader(t.Context(), aliasPath, 1); !errors.Is(err, filesnapshot.ErrLimitExceeded) {
		t.Fatal("retained content bypassed the observer's byte limit", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := reader(ctx, aliasPath, 1024); !errors.Is(err, context.Canceled) {
		t.Fatal("retained content bypassed cancellation", err)
	}
	unrelated := filepath.Join(t.TempDir(), "config.json")
	if _, exists, err := reader(t.Context(), unrelated, 1024); err != nil || exists {
		t.Fatal("unrelated absence was not preserved", err)
	}
	if err := os.WriteFile(unrelated, []byte(`{"unrelated":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if content, exists, err := reader(t.Context(), unrelated, 1024); err != nil || !exists || string(content) != `{"unrelated":true}` {
		t.Fatal("unrelated document did not use its normal bounded read", err)
	}
}

func TestNativeMCPReaderKeepsCapturedAndNewlyResolvedLocationsDistinct(t *testing.T) {
	fixture := newNativePlanningFixture(t, target.ScopeGlobal, "extension-settings/test.json")
	documents, err := journalpublication.ObserveNativeDocuments(t.Context(), fixture.observation)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(pihostpath.AgentRootEnvironmentVariable, t.TempDir())
	resolver := managedhostpath.Resolver(fixture.paths, nil).Resolve
	alias := fixture.observation.Aliases[0]
	ordinaryPath, err := resolver(alias.AggregateRoot())
	if err != nil {
		t.Fatal(err)
	}
	ordinary := []byte(`{"ordinary":true}`)
	if err := os.MkdirAll(filepath.Dir(ordinaryPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ordinaryPath, ordinary, 0o600); err != nil {
		t.Fatal(err)
	}
	native := []nativeAssessmentSources{{input: &NativeAssessmentInput{Documents: &documents}}}
	reader, err := nativeConfigReader(native)
	if err != nil {
		t.Fatal(err)
	}
	original, _, observed := documents.Evidence().Document(alias)
	if !observed {
		t.Fatal("fixture alias was not captured")
	}
	if err := os.WriteFile(fixture.hostPath, []byte(`{"later":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	content, exists, err := reader(t.Context(), ordinaryPath, 1024)
	if err != nil || !exists || !bytes.Equal(content, ordinary) {
		t.Fatalf("ordinary document was replaced with retained native bytes: %s, %v", content, err)
	}
	content, exists, err = reader(t.Context(), fixture.hostPath, 1024)
	if err != nil || !exists || !bytes.Equal(content, original.Content()) {
		t.Fatal("captured document lost its selected physical location", err)
	}
}

func TestNativeMCPReaderRetainsGlobalRootSelectorSpellings(t *testing.T) {
	for _, spelling := range []string{"symlink", "trailing separator", "parent component"} {
		t.Run(spelling, func(t *testing.T) {
			if spelling == "parent component" && runtime.GOOS == "windows" {
				t.Skip("Win32 normalizes parents before following reparse points")
			}
			fixture := newNativePlanningFixture(t, target.ScopeGlobal, "extension-settings/test.json")
			qualified := fixture.input.Documents.Evidence().Namespaces()[0].Aliases()[0]
			physical := qualified.Root().SelectedPath()
			if spelling == "parent component" {
				physical = filepath.Join(physical, "inner")
				if err := os.Mkdir(physical, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			selector := filepath.Join(t.TempDir(), "agent")
			if err := os.Symlink(physical, selector); err != nil {
				t.Fatal(err)
			}
			if spelling == "parent component" {
				selector += string(filepath.Separator) + ".."
			} else if spelling == "trailing separator" {
				selector += string(filepath.Separator)
			}
			root, err := journalnamespace.CaptureRootSelector(t.Context(), selector, fixture.observation.Budget)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = root.Close() })
			fixture.observation.Roots, err = fixture.observation.Roots.With(qualified.Root().RootReference(), root)
			if err != nil {
				t.Fatal(err)
			}
			documents, err := journalpublication.ObserveNativeDocuments(t.Context(), fixture.observation)
			if err != nil {
				t.Fatal(err)
			}
			original, _, observed := documents.Evidence().Document(qualified.Address())
			if !observed {
				t.Fatal("fixture alias was not captured")
			}
			if err := os.WriteFile(fixture.hostPath, []byte(`{"later":true}`), 0o600); err != nil {
				t.Fatal(err)
			}
			relative := filepath.FromSlash(qualified.Address().AggregateRoot().RelativePath())
			paths := []string{selector + string(filepath.Separator) + relative, filepath.Join(root.SelectedPath, relative)}
			cleaned := filepath.Join(selector, relative)
			ordinary := []byte(`{"ordinary":true}`)
			if spelling == "parent component" {
				if err := os.MkdirAll(filepath.Dir(cleaned), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(cleaned, ordinary, 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				paths = append(paths, cleaned)
			}
			native := []nativeAssessmentSources{{input: &NativeAssessmentInput{Documents: &documents}}}
			reader, err := nativeConfigReader(native)
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range paths {
				content, exists, err := reader(t.Context(), path, 1024)
				if err != nil || !exists || !bytes.Equal(content, original.Content()) {
					t.Fatalf("root selector spelling reread the document at %s: %s, %v", path, content, err)
				}
			}
			if spelling == "parent component" {
				content, exists, err := reader(t.Context(), cleaned, 1024)
				if err != nil || !exists || !bytes.Equal(content, ordinary) {
					t.Fatal("lexical cleaning redirected a different physical document", err)
				}
			}
		})
	}
}
