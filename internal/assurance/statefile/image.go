package statefile

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
)

// StateImage records a snapshot's statefile reference, including empty or
// native-only state. It is not live filesystem authority.
type StateImage struct {
	snapshot  durable.Snapshot
	reference pathauthority.Reference
}

func NewStateImage(snapshot durable.Snapshot, reference pathauthority.Reference) (StateImage, error) {
	image := StateImage{snapshot: snapshot, reference: reference}
	if err := image.Validate(); err != nil {
		return StateImage{}, err
	}
	return image, nil
}

func (image StateImage) Validate() error {
	if err := image.reference.Validate(); err != nil {
		return fmt.Errorf("statefile reference: %w", err)
	}
	return validateSnapshotStatefileIdentity(image.snapshot, image.reference.Identity())
}

func (image StateImage) Snapshot() durable.Snapshot { return image.snapshot }

func (image StateImage) Reference() pathauthority.Reference { return image.reference }
