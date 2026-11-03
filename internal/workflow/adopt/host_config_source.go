package adopt

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/supply/source"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

type HostConfigSourceReference struct {
	Resource string
	Path     string
}

type captureSourceReference struct {
	resource  string
	source    source.Source
	directory bool
	selector  *skill.SkillSet
}

func localCaptureSourcePath(root string, value source.Source) (string, error) {
	local, present := value.Local()
	if !present || local.Mode() != source.LocalSourceModeVendor {
		return "", fmt.Errorf("capture update requires an explicit writable local vendor source")
	}
	path := local.Path()
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	return filepath.Clean(path), nil
}

func captureSourceReferences(ctx context.Context, paths daempaths.Paths, environment desired.Environment, referent string, budget *recovery.PhysicalWorkBudget) ([]HostConfigSourceReference, error) {
	referentKey, err := captureReferentKey(referent, budget)
	if err != nil {
		return nil, err
	}
	var candidates []captureSourceReference
	for _, config := range environment.HostConfigs() {
		candidates = append(candidates, captureSourceReference{resource: config.ID().String(), source: config.Source()})
	}
	for _, resource := range environment.Instructions() {
		candidates = append(candidates, captureSourceReference{resource: resource.ID().String(), source: resource.Source()})
	}
	for _, resource := range environment.Skills() {
		candidates = append(candidates, captureSourceReference{resource: resource.ID().String(), source: resource.Source(), directory: true})
	}
	for _, resource := range environment.HookAssets() {
		candidates = append(candidates, captureSourceReference{resource: resource.ID().String(), source: resource.Source()})
	}
	for index, selector := range environment.SkillSets() {
		candidates = append(candidates, captureSourceReference{resource: fmt.Sprintf("skill_group[%d]", index), source: selector.Source(), directory: true, selector: &selector})
	}
	for _, resource := range environment.Extensions() {
		interpreted, err := extensiontopology.InterpretCarrierSource(resource.CarrierKey())
		if err != nil {
			return nil, err
		}
		if interpreted.Class() != extensiontopology.CarrierSourceLocal {
			continue
		}
		home, err := paths.UserHomeDir()
		if err != nil {
			return nil, err
		}
		sourceContext, err := extensiontopology.NewLocalSourceContext(paths.ManifestRoot, home)
		if err != nil {
			return nil, err
		}
		local, err := interpreted.ResolveLocal(sourceContext)
		if err != nil {
			return nil, err
		}
		value, err := source.NewLocalSource(local.Path(), source.LocalSourceModeVendor)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, captureSourceReference{resource: resource.ID().String(), source: value, directory: true})
	}
	var result []HostConfigSourceReference
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		local, present := candidate.source.Local()
		if !present {
			continue
		}
		path := local.Path()
		if !filepath.IsAbs(path) {
			path = filepath.Join(paths.ManifestRoot, path)
		}
		physical, err := captureReferentKey(path, budget)
		if err != nil {
			return nil, fmt.Errorf("qualify local source reference %s: %w", candidate.resource, err)
		}
		if physical == referentKey || candidate.directory && capturePathContains(physical, referentKey) {
			if candidate.selector != nil {
				relative, err := filepath.Rel(physical, referentKey)
				if err != nil {
					return nil, err
				}
				// Equality keys can fold case; selector matching uses the access
				// spelling of the corresponding source-directory child.
				childPath := referent
				for range len(strings.Split(relative, string(filepath.Separator))) - 1 {
					childPath = filepath.Dir(childPath)
				}
				child := filepath.Base(childPath)
				selected, err := candidate.selector.Selects(child)
				if err != nil {
					return nil, err
				}
				if !selected {
					continue
				}
			}
			result = append(result, HostConfigSourceReference{Resource: candidate.resource, Path: filepath.Clean(path)})
		}
	}
	slices.SortFunc(result, func(left, right HostConfigSourceReference) int {
		return strings.Compare(left.Resource+"\x00"+left.Path, right.Resource+"\x00"+right.Path)
	})
	return result, nil
}

func captureReferentKey(path string, budget *recovery.PhysicalWorkBudget) (string, error) {
	physical, err := rootedpath.ResolveReferentPathBounded(path, recovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return "", err
	}
	observed, err := mutation.ObserveDirectoryEntryAuthorityBounded(physical, recovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return "", err
	}
	if exact, present := observed.Exact(); present {
		return exact.Key(), nil
	}
	missing, _ := observed.Provisional()
	return missing.CandidateKey(), nil
}

func capturePathContains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && (relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func captureSourceConfig(config hostconfig.Config, path string) (hostconfig.Config, error) {
	value, err := source.NewLocalSource(path, source.LocalSourceModeVendor)
	if err != nil {
		return hostconfig.Config{}, err
	}
	return hostconfig.New(hostconfig.Spec{Name: config.ID().Name(), Source: value, Target: config.Target(), Scope: config.Scope(), Path: config.Path(), Ownership: config.Ownership(), Syntax: config.Syntax()})
}
