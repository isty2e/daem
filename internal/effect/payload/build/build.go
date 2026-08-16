package build

import (
	"context"
	"errors"

	"github.com/isty2e/daem/internal/desired"
	lock "github.com/isty2e/daem/internal/realization/lock"

	"github.com/isty2e/daem/internal/effect/payload"
	daempaths "github.com/isty2e/daem/internal/paths"
	sourceresolution "github.com/isty2e/daem/internal/supply/source/resolution"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/topology"
)

// Input carries canonical inputs needed to materialize host output payloads.
type Input struct {
	Paths       daempaths.Paths
	Environment desired.Environment
	Lockfile    lock.File
	Selection   targetselection.Selection
	// ManagedPathPayloadSubjects names only create/replace effects that require
	// exact payload bytes. Family adapters remain private to this composition root.
	ManagedPathPayloadSubjects []topology.SubjectID
}

// PayloadSet materializes selected instruction, skill, and hook host payloads.
func PayloadSet(ctx context.Context, input Input) (result payload.PayloadSet, resultErr error) {
	var values []payload.Payload
	var cleanups []func() error
	committed := false
	defer func() {
		if !committed {
			resultErr = errors.Join(resultErr, runCleanups(cleanups))
		}
	}()

	managedPaths, err := buildManagedPathPayloads(ctx, input)
	cleanups = append(cleanups, managedPaths.cleanups...)
	if err != nil {
		return payload.PayloadSet{}, err
	}
	values = append(values, managedPaths.payloads...)

	set, err := payload.NewPayloadSet(values, cleanups)
	if err != nil {
		return payload.PayloadSet{}, err
	}
	committed = true
	return set, nil
}

type managedPathPayloads struct {
	payloads []payload.Payload
	cleanups []func() error
}

func buildManagedPathPayloads(ctx context.Context, input Input) (managedPathPayloads, error) {
	resolvers := sourceResolverOnce{paths: input.Paths}
	instructionPayloads, err := buildInstructionPayloads(
		ctx,
		&resolvers,
		input.Environment.Instructions(),
		input.Lockfile,
		input.ManagedPathPayloadSubjects,
	)
	if err != nil {
		return managedPathPayloads{}, err
	}

	skillPayloads, err := buildSkillPayloads(
		ctx,
		&resolvers,
		input.Environment.Skills(),
		input.Lockfile,
		input.Selection,
		input.ManagedPathPayloadSubjects,
	)
	result := managedPathPayloads{
		payloads: append(instructionPayloads, skillPayloads.payloads...),
		cleanups: skillPayloads.cleanups,
	}
	if err != nil {
		return result, err
	}
	hookAssetPayloads, hookAssetErr := buildHookAssetPayloads(
		ctx,
		&resolvers,
		input.Environment.HookAssets(),
		input.Lockfile,
		input.ManagedPathPayloadSubjects,
	)
	if hookAssetErr != nil {
		return result, hookAssetErr
	}
	result.payloads = append(result.payloads, hookAssetPayloads...)
	return result, nil
}

// sourceResolverOnce preserves family validation order while sharing one
// operation-local resolver across every payload family that needs Supply.
type sourceResolverOnce struct {
	paths       daempaths.Paths
	resolver    sourceresolution.Resolver
	initialized bool
	err         error
}

func (source *sourceResolverOnce) get() (sourceresolution.Resolver, error) {
	if !source.initialized {
		source.resolver, source.err = sourceresolution.NewResolver(source.paths)
		source.initialized = true
	}
	return source.resolver, source.err
}

func runCleanups(cleanups []func() error) error {
	var cleanupErrors []error
	for index := len(cleanups) - 1; index >= 0; index-- {
		if cleanups[index] != nil {
			cleanupErrors = append(cleanupErrors, cleanups[index]())
		}
	}
	return errors.Join(cleanupErrors...)
}
