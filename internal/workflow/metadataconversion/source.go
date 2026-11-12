// Package metadataconversion coordinates whole-registry metadata format changes.
// Recorded paths and compiled plans are evidence, not mutation authority.
package metadataconversion

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/fileset"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
)

const maximumMetadataBytes int64 = 16 << 20

type sourceKind string

const (
	stateSource     sourceKind = "state"
	ownershipSource sourceKind = "ownership"
	carrierSource   sourceKind = "carrier"
)

type sourceEntry struct {
	image   fileset.PublicationImage
	content []byte
}

type sourceReader func(context.Context, string, sourceKind) (sourceEntry, error)

func liveSources(budget fileset.PhysicalWorkBudget) sourceReader {
	return func(ctx context.Context, path string, kind sourceKind) (sourceEntry, error) {
		depth, err := metadataPathDepth(path)
		if err != nil {
			return sourceEntry{}, err
		}
		if err := budget.AdmitPhysicalWork(depth, 0, 0); err != nil {
			return sourceEntry{}, err
		}
		info, statErr := os.Lstat(path)
		if err := ctx.Err(); err != nil {
			return sourceEntry{}, err
		}
		if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
			return sourceEntry{}, statErr
		}
		var size int64
		if statErr == nil {
			size = info.Size()
			if !info.Mode().IsRegular() || size < 0 || size > maximumMetadataBytes {
				return sourceEntry{}, fmt.Errorf("metadata %q is not a bounded regular file", path)
			}
		}
		work, err := storagecommit.MaximumFileSnapshotWork(depth, size)
		if err != nil {
			return sourceEntry{}, err
		}
		if err := budget.AdmitPhysicalWork(work.PathWork(), work.EntryWork(), work.ByteWork()); err != nil {
			return sourceEntry{}, err
		}
		loaded, err := storagecommit.ReadRegularFileSnapshotUpTo(ctx, path, size)
		if cancellation := ctx.Err(); cancellation != nil {
			return sourceEntry{}, errors.Join(cancellation, err)
		}
		if errors.Is(err, fs.ErrNotExist) {
			return sourceEntry{image: fileset.AbsentPublicationImage()}, nil
		}
		if err != nil {
			return sourceEntry{}, err
		}
		content := loaded.Content()
		image, err := fileset.NewPublicationImage(content, loaded.Mode().Perm())
		if err != nil {
			return sourceEntry{}, err
		}
		entry := sourceEntry{image: image, content: content}
		return entry, validateSourceEntry(kind, entry)
	}
}

func validateSourceEntry(kind sourceKind, entry sourceEntry) error {
	if err := entry.image.Validate(); err != nil {
		return err
	}
	if !entry.image.Exists() {
		if len(entry.content) != 0 {
			return fmt.Errorf("absent metadata carries content")
		}
		return nil
	}
	if entry.image.Size() > maximumMetadataBytes {
		return fmt.Errorf("metadata exceeds %d bytes", maximumMetadataBytes)
	}
	if kind == stateSource && entry.image.Mode() != 0o600 {
		return fmt.Errorf("statefile permissions must be 0600")
	}
	if entry.image.Mode()&0o077 != 0 {
		return fmt.Errorf("metadata permissions expose authority metadata")
	}
	actual, err := fileset.NewPublicationImage(entry.content, entry.image.Mode())
	if err != nil {
		return err
	}
	if !actual.Equal(entry.image) {
		return fmt.Errorf("metadata content differs from its admitted image")
	}
	return nil
}

func metadataPathDepth(path string) (int, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return 0, fmt.Errorf("metadata requires a clean absolute namespace")
	}
	root := filepath.VolumeName(path) + string(filepath.Separator)
	depth := len(strings.Split(strings.TrimPrefix(path, root), string(filepath.Separator)))
	if path == root || depth > mutationfs.MaximumPhysicalPathDepth {
		return 0, fmt.Errorf("metadata namespace exceeds admitted physical depth")
	}
	return depth, nil
}

func metadataVersion(content []byte) (int, error) {
	return jsonstrict.ValidateVersionedObject(content, "metadata conversion source", 64)
}

func stateFacts(ctx context.Context, content []byte) (durable.Snapshot, pathauthority.Reference, error) {
	version, err := metadataVersion(content)
	if err != nil {
		return durable.Snapshot{}, pathauthority.Reference{}, err
	}
	if version == 11 {
		image, err := statefile.UnmarshalImage(content)
		return image.Snapshot(), image.Reference(), err
	}
	snapshot, err := statefile.DecodePersistedFacts(ctx, content)
	return snapshot, pathauthority.Reference{}, err
}

func ownershipFacts(entry sourceEntry) (ownership.Registry, []pathauthority.Reference, error) {
	if !entry.image.Exists() {
		registry, err := ownership.NewRegistry(nil)
		return registry, nil, err
	}
	version, err := metadataVersion(entry.content)
	if err != nil {
		return ownership.Registry{}, nil, err
	}
	if version == 5 {
		image, err := ownershipstore.UnmarshalImage(entry.content)
		return image.Registry(), image.References(), err
	}
	registry, err := ownershipstore.DecodeRegistryFacts(entry.content)
	return registry, nil, err
}

func carrierFacts(entry sourceEntry) (durablecarrier.GlobalCarrierClaims, []pathauthority.Reference, error) {
	if !entry.image.Exists() {
		return durablecarrier.EmptyGlobalCarrierClaims(), nil, nil
	}
	version, err := metadataVersion(entry.content)
	if err != nil {
		return durablecarrier.GlobalCarrierClaims{}, nil, err
	}
	if version == 3 {
		image, err := carrierclaim.UnmarshalImage(entry.content)
		return image.Registry(), image.References(), err
	}
	registry, err := carrierclaim.DecodeRegistryFacts(entry.content)
	return registry, nil, err
}
