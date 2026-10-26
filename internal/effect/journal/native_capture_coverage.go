package journal

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/topology"
)

func validateNativeCaptureContext(ctx context.Context, paths Paths, batch NativeDocumentObservationBatch, options CaptureOptions) error {
	manifest := batch.namespaces[0].roots.Manifest
	if manifest.Root == nil || filepath.Clean(paths.ManifestRoot) != manifest.SelectedPath {
		return fmt.Errorf("capture paths differ from the selected native manifest root")
	}
	declaration, err := mutation.CanonicalDirectoryEntryPathBounded(batch.owner.ManifestPath(), recovery.MaximumPhysicalPathDepth, batch.budget)
	if err != nil {
		return err
	}
	root, err := manifest.Root.AuthorityBounded(batch.budget)
	if err != nil {
		return err
	}
	provenance, err := root.Provenance()
	if err != nil {
		return err
	}
	if filepath.Dir(declaration) != provenance.PhysicalRoot() {
		return fmt.Errorf("native owner belongs to another manifest root")
	}
	if options.ManifestRoot != nil {
		selected, err := options.ManifestRoot.AuthorityBounded(batch.budget)
		if err != nil {
			return err
		}
		other, err := selected.Provenance()
		if err != nil || persistedNativeRoot(other) != persistedNativeRoot(provenance) {
			return fmt.Errorf("ordinary capture manifest root differs from the native operation root")
		}
	}
	state, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(paths.StatefilePath, recovery.MaximumPhysicalPathDepth, batch.budget)
	if err != nil {
		return err
	}
	if !state.Exact().Equal(batch.owner.StatefileAuthority()) {
		return fmt.Errorf("capture statefile differs from its observed native owner")
	}
	return ctx.Err()
}

func validateNativeCaptureWork(before, after durable.Snapshot, edits []compose.ProjectionEdit) error {
	projected, err := before.WithManagedAggregates(after.ManagedAggregates())
	if err != nil {
		return err
	}
	if !projected.Equal(after) {
		return fmt.Errorf("whole-document or non-document state changes require their capture integration")
	}

	contracts := make(map[documentCaptureContractKey]struct{})
	for _, edit := range edits {
		for _, contract := range edit.Participant().Selection().Contracts() {
			contracts[documentCaptureContractKey{address: contract.Address(), codec: contract.CodecContractID()}] = struct{}{}
		}
	}
	for side, snapshot := range []durable.Snapshot{before, after} {
		other := after
		if side == 1 {
			other = before
		}
		unchanged := make(map[topology.SubjectID]durable.ManagedAggregateState)
		for _, row := range other.ManagedAggregates() {
			unchanged[row.Subject()] = row
		}
		for _, row := range snapshot.ManagedAggregates() {
			if previous, found := unchanged[row.Subject()]; found && previous.Equal(row) {
				continue
			}
			contract := row.Contribution().Contract()
			if _, joined := contracts[documentCaptureContractKey{address: contract.Address(), codec: contract.CodecContractID()}]; !joined {
				return fmt.Errorf("changed aggregate %q has no selected capture edit", row.Subject())
			}
		}
	}
	return nil
}

func validateNativePublicationPartition(ctx context.Context, directory string, batch NativeDocumentObservationBatch) error {
	publication, err := mutation.ObserveDirectoryEntryAuthorityBounded(directory, recovery.MaximumPhysicalPathDepth, batch.budget)
	if err != nil {
		return err
	}
	var paths []documentWriterPath
	appendPath := func(path string, group int) {
		volume, components := recoveryFilesystemPathComponents(path)
		paths = append(paths, documentWriterPath{volume: volume, components: components, group: group})
	}
	appendPath(nativeDocumentObservationKey(publication), -1)
	appendPath(batch.owner.StatefileKey(), -2)
	for index, namespace := range batch.namespaces {
		appendPath(nativeDocumentObservationKey(namespace.observation), index)
		for _, alias := range namespace.aliases {
			appendPath(nativeDocumentObservationKey(alias.observation), index)
		}
	}
	return validateDocumentWriterPaths(ctx, paths)
}
