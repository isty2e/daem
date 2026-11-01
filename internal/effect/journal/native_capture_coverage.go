package journal

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func (batch NativeDocumentObservationBatch) manifest(ctx context.Context) (nativeDocumentRootGrant, error) {
	roots, err := batch.roots.qualify(ctx, batch.budget)
	if err != nil {
		return nativeDocumentRootGrant{}, err
	}
	manifest, present := roots[output.RootProject]
	if !present {
		return nativeDocumentRootGrant{}, fmt.Errorf("native observation requires its selected manifest root")
	}
	return manifest, nil
}

func validateNativeCaptureContext(ctx context.Context, paths Paths, batch NativeDocumentObservationBatch, options CaptureOptions) error {
	manifest, err := batch.manifest(ctx)
	if err != nil {
		return err
	}
	if filepath.Clean(paths.ManifestRoot) != manifest.selection.SelectedPath {
		return fmt.Errorf("capture paths differ from the selected native manifest root")
	}
	provenance := manifest.provenance
	declaration, err := mutation.CanonicalDirectoryEntryPathBounded(batch.owner.ManifestPath(), recovery.MaximumPhysicalPathDepth, batch.budget)
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

func validateNativeCaptureWork(before, after durable.Snapshot, edits []compose.ProjectionEdit, mutations []pathMutation, completions []durablecarrier.ManagedCarrierClaim) error {
	projected, err := before.WithManagedPaths(after.ManagedPaths())
	if err != nil {
		return err
	}
	projected, err = projected.WithManagedAggregates(after.ManagedAggregates())
	if err != nil {
		return err
	}
	projected, _, err = projected.WithPromotedCarrierClaims(completions)
	if err != nil {
		return err
	}
	if err := before.ValidateNativeDocumentCommitTransition(after); err != nil {
		return err
	}
	if before.NativeDocumentCommit() != after.NativeDocumentCommit() {
		projected, err = projected.AdvanceNativeDocumentCommit()
		if err != nil {
			return err
		}
	}
	if !projected.Equal(after) {
		return fmt.Errorf("non-document and non-resource state changes require their capture integration")
	}

	contracts := make(map[documentCaptureContractKey]struct{})
	paths := make(map[topology.SubjectID]struct{})
	for _, edit := range edits {
		for _, contract := range edit.Participant().Selection().Contracts() {
			contracts[documentCaptureContractKey{address: contract.Address(), codec: contract.CodecContractID()}] = struct{}{}
		}
	}
	for _, mutation := range mutations {
		if mutation.AggregateContract != nil {
			contract := mutation.AggregateContract
			contracts[documentCaptureContractKey{address: contract.Address(), codec: contract.CodecContractID()}] = struct{}{}
			continue
		}
		paths[mutation.Subject] = struct{}{}
		if mutation.PreviousState != nil {
			paths[mutation.PreviousState.Subject] = struct{}{}
		}
	}
	for side, snapshot := range []durable.Snapshot{before, after} {
		other := after
		if side == 1 {
			other = before
		}
		unchangedPaths := make(map[topology.SubjectID]durable.ManagedPathState)
		for _, row := range other.ManagedPaths() {
			unchangedPaths[row.Subject()] = row
		}
		for _, row := range snapshot.ManagedPaths() {
			if _, retained := unchangedPaths[row.Subject()]; side == 0 && !retained && aggregate.IsHostConfigDocumentSubject(row.Subject()) {
				continue
			}
			if previous, found := unchangedPaths[row.Subject()]; found && previous.Equal(row) {
				continue
			}
			if _, joined := paths[row.Subject()]; !joined {
				return fmt.Errorf("changed managed path %q has no ordinary capture actor", row.Subject())
			}
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
				return fmt.Errorf("changed aggregate %q has no selected capture actor", row.Subject())
			}
		}
	}
	return nil
}

func validateNativePublicationPartition(ctx context.Context, directory string, batch NativeDocumentObservationBatch, mutations []pathMutation, resolver func(output.Destination) (string, error)) error {
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
	for _, action := range mutations {
		var path string
		if action.Scope == target.ScopeProject {
			path = filepath.Join(batch.roots.Manifest.SelectedPath, filepath.FromSlash(action.Destination.RelativePath()))
		} else {
			if resolver == nil {
				return fmt.Errorf("ordinary publication partition requires its selected global resolver")
			}
			path, err = resolver(action.Destination)
			if err != nil {
				return err
			}
		}
		observed, err := mutation.ObserveDirectoryEntryAuthorityBounded(path, recovery.MaximumPhysicalPathDepth, batch.budget)
		if err != nil {
			return err
		}
		appendPath(nativeDocumentObservationKey(observed), -3)
	}
	return validateDocumentWriterPaths(ctx, paths)
}
