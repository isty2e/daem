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

func (after SnapshotIndex) ValidateExpectedBaselines(ctx context.Context, groups []StateGroup, codecs aggregate.CodecCatalog, images []ImageIdentity) error {
	if ctx == nil {
		return fmt.Errorf("document state context is required")
	}
	aliases := make(map[aggregate.DocumentAddress]int)
	for index, group := range groups {
		for _, alias := range group.aliases {
			aliases[alias] = index
		}
	}
	for _, row := range after.aggregates {
		if err := ctx.Err(); err != nil {
			return err
		}
		contribution := row.Contribution()
		contract := contribution.Contract()
		index, present := aliases[contract.Address().Document()]
		if !present || !groups[index].Covers(contract) {
			continue
		}
		codec, found := codecs.Lookup(contract.CodecContractID())
		if !found {
			return fmt.Errorf("expected baseline codec is not admitted by the operation")
		}
		selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
		if err != nil {
			return err
		}
		snapshot, failure := codec.Read(groups[index].comparison.expected, selection)
		if failure != nil {
			return failure
		}
		state, found := snapshot.State(contract)
		if !found || !state.Present() || state.CanonicalProjection() != contribution.CanonicalContribution() {
			return fmt.Errorf("selected after baseline differs from the actual expected image")
		}
	}
	for _, row := range after.paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		id, native := topologyprojection.EntityID(row.Subject())
		if !native || id.Kind() != entity.KindHostConfig {
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
				if !file || image.digest != row.ContentHash() || uint32(mode) != uint32(row.FileMode().Perm()) {
					return fmt.Errorf("whole after baseline differs from the actual expected image or mode")
				}
			}
		}
	}
	return ctx.Err()
}
