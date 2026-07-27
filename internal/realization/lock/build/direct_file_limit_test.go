package build

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/hookasset"
	"github.com/isty2e/daem/internal/desired/instructions"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/supply/source/directfile"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

func TestBuildRejectsOversizedDirectFileFamiliesWithoutLockPublication(t *testing.T) {
	tests := []struct {
		name        string
		sourcePath  string
		environment func(*testing.T, source.Source) desired.Environment
	}{
		{
			name:       "instructions",
			sourcePath: "instructions/AGENTS.md",
			environment: func(t *testing.T, sourceSpec source.Source) desired.Environment {
				return lockEnvironment(t, desired.Spec{
					Instructions: []instructions.Instructions{
						projectInstructions(t, "project", sourceSpec, []target.Target{target.TargetCodex}),
					},
				})
			},
		},
		{
			name:       "hook_asset",
			sourcePath: "hooks/guard.sh",
			environment: func(t *testing.T, sourceSpec source.Source) desired.Environment {
				return lockEnvironment(t, desired.Spec{
					HookAssets: []hookasset.HookAsset{
						desiredtest.HookAsset(t, hookasset.Spec{
							Name:         "guard",
							Source:       sourceSpec,
							ArtifactKind: hookasset.ArtifactKindFile,
							Scope:        target.ScopeProject,
							Executable:   true,
						}),
					},
				})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, test.sourcePath)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := file.Truncate(128<<20 + 1); err != nil {
				file.Close()
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}

			sourceSpec := sourcetest.Local(t, test.sourcePath, source.LocalSourceModeVendor)
			resolution := oversizedFileResolution(t, sourceSpec, path)
			lockfile, err := buildWithTestOptions(
				context.Background(),
				test.environment(t, sourceSpec),
				fixedResolutionResolver{resolution: resolution},
				Options{},
			)
			var limitErr *directfile.LimitError
			if !errors.As(err, &limitErr) {
				t.Fatalf("Build error = %v, want directfile.LimitError", err)
			}
			if lockfile.Version != 0 || lockfile.Locked.Len() != 0 {
				t.Fatalf("failed build returned published lock state: %#v", lockfile)
			}
		})
	}
}

type fixedResolutionResolver struct {
	resolution acquisition.Resolution
}

func (resolver fixedResolutionResolver) Resolve(
	_ context.Context,
	_ source.Source,
	_ acquisition.OperationOptions,
) (acquisition.Resolution, error) {
	return resolver.resolution, nil
}

func oversizedFileResolution(
	t *testing.T,
	sourceSpec source.Source,
	path string,
) acquisition.Resolution {
	t.Helper()
	view, err := access.OpenView(path)
	if err != nil {
		t.Fatal(err)
	}
	sourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := artifact.NewExactIdentity(
		sourceID,
		"",
		artifact.ArtifactKindFile,
		artifact.HashFileContent(nil),
	)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := acquisition.NewResolution(sourceSpec, identity, view)
	if err != nil {
		t.Fatal(err)
	}
	return resolution
}

var _ acquisition.Resolver = fixedResolutionResolver{}
