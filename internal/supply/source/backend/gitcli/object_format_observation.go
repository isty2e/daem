package gitcli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/supply/source"
)

const (
	observationProbeNamespace      = "probes"
	observationProbeIDBytes        = 16
	observationProbeMaximumEntries = 10_000
	observationProbeMaximumDepth   = 16
	observationProbeMaximumBytes   = 32 << 20
)

type originObservationRepository struct {
	resolver    Resolver
	cacheRoot   *rootedpath.CapturedRoot
	destination rootedpath.Destination
	root        *rootedpath.CapturedRoot
	path        string
}

func (resolver Resolver) openOriginObservationRepository(
	ctx context.Context,
	cacheRoot *rootedpath.CapturedRoot,
	locator source.GitLocator,
) (*originObservationRepository, error) {
	if cacheRoot == nil {
		return nil, fmt.Errorf("git source cache root authority is required")
	}
	destination, err := newObservationProbeDestination(cacheRoot)
	if err != nil {
		return nil, err
	}
	if err := createObservationProbe(ctx, cacheRoot, destination); err != nil {
		return nil, err
	}
	probe := &originObservationRepository{
		resolver:    resolver,
		cacheRoot:   cacheRoot,
		destination: destination,
	}
	if err := probe.capture(ctx, destination); err != nil {
		return nil, errors.Join(err, probe.Close(ctx))
	}
	if err := probe.declareOrigin(ctx, locator); err != nil {
		return nil, errors.Join(err, probe.Close(ctx))
	}
	return probe, nil
}

func (probe *originObservationRepository) Close(ctx context.Context) error {
	if probe == nil {
		return nil
	}
	cleanupCtx := ctx
	if ctx != nil {
		cleanupCtx = context.WithoutCancel(ctx)
	} else {
		cleanupCtx = context.Background()
	}
	var closeErr error
	if probe.root != nil {
		closeErr = probe.root.Close()
		probe.root = nil
	}
	if probe.cacheRoot == nil {
		return closeErr
	}
	return errors.Join(closeErr, removeObservationProbe(cleanupCtx, probe.cacheRoot, probe.destination))
}

func (probe *originObservationRepository) capture(
	ctx context.Context,
	destination rootedpath.Destination,
) error {
	path, err := destination.LexicalPath()
	if err != nil {
		return fmt.Errorf("resolve git locator observation path: %w", err)
	}
	root, err := rootedpath.CaptureRootNoFollow(path)
	if err != nil {
		return fmt.Errorf("capture git locator observation authority: %w", err)
	}
	probe.root = root
	probe.path = path
	return ctx.Err()
}

func (probe *originObservationRepository) declareOrigin(
	ctx context.Context,
	locator source.GitLocator,
) error {
	explicit, err := probe.resolver.explicitObjectFormatSupported(ctx)
	if err != nil {
		return err
	}
	if err := runGitAtCapturedRepository(
		ctx,
		probe.root,
		probe.path,
		initializeBareRepositoryArgs(gitObjectFormatSHA1, explicit)...,
	); err != nil {
		return fmt.Errorf("initialize git locator observation repository: %w", err)
	}
	if err := runGitAtCapturedRepository(
		ctx,
		probe.root,
		probe.path,
		addOriginArgs(locator.String())...,
	); err != nil {
		return fmt.Errorf("declare git locator observation origin: %w", err)
	}
	output, err := gitOutputAtCapturedRepository(
		ctx,
		probe.root,
		probe.path,
		inspectEffectiveOriginArgs()...,
	)
	if err != nil {
		return fmt.Errorf("inspect effective git source origin: %w", err)
	}
	effectiveValue, ok := trimSingleGitConfigValue(output)
	if !ok {
		return fmt.Errorf("effective git source origin must contain exactly one canonical locator")
	}
	effective, err := source.ParseGitLocator(effectiveValue)
	if err != nil || !locator.Equivalent(effective) {
		return fmt.Errorf("effective git source origin does not match the declared locator")
	}
	return nil
}

func newObservationProbeDestination(cacheRoot *rootedpath.CapturedRoot) (rootedpath.Destination, error) {
	id, err := newObservationProbeID()
	if err != nil {
		return rootedpath.Destination{}, err
	}
	relative, err := rootedpath.NewRelativeDestination(observationProbeNamespace + "/" + id)
	if err != nil {
		return rootedpath.Destination{}, err
	}
	authority, err := cacheRoot.Authority()
	if err != nil {
		return rootedpath.Destination{}, fmt.Errorf("inspect git source cache root: %w", err)
	}
	return authority.Bind(relative)
}

func newObservationProbeID() (string, error) {
	var raw [observationProbeIDBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("create git locator observation identity: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func createObservationProbe(
	ctx context.Context,
	cacheRoot *rootedpath.CapturedRoot,
	destination rootedpath.Destination,
) error {
	capability, err := cacheRoot.Acquire(destination)
	if err != nil {
		return fmt.Errorf("acquire git locator observation destination: %w", err)
	}
	prepared, err := storagecommit.PrepareRootedTree(
		ctx,
		capability,
		func(writer mutationfs.RootedTreeWriter) error {
			return writer.SetRootMode(0o700)
		},
	)
	if err != nil {
		return fmt.Errorf("prepare git locator observation repository: %w", err)
	}
	if err := prepared.Commit(ctx); err != nil {
		return fmt.Errorf("publish git locator observation repository: %w", err)
	}
	return nil
}

func removeObservationProbe(
	ctx context.Context,
	cacheRoot *rootedpath.CapturedRoot,
	destination rootedpath.Destination,
) error {
	if cacheRoot == nil {
		return nil
	}
	capability, err := cacheRoot.Acquire(destination)
	if err != nil {
		return fmt.Errorf("acquire git locator observation cleanup: %w", err)
	}
	identity, err := storagecommit.CaptureRootedEntryIdentity(ctx, capability)
	if err != nil {
		_ = capability.Close()
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("inspect git locator observation repository: %w", err)
	}
	request, err := storagecommit.NewRootedEntryCleanup(
		capability,
		identity,
		observationProbeTraversalLimits(),
	)
	if err != nil {
		_ = capability.Close()
		return fmt.Errorf("prepare git locator observation cleanup: %w", err)
	}
	_, err = storagecommit.CommitRootedEntryCleanup(ctx, request)
	if err != nil {
		return fmt.Errorf("remove git locator observation repository: %w", err)
	}
	return nil
}

func observationProbeTraversalLimits() mutationfs.TreeTraversalLimits {
	limits, err := mutationfs.NewTreeTraversalLimits(
		observationProbeMaximumEntries,
		observationProbeMaximumDepth,
		observationProbeMaximumBytes,
	)
	if err != nil {
		panic(err)
	}
	return limits
}
