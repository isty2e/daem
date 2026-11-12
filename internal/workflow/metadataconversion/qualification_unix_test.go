//go:build darwin || linux

package metadataconversion

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/recoverygate"
)

func TestCandidateOriginPreservesProofAndRecordedDuplicate(t *testing.T) {
	fixture := compilerFixture(t, 1)
	path := fixture.states[0]
	observed, err := mutation.ObservePersistedDirectoryEntryAuthority(path)
	if err != nil {
		t.Fatal(err)
	}
	_, proven := observed.Exact().LegacyLocatorCandidate()
	for _, recorded := range []bool{false, true} {
		budget, err := physicalwork.NewBudget(0)
		if err != nil {
			t.Fatal(err)
		}
		qualifier, err := newReferenceQualification(budget, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		candidates := []lookupCandidate{{path: path, origin: lookupCurrentRoot}}
		if recorded {
			candidates = append(candidates, lookupCandidate{path: path, origin: lookupRecordedRoot})
		}
		result, err := qualifier.require(t.Context(), observed.Exact(), candidates)
		if proven || recorded {
			if err != nil || !result.Equal(observed.Reference()) {
				t.Fatal("proven or independently recorded root was over-rejected", err)
			}
		} else if !errors.Is(err, mutation.ErrLegacyReferenceUnprovable) || !result.IsZero() {
			t.Fatal("current root laundered a lossy identity", err)
		}
	}
}

func TestSuppliedReferencesCannotConflictOrExpandTheClosure(t *testing.T) {
	fixture := compilerFixture(t, 1)
	actual, err := mutation.ObservePersistedDirectoryEntryAuthority(fixture.states[0])
	if err != nil {
		t.Fatal(err)
	}
	conflicting, err := pathauthority.NewReference(actual.Exact(), filepath.Join(filepath.Dir(fixture.paths.ManifestRoot), "wrong", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	extra, err := mutation.ObservePersistedDirectoryEntryAuthority(filepath.Join(filepath.Dir(fixture.paths.ManifestRoot), "unrelated.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, candidates := range [][]pathauthority.Reference{{actual.Reference(), conflicting}, {extra.Reference()}} {
		authority, err := recoverygate.NewEffectAuthority(t.Context(), fixture.paths)
		if err != nil {
			t.Fatal(err)
		}
		budget, err := physicalwork.NewBudget(0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Prepare(t.Context(), authority, budget, candidates); err == nil {
			t.Fatal("supplied reference conflict or extra identity was admitted")
		}
	}
	fixture.assertUnchanged(t)
}

func TestLossyDocumentRootsAreRejectedBeforeEnvironmentResolution(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, directory := range map[string]string{
		"HOME": "home", "XDG_CONFIG_HOME": "config", "XDG_STATE_HOME": "state",
		"XDG_DATA_HOME": "data", "XDG_CACHE_HOME": "cache", "PI_CODING_AGENT_DIR": "pi",
		"CODEX_HOME": "codex", "CLAUDE_CONFIG_DIR": "claude",
	} {
		path := filepath.Join(root, directory)
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv(name, path)
	}
	fixture := compilerFixture(t, 1)
	expected, err := pathauthority.NewExact("/root/document.json", "darwin-case-v1:ii")
	if err != nil {
		t.Fatal(err)
	}
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	proven, err := pathauthority.NewExact("/root/document.json", "exact-v1:")
	if err != nil {
		t.Fatal(err)
	}
	for _, spelling := range []string{"~/document.json", "@data/document.json", "@pi-agent/settings.json", "@codex-home/config.toml", "~/.claude.json"} {
		destination, err := output.Parse(spelling)
		if err != nil {
			t.Fatal(err)
		}
		candidates, err := destinationCandidates(fixture.paths, destination, expected, budget)
		if err != nil || len(candidates) != 0 {
			t.Fatal("current-root document bypassed the origin predicate", spelling, err)
		}
		allowed, err := destinationCandidates(fixture.paths, destination, proven, budget)
		if err != nil || len(allowed) == 0 {
			t.Fatal("version-proven current-root document was over-rejected", spelling, err)
		}
	}
	project, err := output.Parse("document.json")
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := destinationCandidates(fixture.paths, project, expected, budget)
	if err != nil || len(candidates) != 1 || candidates[0].path != filepath.Join(fixture.paths.ManifestRoot, "document.json") {
		t.Fatal("recorded project root was over-rejected", err)
	}
}
