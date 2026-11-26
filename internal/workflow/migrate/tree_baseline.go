package migrate

import (
	"bytes"
	"context"
	"errors"
	"io/fs"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal/qualification"
	"github.com/isty2e/daem/internal/effect/metadataconversion"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
)

// A digest conversion replaces one statefile atomically. Neither the baseline
// tree nor an installed tree participates as a write target.
func planTreeBaseline(ctx context.Context, paths daempaths.Paths, tree string, budget *physicalwork.Budget) (*PreparedMetadata, error) {
	authority, err := recoverygate.NewEffectAuthority(ctx, paths)
	if err != nil {
		return nil, err
	}
	if err := authority.Validate(ctx); err != nil {
		return nil, err
	}
	path, err := mutation.CanonicalDirectoryEntryPathBounded(paths.StatefilePath, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return nil, err
	}
	before, selected, err := statefile.LoadPhysicalFile(ctx, path)
	if err != nil {
		return nil, err
	}
	hashes, err := qualification.ReadBaselineTree(ctx, tree, budget)
	if err != nil {
		return nil, err
	}
	after, err := before.Snapshot().WithVerifiedTreeBaseline(hashes)
	if err != nil {
		return nil, err
	}
	disclosure := MetadataDisclosure{Disclosure: metadataconversion.Disclosure{ManifestPath: paths.ManifestPath, DataDir: paths.DataDir, Statefiles: []string{path}, Targets: []string{path}}, Action: "already_converted"}
	if before.Snapshot().Equal(after) {
		return &PreparedMetadata{execution: func(ctx context.Context) error { return ctx.Err() }, disclosure: disclosure}, nil
	}
	encoded, err := before.Codec().EncodeState(after)
	if err != nil {
		return nil, err
	}
	identity, original := selected.Identity(), selected.Content()
	domains := authority.Domains()
	for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
		domain, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{Path: path, Access: mutation.AccessExclusive, Effect: effect})
		if err != nil {
			return nil, err
		}
		domains = append(domains, domain)
	}
	disclosure.Action = "convert"
	execute := func(ctx context.Context) (resultErr error) {
		store, err := mutation.NewStore(paths.DataDir)
		if err != nil {
			return err
		}
		leases, err := store.Acquire(ctx, domains...)
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, leases.Release()) }()
		if err := authority.Validate(ctx); err != nil {
			return err
		}
		matches, err := leases.DomainsMatchCurrent(ctx)
		if err != nil {
			return err
		}
		if !matches {
			return mutation.StaleSnapshotError{}
		}
		current, err := storagecommit.ReadRegularFileSnapshotUpTo(ctx, path, int64(len(original)))
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, storagecommit.ErrRegularFileReadLimitExceeded) {
			return mutation.StaleSnapshotError{}
		}
		if err != nil {
			return err
		}
		if !mutationfs.SameEntryObject(identity, current.Identity()) || !bytes.Equal(original, current.Content()) {
			return mutation.StaleSnapshotError{}
		}
		return (storagecommit.Adapter{}).ReplaceFile(ctx, path, encoded, current.Mode(), current.Identity())
	}
	return &PreparedMetadata{execution: execute, disclosure: disclosure}, nil
}
