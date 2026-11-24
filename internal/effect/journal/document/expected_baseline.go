package document

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

// ImageIdentity is recorded content and mode evidence, not a loaded image or
// filesystem authority. Its digest excludes executable bits, as image artifacts do.
type ImageIdentity struct {
	root   recovery.PartialDocumentState
	digest artifact.ContentHash
}

func NewImageIdentity(exists bool, digest artifact.ContentHash, mode *recovery.PermissionMode) (ImageIdentity, error) {
	root, err := recovery.NewPartialDocumentState(exists, mode, nil)
	if err != nil {
		return ImageIdentity{}, err
	}
	if exists {
		if err := digest.Validate(); err != nil {
			return ImageIdentity{}, err
		}
	} else if digest != "" {
		return ImageIdentity{}, fmt.Errorf("absent image has a content digest")
	}
	return ImageIdentity{root: root, digest: digest}, nil
}

func (index SnapshotIndex) WholePath(subject topology.SubjectID) (durable.ManagedPathState, bool) {
	row, found := index.paths[subject]
	return row, found
}

func (indexes SnapshotIndexes) ValidateExpectedBaselines(ctx context.Context, groups []StateGroup, codecs aggregate.CodecCatalog, images []ImageIdentity) error {
	if ctx == nil {
		return fmt.Errorf("document state context is required")
	}
	aliases := make(map[aggregate.DocumentAddress]int)
	for index, group := range groups {
		for _, alias := range group.afterAliases {
			aliases[alias] = index
		}
	}
	sets, err := indexes.after.coveredContributionSets(ctx, groups, aliases)
	if err != nil {
		return err
	}
	for _, set := range sets {
		if err := ctx.Err(); err != nil {
			return err
		}
		group := groups[aliases[set.Address().Document()]]
		if err := group.validateExpectedProjection(codecs, set); err != nil {
			return err
		}
	}
	for _, row := range indexes.after.paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		id, native := topologyprojection.EntityID(row.Subject())
		if !native || id.Kind() != entity.KindAgentConfig {
			continue
		}
		for _, selected := range row.ConsumerTargets() {
			address, err := aggregate.NewDocumentAddress(selected, row.Scope(), row.Destination())
			if err != nil {
				return err
			}
			if index, present := aliases[address]; present {
				if index >= len(images) {
					return fmt.Errorf("whole after baseline differs from the actual expected image or mode")
				}
				image := images[index]
				mode, file := image.root.RegularFileMode()
				comparison := groups[index].comparison
				if !file && comparison.whole && !comparison.before.Exists() && !comparison.expected.Exists() &&
					image.root.SameRootMetadata(comparison.expectedState) {
					// An unchanged row can retain missing-path evidence for recovery.
					// It does not acquire ownership or advance its content baseline.
					if previous, found := indexes.before.paths[row.Subject()]; found && previous.Equal(row) {
						continue
					}
				}
				if !file || image.digest != row.ContentHash() || uint32(mode) != uint32(row.FileMode().Perm()) {
					return fmt.Errorf("whole after baseline differs from the actual expected image or mode")
				}
			}
		}
	}
	return ctx.Err()
}

func (index SnapshotIndex) coveredContributionSets(ctx context.Context, groups []StateGroup, aliases map[aggregate.DocumentAddress]int) ([]aggregate.ContributionSet, error) {
	var selected []aggregate.SubjectContribution
	for _, row := range index.aggregates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		contribution := row.Contribution()
		contract := contribution.Contract()
		group, found := aliases[contract.Address().Document()]
		if !found || !groups[group].Covers(contract) {
			continue
		}
		item, err := aggregate.NewSubjectContribution(row.Subject(), contribution)
		if err != nil {
			return nil, err
		}
		selected = append(selected, item)
	}
	return aggregate.PartitionContributionSets(selected)
}

func (group StateGroup) validateExpectedProjection(codecs aggregate.CodecCatalog, set aggregate.ContributionSet) error {
	contract := set.Contract()
	codec, found := codecs.Lookup(contract.CodecContractID())
	if !found {
		return fmt.Errorf("expected baseline codec is not admitted by the operation")
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		return err
	}
	snapshot, failure := codec.Read(group.comparison.expected, selection)
	if failure != nil {
		return failure
	}
	expected, err := codec.CanonicalProjection(set)
	if err != nil {
		return err
	}
	state, found := snapshot.State(contract)
	if !found || !state.Present() || state.CanonicalProjection() != expected {
		return fmt.Errorf("selected after baseline differs from the actual expected image")
	}
	return nil
}
