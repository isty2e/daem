package publication

import (
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
)

func (captured CapturedNativeJournal) RequireAllocation(budget *physicalwork.Budget) error {
	if err := captured.Validate(); err != nil {
		return err
	}
	if budget == nil || budget != captured.budget {
		return fmt.Errorf("captured native recovery requires its original allocation")
	}
	return nil
}

// RequireRecoverySelection admits a borrowed handle to the same manifest incarnation.
func (captured CapturedNativeJournal) RequireRecoverySelection(manifest *rootedpath.CapturedRoot, budget *physicalwork.Budget) error {
	if err := captured.RequireAllocation(budget); err != nil {
		return err
	}
	selected, err := manifest.Authority()
	if err != nil {
		return err
	}
	anchors, err := captured.journal.RecordedRootAnchors()
	if err != nil {
		return err
	}
	selectedProvenance, err := selected.Provenance()
	if err != nil {
		return err
	}
	manifestReference, err := captured.journal.ManifestRootReference()
	if err != nil {
		return err
	}
	wantProvenance, supplied := anchors[manifestReference]
	if !supplied {
		return fmt.Errorf("captured native recovery requires its manifest binding")
	}
	if selectedProvenance != wantProvenance {
		return fmt.Errorf("captured native recovery manifest differs from its publication")
	}
	return nil
}

func (captured CapturedNativeJournal) ResolveOrdinaryDestination(destination output.Destination) (string, error) {
	if err := captured.Validate(); err != nil {
		return "", err
	}
	if captured.publication.resolver == nil {
		return "", fmt.Errorf("native ordinary recovery requires independent path selection")
	}
	return captured.publication.resolver(destination)
}
