package metadataconversion

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	physicalhostpath "github.com/isty2e/daem/internal/assurance/observe/hostpath"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/output"
	nativehostpath "github.com/isty2e/daem/internal/output/hostpath/native"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

type referenceQualification struct {
	budget fileset.PhysicalWorkBudget
	fixed  map[pathauthority.Exact]pathauthority.Reference
	found  map[pathauthority.Exact]pathauthority.Reference
}

type lookupOrigin uint8

const (
	lookupRecordedRoot lookupOrigin = iota + 1
	lookupCurrentRoot
)

type lookupCandidate struct {
	path   string
	origin lookupOrigin
}

func (origin lookupOrigin) admits(expected pathauthority.Exact) bool {
	switch origin {
	case lookupRecordedRoot:
		return true
	case lookupCurrentRoot:
		_, proven := expected.LegacyLocatorCandidate()
		return proven
	default:
		return false
	}
}

func newReferenceQualification(budget fileset.PhysicalWorkBudget, recorded, supplied []pathauthority.Reference) (*referenceQualification, error) {
	value := &referenceQualification{
		budget: budget, fixed: make(map[pathauthority.Exact]pathauthority.Reference),
		found: make(map[pathauthority.Exact]pathauthority.Reference),
	}
	for _, reference := range append(slices.Clone(recorded), supplied...) {
		if err := reference.Validate(); err != nil {
			return nil, err
		}
		identity := reference.Identity()
		if previous, present := value.fixed[identity]; present && !previous.Equal(reference) {
			return nil, fmt.Errorf("identity %q has conflicting locator evidence; replacement requires an explicit transition", identity.Key())
		}
		value.fixed[identity] = reference
	}
	return value, nil
}

func (value *referenceQualification) observe(ctx context.Context, candidate string) (pathauthority.Reference, error) {
	if err := ctx.Err(); err != nil {
		return pathauthority.Reference{}, err
	}
	observed, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(candidate, mutationfs.MaximumPhysicalPathDepth, value.budget)
	if cancellation := ctx.Err(); cancellation != nil {
		return pathauthority.Reference{}, errors.Join(cancellation, err)
	}
	if err != nil {
		return pathauthority.Reference{}, err
	}
	return observed.Reference(), nil
}

func (value *referenceQualification) require(ctx context.Context, expected pathauthority.Exact, candidates []lookupCandidate) (pathauthority.Reference, error) {
	if reference, present := value.found[expected]; present {
		return reference, nil
	}
	if recorded, present := value.fixed[expected]; present {
		observed, err := value.observe(ctx, recorded.Locator())
		if err != nil {
			return pathauthority.Reference{}, err
		}
		if !observed.Identity().Equal(recorded.Identity()) {
			return pathauthority.Reference{}, fmt.Errorf("recorded reference %q no longer matches its exact namespace", recorded.Locator())
		}
		value.found[expected] = recorded
		return recorded, nil
	}

	var result pathauthority.Reference
	var causes []error
	seen := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		if !candidate.origin.admits(expected) || seen[candidate.path] {
			continue
		}
		seen[candidate.path] = true
		observed, err := value.observe(ctx, candidate.path)
		if cancellation := ctx.Err(); cancellation != nil {
			return pathauthority.Reference{}, errors.Join(cancellation, err)
		}
		if err != nil {
			causes = append(causes, err)
			continue
		}
		if !observed.Identity().Equal(expected) {
			continue
		}
		if !result.IsZero() && !result.Equal(observed) {
			return pathauthority.Reference{}, fmt.Errorf("identity %q has conflicting freshly qualified locators", expected.Key())
		}
		result = observed
	}
	if result.IsZero() {
		observed, err := mutation.ObserveLegacyReferenceBounded(ctx, expected, mutationfs.MaximumPhysicalPathDepth, value.budget)
		if err != nil {
			return pathauthority.Reference{}, fmt.Errorf("identity %q lacks provable access evidence; preview daem migrate metadata --reference PATH --dry-run with an asserted historical lookup path: %w", expected.Key(), errors.Join(append(causes, err)...))
		}
		result = observed.Reference()
	}
	value.found[expected] = result
	return result, nil
}

func (value *referenceQualification) selected(ctx context.Context, path string) (pathauthority.Reference, error) {
	reference, err := value.observe(ctx, path)
	if err != nil {
		return pathauthority.Reference{}, err
	}
	if previous, present := value.found[reference.Identity()]; present {
		return previous, nil
	}
	if _, present := value.fixed[reference.Identity()]; present {
		return value.require(ctx, reference.Identity(), nil)
	}
	value.found[reference.Identity()] = reference
	return reference, nil
}

type ownerCandidates struct {
	owners      []stateauthority.Authority
	contexts    []daempaths.Paths
	paths       []lookupCandidate
	barrierDirs []string
}

func (owner *ownerCandidates) discover(selected daempaths.Paths) {
	seen := make(map[string]bool, len(owner.owners))
	for _, provenance := range owner.owners {
		manifest := provenance.ManifestPath()
		if seen[manifest] {
			continue
		}
		seen[manifest] = true
		recordedStatefile := filepath.Join(filepath.Dir(manifest), ".daem", "state.json")
		owner.paths = append(owner.paths, lookupCandidate{path: recordedStatefile, origin: lookupRecordedRoot})
		paths, err := daempaths.Resolve(manifest)
		if err != nil {
			continue
		}
		paths, err = paths.WithDataDir(selected.DataDir)
		if err != nil {
			continue
		}
		recorded := paths
		recorded.ManifestPath = manifest
		recorded.ManifestRoot = filepath.Dir(manifest)
		recorded.StatefilePath = recordedStatefile
		recorded.StateDir = filepath.Dir(recordedStatefile)
		recorded.RecoveryDir = filepath.Join(recorded.StateDir, "recovery")
		owner.contexts = append(owner.contexts, recorded, paths)
		owner.paths = append(owner.paths, lookupCandidate{path: paths.StatefilePath, origin: lookupCurrentRoot})
		if legacy, err := paths.LegacyUserState(); err == nil {
			owner.contexts = append(owner.contexts, legacy)
		}
	}
}

func (owner *ownerCandidates) qualifyContexts(ctx context.Context, qualifiers *referenceQualification, reference pathauthority.Reference) error {
	var qualified []daempaths.Paths
	for _, paths := range owner.contexts {
		observed, err := qualifiers.observe(ctx, paths.StatefilePath)
		if cancellation := ctx.Err(); cancellation != nil {
			return errors.Join(cancellation, err)
		}
		if err == nil && observed.Identity().Equal(reference.Identity()) {
			qualified = append(qualified, paths)
			owner.barrierDirs = append(owner.barrierDirs, paths.StateDir)
			if paths.LegacyUserStateDir != "" {
				owner.barrierDirs = append(owner.barrierDirs, paths.LegacyUserStateDir)
			}
			for _, peer := range owner.contexts {
				if peer.LegacyUserStateDir == paths.StateDir {
					owner.barrierDirs = append(owner.barrierDirs, peer.StateDir)
				}
			}
		}
	}
	owner.contexts = qualified
	return nil
}

func destinationCandidates(paths daempaths.Paths, destination output.Destination, expected pathauthority.Exact, budget fileset.PhysicalWorkBudget) ([]lookupCandidate, error) {
	origin := lookupCurrentRoot
	if destination.RootRole() == output.RootProject {
		origin = lookupRecordedRoot
	}
	if !origin.admits(expected) {
		return nil, nil
	}
	if destination.String() == aggregate.ClaudeGlobalMCPConfigPath {
		home, err := paths.UserHomeDir()
		if err != nil {
			return nil, err
		}
		configured, present := os.LookupEnv("CLAUDE_CONFIG_DIR")
		points, err := nativehostpath.ClaudeAccountFiles(configured, present, paths.ManifestRoot, home)
		if err != nil {
			return nil, err
		}
		return []lookupCandidate{{path: points[0], origin: origin}, {path: points[1], origin: origin}}, nil
	}
	path, err := physicalhostpath.NewResolver(paths).Resolve(destination, budget)
	if err != nil {
		return nil, err
	}
	return []lookupCandidate{{path: path, origin: origin}}, nil
}

func (value *referenceQualification) hasReference(identity pathauthority.Exact) bool {
	_, recorded := value.fixed[identity]
	_, observed := value.found[identity]
	return recorded || observed
}

func (value *referenceQualification) retainRecorded(ctx context.Context, reference pathauthority.Reference) (pathauthority.Reference, error) {
	identity := reference.Identity()
	if previous, present := value.fixed[identity]; present && !previous.Equal(reference) {
		return pathauthority.Reference{}, fmt.Errorf("identity %q has conflicting recorded locators", identity.Key())
	}
	value.fixed[identity] = reference
	delete(value.found, identity)
	return value.require(ctx, identity, nil)
}
