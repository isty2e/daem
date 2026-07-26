// Package hook owns command-Hook projection identities and the structural
// consumption relation from referenced HookAsset paths to Hook projections.
package hook

import (
	"fmt"
	"slices"
	"sort"

	"github.com/isty2e/daem/internal/desired/entity"
	desiredhook "github.com/isty2e/daem/internal/desired/hook"
	desiredhookasset "github.com/isty2e/daem/internal/desired/hookasset"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

const (
	CodexProjectProjectionNamespace  = "codex.project.hooks"
	CodexGlobalProjectionNamespace   = "codex.global.hooks"
	ClaudeProjectProjectionNamespace = "claude-code.project.hooks"
	ClaudeGlobalProjectionNamespace  = "claude-code.global.hooks"
	AssetProjectProjectionNamespace  = "hook-asset.project.data"
	AssetGlobalProjectionNamespace   = "hook-asset.global.data"
)

// Projection is one declared native Hook projection subject.
type Projection struct {
	entityID entity.ID
	subject  topology.SubjectID
	target   target.Target
	scope    target.Scope
}

// AssetProjection is one referenced HookAsset path projection subject.
type AssetProjection struct {
	entityID entity.ID
	subject  topology.SubjectID
	scope    target.Scope
}

// Model is one immutable complete Hook/HookAsset structural lowering.
type Model struct {
	graph       topology.Graph
	projections []Projection
	assets      []AssetProjection
}

// Lower constructs every declared projection subject and exact placeholder
// consumption edges. It does not choose placement, resolve content, or render
// host syntax.
func Lower(
	assets []desiredhookasset.HookAsset,
	hooks []desiredhook.Hook,
) (Model, error) {
	assetByName := make(map[string]desiredhookasset.HookAsset, len(assets))
	for index, asset := range assets {
		if err := asset.Validate(); err != nil {
			return Model{}, fmt.Errorf("hook_asset[%d]: %w", index, err)
		}
		name := asset.ID().Name()
		if _, duplicate := assetByName[name]; duplicate {
			return Model{}, fmt.Errorf("hook_asset[%d]: duplicate HookAsset identity %q", index, asset.ID())
		}
		assetByName[name] = asset
	}

	projectionBySubject := make(map[topology.SubjectID]Projection)
	assetBySubject := make(map[topology.SubjectID]AssetProjection)
	edges := make(map[struct {
		source topology.SubjectID
		target topology.SubjectID
	}]struct{})

	for hookIndex, value := range hooks {
		if err := value.Validate(); err != nil {
			return Model{}, fmt.Errorf("hook[%d]: %w", hookIndex, err)
		}
		references := value.AssetReferences()
		for _, selectedTarget := range value.Targets() {
			_, admitted := ProjectionNamespace(selectedTarget, value.Scope())
			if !admitted {
				if len(references) != 0 {
					return Model{}, fmt.Errorf(
						"hook %q target %q: hook asset placeholders require a supported Codex or Claude Code command hook target",
						value.ID().Name(), selectedTarget,
					)
				}
				continue
			}
			hookSubject, err := ProjectionSubjectID(value.ID(), selectedTarget, value.Scope())
			if err != nil {
				return Model{}, fmt.Errorf("lower hook %q target %q: %w", value.ID().Name(), selectedTarget, err)
			}
			if _, duplicate := projectionBySubject[hookSubject]; duplicate {
				return Model{}, fmt.Errorf("duplicate Hook projection subject %q", hookSubject)
			}
			projectionBySubject[hookSubject] = Projection{
				entityID: value.ID(), subject: hookSubject, target: selectedTarget, scope: value.Scope(),
			}

			for _, reference := range references {
				asset, err := referencedAsset(value, selectedTarget, reference, assetByName)
				if err != nil {
					return Model{}, err
				}
				assetSubject, err := assetSubject(asset)
				if err != nil {
					return Model{}, err
				}
				assetBySubject[assetSubject] = AssetProjection{
					entityID: asset.ID(), subject: assetSubject, scope: asset.Scope(),
				}
				edges[struct {
					source topology.SubjectID
					target topology.SubjectID
				}{source: assetSubject, target: hookSubject}] = struct{}{}
			}
		}
	}

	projections := make([]Projection, 0, len(projectionBySubject))
	for _, projection := range projectionBySubject {
		projections = append(projections, projection)
	}
	sort.Slice(projections, func(left int, right int) bool {
		return topology.CompareSubjectID(projections[left].subject, projections[right].subject) < 0
	})
	assetProjections := make([]AssetProjection, 0, len(assetBySubject))
	for _, asset := range assetBySubject {
		assetProjections = append(assetProjections, asset)
	}
	sort.Slice(assetProjections, func(left int, right int) bool {
		return topology.CompareSubjectID(assetProjections[left].subject, assetProjections[right].subject) < 0
	})
	subjects := make([]topology.SubjectID, 0, len(projections)+len(assetProjections))
	for _, projection := range projections {
		subjects = append(subjects, projection.subject)
	}
	for _, asset := range assetProjections {
		subjects = append(subjects, asset.subject)
	}
	relations := make([]topology.Edge, 0, len(edges))
	for edge := range edges {
		relations = append(relations, topology.NewEdge(topology.EdgeConsumedBy, edge.source, edge.target))
	}
	graph, err := topology.NewGraph(subjects, relations)
	if err != nil {
		return Model{}, fmt.Errorf("lower Hook topology: %w", err)
	}
	return Model{graph: graph, projections: projections, assets: assetProjections}, nil
}

// ProjectionNamespace returns the canonical collision domain for one admitted
// native Hook target and scope.
func ProjectionNamespace(selectedTarget target.Target, scope target.Scope) (string, bool) {
	switch {
	case selectedTarget == target.TargetCodex && scope == target.ScopeProject:
		return CodexProjectProjectionNamespace, true
	case selectedTarget == target.TargetCodex && scope == target.ScopeGlobal:
		return CodexGlobalProjectionNamespace, true
	case selectedTarget == target.TargetClaudeCode && scope == target.ScopeProject:
		return ClaudeProjectProjectionNamespace, true
	case selectedTarget == target.TargetClaudeCode && scope == target.ScopeGlobal:
		return ClaudeGlobalProjectionNamespace, true
	default:
		return "", false
	}
}

func assetProjectionNamespace(scope target.Scope) (string, error) {
	switch scope {
	case target.ScopeProject:
		return AssetProjectProjectionNamespace, nil
	case target.ScopeGlobal:
		return AssetGlobalProjectionNamespace, nil
	default:
		return "", fmt.Errorf("unsupported HookAsset scope %q", scope)
	}
}

func assetSubject(asset desiredhookasset.HookAsset) (topology.SubjectID, error) {
	if err := asset.Validate(); err != nil {
		return topology.SubjectID{}, err
	}
	return AssetSubjectID(asset.ID(), asset.Scope())
}

// ProjectionSubjectID constructs one admitted Hook projection identity from
// canonical entity, target, and scope facts.
func ProjectionSubjectID(id entity.ID, selectedTarget target.Target, scope target.Scope) (topology.SubjectID, error) {
	if err := id.Validate(); err != nil {
		return topology.SubjectID{}, err
	}
	if id.Kind() != entity.KindHook {
		return topology.SubjectID{}, fmt.Errorf("Hook projection requires Hook entity identity")
	}
	namespace, admitted := ProjectionNamespace(selectedTarget, scope)
	if !admitted {
		return topology.SubjectID{}, fmt.Errorf("target %q scope %q has no native Hook projection", selectedTarget, scope)
	}
	return topologyprojection.Subject(id, namespace)
}

// AssetSubjectID constructs one admitted HookAsset path identity from
// canonical entity and scope facts.
func AssetSubjectID(id entity.ID, scope target.Scope) (topology.SubjectID, error) {
	if err := id.Validate(); err != nil {
		return topology.SubjectID{}, err
	}
	if id.Kind() != entity.KindHookAsset {
		return topology.SubjectID{}, fmt.Errorf("HookAsset projection requires HookAsset entity identity")
	}
	namespace, err := assetProjectionNamespace(scope)
	if err != nil {
		return topology.SubjectID{}, err
	}
	return topologyprojection.Subject(id, namespace)
}

func referencedAsset(
	value desiredhook.Hook,
	selectedTarget target.Target,
	reference desiredhook.AssetReference,
	assets map[string]desiredhookasset.HookAsset,
) (desiredhookasset.HookAsset, error) {
	asset, present := assets[reference.ID()]
	if !present {
		return desiredhookasset.HookAsset{}, fmt.Errorf(
			"hook %q target %q: hook asset %q is not declared",
			value.ID().Name(), selectedTarget, reference.ID(),
		)
	}
	if asset.Scope() != value.Scope() {
		return desiredhookasset.HookAsset{}, fmt.Errorf(
			"hook %q target %q: hook asset %q scope %q does not match hook scope %q",
			value.ID().Name(), selectedTarget, asset.ID().Name(), asset.Scope(), value.Scope(),
		)
	}
	if asset.ArtifactKind() != desiredhookasset.ArtifactKindFile {
		return desiredhookasset.HookAsset{}, fmt.Errorf(
			"hook %q target %q: hook asset %q kind %q is unsupported",
			value.ID().Name(), selectedTarget, asset.ID().Name(), asset.ArtifactKind(),
		)
	}
	return asset, nil
}

func (model Model) Projections() []Projection {
	return append([]Projection(nil), model.projections...)
}

func (model Model) AssetProjections() []AssetProjection {
	return append([]AssetProjection(nil), model.assets...)
}

// AssetSubjectsOf returns HookAsset path subjects consumed by one Hook
// projection subject.
func (model Model) AssetSubjectsOf(hookSubject topology.SubjectID) []topology.SubjectID {
	return model.graph.ConsumedSubjectsOf(hookSubject)
}

func (model Model) consumerSubjectsOf(assetSubject topology.SubjectID) []topology.SubjectID {
	return model.graph.ConsumersOf(assetSubject)
}

// ConsumerTargetsOf returns the canonical target set consuming one HookAsset
// path subject.
func (model Model) ConsumerTargetsOf(assetSubject topology.SubjectID) []target.Target {
	consumers := model.consumerSubjectsOf(assetSubject)
	targets := make(map[target.Target]struct{}, len(consumers))
	for _, consumer := range consumers {
		for _, projection := range model.projections {
			if projection.subject == consumer {
				targets[projection.target] = struct{}{}
				break
			}
		}
	}
	result := make([]target.Target, 0, len(targets))
	for selectedTarget := range targets {
		result = append(result, selectedTarget)
	}
	slices.Sort(result)
	return result
}

func (projection Projection) EntityID() entity.ID           { return projection.entityID }
func (projection Projection) SubjectID() topology.SubjectID { return projection.subject }
func (projection Projection) Target() target.Target         { return projection.target }
func (projection Projection) Scope() target.Scope           { return projection.scope }

func (projection AssetProjection) EntityID() entity.ID           { return projection.entityID }
func (projection AssetProjection) SubjectID() topology.SubjectID { return projection.subject }
func (projection AssetProjection) Scope() target.Scope           { return projection.scope }
