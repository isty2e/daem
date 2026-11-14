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
	"github.com/isty2e/daem/internal/effect/physicalwork"
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

func captureSourceReferences(ctx context.Context, paths daempaths.Paths, environment desired.Environment, referent string, budget *physicalwork.Budget) ([]HostConfigSourceReference, error) {
	referentPath, err := captureReferent(referent, budget)
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
		physical, err := captureReferent(path, budget)
		if err != nil {
			return nil, fmt.Errorf("qualify local source reference %s: %w", candidate.resource, err)
		}
		equal := physical.observed.Equal(referentPath.observed)
		if equal || candidate.directory && physical.contains(referentPath) {
			if candidate.selector != nil && !equal {
				child, stable := physical.sourceChild(referentPath)
				if !stable {
					return nil, fmt.Errorf("source reference %s has an unprovable selector child", candidate.resource)
				}
				selected, err := candidate.selector.Selects(child)
				if err != nil {
					return nil, err
				}
				if !selected {
					continue
				}
			}
			result = append(result, HostConfigSourceReference{Resource: candidate.resource, Path: filepath.Clean(path)})
		} else if physical.mayOverlap(referentPath) {
			return nil, fmt.Errorf("source reference %s has possible overlap without a qualified sharing relation", candidate.resource)
		}
	}
	slices.SortFunc(result, func(left, right HostConfigSourceReference) int {
		return strings.Compare(left.Resource+"\x00"+left.Path, right.Resource+"\x00"+right.Path)
	})
	return result, nil
}

func captureSourceConfig(config hostconfig.Config, path string) (hostconfig.Config, error) {
	value, err := source.NewLocalSource(path, source.LocalSourceModeVendor)
	if err != nil {
		return hostconfig.Config{}, err
	}
	return hostconfig.New(hostconfig.Spec{Name: config.ID().Name(), Source: value, Target: config.Target(), Scope: config.Scope(), Path: config.Path(), Ownership: config.Ownership(), Syntax: config.Syntax()})
}
