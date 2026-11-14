package publication

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func (batch NativeDocumentObservationBatch) manifest(ctx context.Context) (journalnamespace.RootGrant, error) {
	roots, err := batch.roots.Qualify(ctx, batch.budget)
	if err != nil {
		return journalnamespace.RootGrant{}, err
	}
	manifest, present := roots.Manifest()
	if !present {
		return journalnamespace.RootGrant{}, fmt.Errorf("native observation requires its selected manifest root")
	}
	return manifest, nil
}

func validateNativeCaptureContext(ctx context.Context, paths Paths, batch NativeDocumentObservationBatch, options CaptureOptions) error {
	manifest, err := batch.manifest(ctx)
	if err != nil {
		return err
	}
	if filepath.Clean(paths.ManifestRoot) != manifest.SelectedPath() {
		return fmt.Errorf("capture paths differ from the selected native manifest root")
	}
	provenance := manifest.Provenance()
	declaration, err := mutation.CanonicalDirectoryEntryPathBounded(batch.owner.ManifestPath(), physicalwork.MaximumPhysicalPathDepth, batch.budget)
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
		if err != nil || other != provenance {
			return fmt.Errorf("ordinary capture manifest root differs from the native operation root")
		}
	}
	state, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(paths.StatefilePath, physicalwork.MaximumPhysicalPathDepth, batch.budget)
	if err != nil {
		return err
	}
	if !state.Exact().Equal(batch.owner.StatefileAuthority()) {
		return fmt.Errorf("capture statefile differs from its observed native owner")
	}
	return ctx.Err()
}

func validateNativeCaptureWork(before, after durable.Snapshot, edits []compose.ProjectionEdit, mutations []journalrecord.CaptureMutation, completions []durablecarrier.ManagedCarrierClaim) error {
	if err := journalrecord.ValidateCaptureMutations(mutations); err != nil {
		return err
	}
	projected, err := before.WithManagedProjections(after.ManagedPaths(), after.ManagedAggregates())
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
	wholeRecords := make(map[aggregate.DocumentAddress]topology.SubjectID)
	for _, edit := range edits {
		for _, contract := range edit.Participant().Selection().Contracts() {
			contracts[documentCaptureContractKey{address: contract.Address(), codec: contract.CodecContractID()}] = struct{}{}
		}
	}
	for _, mutation := range mutations {
		if contract, present := mutation.AggregateContract(); present {
			contracts[documentCaptureContractKey{address: contract.Address(), codec: contract.CodecContractID()}] = struct{}{}
			continue
		}
		paths[mutation.Subject()] = struct{}{}
		previous, hasPrevious := mutation.PreviousSubject()
		if mutation.Kind() == journalrecord.CaptureRecord && !hasPrevious && aggregate.IsHostConfigDocumentSubject(mutation.Subject()) {
			consumers := mutation.ConsumerTargets()
			for _, state := range after.ManagedPaths() {
				if state.Subject() != mutation.Subject() || len(state.ConsumerTargets()) != 1 || state.Scope() != mutation.Scope() || state.Destination() != mutation.Destination() || !slices.Equal(state.ConsumerTargets(), consumers) {
					continue
				}
				if state.ContentHash() != mutation.DesiredHash() || state.ContentKind() != mutation.ContentKind() || state.FileMode() != mutation.ExpectedPathMode() {
					continue
				}
				address, err := aggregate.NewDocumentAddress(state.ConsumerTargets()[0], state.Scope(), state.Destination())
				if err != nil {
					return err
				}
				wholeRecords[address] = state.Subject()
			}
		}
		if hasPrevious {
			paths[previous] = struct{}{}
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
			if whole, present := wholeRecords[contract.Address().Document()]; side == 0 && present {
				keyOwner, _, key := topologyprojection.HostConfigKeyIdentity(row.Subject())
				wholeOwner, entityBacked := topologyprojection.EntityID(whole)
				if key && entityBacked && keyOwner == wholeOwner {
					continue
				}
			}
			if _, joined := contracts[documentCaptureContractKey{address: contract.Address(), codec: contract.CodecContractID()}]; !joined {
				return fmt.Errorf("changed aggregate %q has no selected capture actor", row.Subject())
			}
		}
	}
	return nil
}

func validateNativePublicationPartition(ctx context.Context, directory string, batch NativeDocumentObservationBatch, mutations []journalrecord.CaptureMutation, resolver func(output.Destination) (string, error)) error {
	if err := journalrecord.ValidateCaptureMutations(mutations); err != nil {
		return err
	}
	publication, err := mutation.ObserveDirectoryEntryAuthorityBounded(directory, physicalwork.MaximumPhysicalPathDepth, batch.budget)
	if err != nil {
		return err
	}
	var partition journalrecord.WriterPartition
	if err := partition.Add(publication.NamespaceKey(), -1); err != nil {
		return err
	}
	if err := partition.Add(batch.owner.StatefileKey(), -2); err != nil {
		return err
	}
	for index, namespace := range batch.Evidence().Namespaces() {
		if err := partition.Add(namespace.Observation().NamespaceKey(), index); err != nil {
			return err
		}
		for _, alias := range namespace.Aliases() {
			if err := partition.Add(alias.Observation().NamespaceKey(), index); err != nil {
				return err
			}
		}
	}
	for _, action := range mutations {
		var path string
		if action.Scope() == target.ScopeProject {
			manifest, supplied := batch.roots.Manifest()
			if !supplied {
				return fmt.Errorf("ordinary project publication requires its selected manifest binding")
			}
			path = filepath.Join(manifest.SelectedPath, filepath.FromSlash(action.Destination().RelativePath()))
		} else {
			if resolver == nil {
				return fmt.Errorf("ordinary publication partition requires its selected global resolver")
			}
			path, err = resolver(action.Destination())
			if err != nil {
				return err
			}
		}
		observed, err := mutation.ObserveDirectoryEntryAuthorityBounded(path, physicalwork.MaximumPhysicalPathDepth, batch.budget)
		if err != nil {
			return err
		}
		if err := partition.Add(observed.NamespaceKey(), -3); err != nil {
			return err
		}
	}
	return partition.Validate(ctx)
}
