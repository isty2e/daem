package gitcli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

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
	identity    storagecommit.EntryIdentity
	root        *rootedpath.CapturedRoot
	path        string
}

type observationProbePathBudget struct{}

func (observationProbePathBudget) AdmitPathComponents(int) error {
	return nil
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
	identity, err := createObservationProbe(
		ctx,
		cacheRoot,
		destination,
		observationProbeAfterPublish(resolver),
	)
	if err != nil {
		return nil, err
	}
	probe := &originObservationRepository{
		resolver:    resolver,
		cacheRoot:   cacheRoot,
		destination: destination,
		identity:    identity,
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
	cleanupCtx := context.Background()
	if ctx != nil {
		cleanupCtx = context.WithoutCancel(ctx)
	}
	identity := probe.identity
	var closeErr error
	if probe.root != nil {
		// Git mutates the probe tree, so cleanup needs the descriptor-backed
		// post-mutation identity. This is not a path recapture: the held root
		// already owns the published object.
		if current, err := probe.heldWorkingDirectoryIdentity(cleanupCtx); err == nil {
			identity = current
		}
		closeErr = probe.root.Close()
		probe.root = nil
	}
	if probe.cacheRoot == nil {
		return closeErr
	}
	return errors.Join(closeErr, removeObservationProbe(cleanupCtx, probe.cacheRoot, probe.destination, identity))
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
	observed, err := workingDirectoryIdentity(ctx, root)
	if err != nil {
		_ = root.Close()
		return err
	}
	if !probe.identity.Equal(observed) {
		_ = root.Close()
		return fmt.Errorf("git locator observation repository was replaced")
	}
	probe.root = root
	probe.path = path
	return ctx.Err()
}

func (probe *originObservationRepository) heldWorkingDirectoryIdentity(
	ctx context.Context,
) (storagecommit.EntryIdentity, error) {
	if probe == nil || probe.root == nil {
		return storagecommit.EntryIdentity{}, fmt.Errorf("git locator observation authority is required")
	}
	return workingDirectoryIdentity(ctx, probe.root)
}

func workingDirectoryIdentity(
	ctx context.Context,
	root *rootedpath.CapturedRoot,
) (storagecommit.EntryIdentity, error) {
	capability, err := root.AcquireWorkingDirectory()
	if err != nil {
		return storagecommit.EntryIdentity{}, fmt.Errorf(
			"acquire git locator observation working directory: %w",
			err,
		)
	}
	observed, err := storagecommit.CaptureWorkingDirectoryIdentity(
		ctx,
		capability,
		observationProbePathBudget{},
	)
	closeErr := capability.Close()
	if err != nil {
		return storagecommit.EntryIdentity{}, errors.Join(
			fmt.Errorf("inspect git locator observation working directory: %w", err),
			closeErr,
		)
	}
	if closeErr != nil {
		return storagecommit.EntryIdentity{}, closeErr
	}
	return observed, nil
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

func observationProbeAfterPublish(resolver Resolver) func(string) {
	if resolver.state == nil {
		return nil
	}
	return resolver.state.testAfterObservationProbePublish
}

func createObservationProbe(
	ctx context.Context,
	cacheRoot *rootedpath.CapturedRoot,
	destination rootedpath.Destination,
	afterPublish func(string),
) (storagecommit.EntryIdentity, error) {
	capability, err := cacheRoot.Acquire(destination)
	if err != nil {
		return storagecommit.EntryIdentity{}, fmt.Errorf("acquire git locator observation destination: %w", err)
	}
	prepared, err := storagecommit.PrepareRootedTree(
		ctx,
		capability,
		func(writer mutationfs.RootedTreeWriter) error {
			return writer.SetRootMode(0o700)
		},
	)
	if err != nil {
		return storagecommit.EntryIdentity{}, fmt.Errorf("prepare git locator observation repository: %w", err)
	}
	outcome, identity, err := prepared.CommitWithPublishedIdentity(ctx)
	if err != nil {
		return storagecommit.EntryIdentity{}, errors.Join(
			fmt.Errorf("publish git locator observation repository: %w", err),
			cleanupPublishedObservationProbe(ctx, cacheRoot, destination, outcome, identity),
		)
	}
	if identity.Kind() != mutationfs.EntryKindDirectory {
		return storagecommit.EntryIdentity{}, errors.Join(
			fmt.Errorf("git locator observation repository is not a directory"),
			cleanupPublishedObservationProbe(ctx, cacheRoot, destination, outcome, identity),
		)
	}
	if afterPublish != nil {
		path, pathErr := destination.LexicalPath()
		if pathErr != nil {
			return storagecommit.EntryIdentity{}, errors.Join(
				pathErr,
				cleanupPublishedObservationProbe(ctx, cacheRoot, destination, outcome, identity),
			)
		}
		afterPublish(path)
	}
	return identity, nil
}

func cleanupPublishedObservationProbe(
	ctx context.Context,
	cacheRoot *rootedpath.CapturedRoot,
	destination rootedpath.Destination,
	outcome mutationfs.CommitOutcome,
	identity storagecommit.EntryIdentity,
) error {
	cleanupCtx := context.Background()
	if ctx != nil {
		cleanupCtx = context.WithoutCancel(ctx)
	}
	if identity.Kind() == mutationfs.EntryKindDirectory {
		return removeObservationProbe(cleanupCtx, cacheRoot, destination, identity)
	}
	switch outcome.State() {
	case mutationfs.CommitOutcomeComplete, mutationfs.CommitOutcomeIndeterminate:
		path, pathErr := destination.LexicalPath()
		if pathErr != nil {
			return fmt.Errorf("git locator observation repository publication retained residue")
		}
		return fmt.Errorf("git locator observation repository publication retained residue at %s", path)
	default:
		return nil
	}
}

func removeObservationProbe(
	ctx context.Context,
	cacheRoot *rootedpath.CapturedRoot,
	destination rootedpath.Destination,
	identity storagecommit.EntryIdentity,
) error {
	if cacheRoot == nil || identity.Kind() != mutationfs.EntryKindDirectory {
		return nil
	}
	capability, err := cacheRoot.Acquire(destination)
	if err != nil {
		return fmt.Errorf("acquire git locator observation cleanup: %w", err)
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
