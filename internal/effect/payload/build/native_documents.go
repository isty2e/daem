package build

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/effect/payload"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

type nativeDocumentPayloadInput struct {
	config  hostconfig.Config
	locked  lock.LockedSubjectContract
	subject topology.SubjectID
}

// NativeDocumentPayloadSet selects native document preparation without granting host-write authority.
func NativeDocumentPayloadSet(ctx context.Context, input Input) (payload.PayloadSet, error) {
	if ctx == nil {
		return payload.PayloadSet{}, fmt.Errorf("native document preparation context is required")
	}
	if err := ctx.Err(); err != nil {
		return payload.PayloadSet{}, err
	}
	if err := input.Environment.Validate(); err != nil {
		return payload.PayloadSet{}, err
	}
	if err := lock.ValidateNative(input.Lockfile); err != nil {
		return payload.PayloadSet{}, err
	}
	ordinary, documents, err := preflightNativeDocumentPayloads(ctx, input)
	if err != nil {
		return payload.PayloadSet{}, err
	}
	return payloadSet(ctx, ordinary, documents)
}

func preflightNativeDocumentPayloads(ctx context.Context, input Input) (Input, []nativeDocumentPayloadInput, error) {
	ordinary := input
	ordinary.ManagedPathPayloadSubjects = nil
	required := make(map[entity.ID]topology.SubjectID)
	for _, subject := range input.ManagedPathPayloadSubjects {
		if err := ctx.Err(); err != nil {
			return Input{}, nil, err
		}
		id, present := topologyprojection.EntityID(subject)
		if !present || id.Kind() != entity.KindHostConfig {
			ordinary.ManagedPathPayloadSubjects = append(ordinary.ManagedPathPayloadSubjects, subject)
			continue
		}
		if _, duplicate := required[id]; duplicate {
			return Input{}, nil, fmt.Errorf("duplicate native document payload request for %q", id)
		}
		required[id] = subject
	}
	inputs := make([]nativeDocumentPayloadInput, 0, len(required))
	for _, config := range input.Environment.HostConfigs() {
		subject, selected := required[config.ID()]
		if !selected {
			continue
		}
		if err := ctx.Err(); err != nil {
			return Input{}, nil, err
		}
		placement, err := profile.ResolveHostConfigPlacement(profile.HostConfigPlacementInput{
			Target: config.Target(), Scope: config.Scope(), Path: config.Path(), Ownership: config.Ownership(), Syntax: config.Syntax(),
		})
		if err != nil {
			return Input{}, nil, err
		}
		expected, err := lock.NewHostConfigDocumentSubjectContract(config.ID(), placement)
		if err != nil {
			return Input{}, nil, err
		}
		projection, present := input.Lockfile.Locked.Subject(subject)
		if !present || !projection.Equal(expected) {
			return Input{}, nil, fmt.Errorf("native document %q projection does not match declaration", config.ID())
		}
		locked, present := input.Lockfile.Locked.ExactSupplySubject(config.ID())
		if !present {
			return Input{}, nil, fmt.Errorf("native document %q exact source is missing", config.ID())
		}
		lockedPlacement, present := locked.HostConfigPlacement()
		if !present || lockedPlacement != placement {
			return Input{}, nil, fmt.Errorf("native document %q placement does not match declaration", config.ID())
		}
		declaredSource, err := source.SourceIDFor(config.Source())
		if err != nil {
			return Input{}, nil, err
		}
		identity, present := locked.ExactSupply()
		if !present || identity.SourceID() != declaredSource {
			return Input{}, nil, fmt.Errorf("native document %q source does not match declaration", config.ID())
		}
		inputs = append(inputs, nativeDocumentPayloadInput{config: config, locked: locked, subject: subject})
		delete(required, config.ID())
	}
	if len(required) != 0 {
		return Input{}, nil, fmt.Errorf("required native document has no declaration")
	}
	return ordinary, inputs, nil
}

func buildNativeDocumentPayloads(ctx context.Context, resolvers *sourceResolverOnce, inputs []nativeDocumentPayloadInput) ([]payload.Payload, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	resolver, err := resolvers.get()
	if err != nil {
		return nil, err
	}
	values := make([]payload.Payload, 0, len(inputs))
	for _, input := range inputs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		materialized, err := materializeLockedFile(ctx, resolver, input.config.Source(), input.locked, false)
		if err != nil {
			return nil, fmt.Errorf("native document %q: %w", input.config.ID(), err)
		}
		value, err := payload.NewFilePayload(input.subject, materialized.content.Bytes(), 0o600)
		if err != nil {
			return nil, err
		}
		if value.Hash() != materialized.transformation.OutputIdentity().ContentHash() {
			return nil, fmt.Errorf("native document %q payload does not match materialized output", input.config.ID())
		}
		values = append(values, value)
	}
	return values, nil
}
