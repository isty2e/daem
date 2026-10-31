package protection

import (
	"fmt"

	journalrecovery "github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

type invocationWorkProjection struct {
	budget  *journalrecovery.PhysicalWorkBudget
	paths   []int
	entries int
	bytes   int64
}

func (work *invocationWorkProjection) AdmitPathComponents(count int) error {
	if err := work.budget.AdmitPathComponents(count); err != nil {
		return err
	}
	work.paths = append(work.paths, count)
	return nil
}

func (work *invocationWorkProjection) AdmitPhysicalWork(paths, entries int, bytes int64) error {
	if err := work.budget.AdmitPhysicalWork(paths, entries, bytes); err != nil {
		return err
	}
	work.paths = append(work.paths, paths)
	work.entries += entries
	work.bytes += bytes
	return nil
}

func (work *invocationWorkProjection) compiled() (journalrecovery.InvocationWork, error) {
	artifact, err := journalrecovery.NewArtifactWork(work.entries, work.bytes)
	if err != nil {
		return journalrecovery.InvocationWork{}, err
	}
	return journalrecovery.NewInvocationWork(work.paths, artifact)
}

// ReserveWork compiles this invocation's fixed lifecycle from owner-held
// layouts. Correlation work belongs to its checkpoint owner. All invocations
// must be reserved against the shared budget before the first execution.
func (input InvocationInput) ReserveWork(budget *journalrecovery.PhysicalWorkBudget, correlation journalrecovery.InvocationWork) (InvocationInput, error) {
	if budget == nil || !input.matchesPreparation() {
		return InvocationInput{}, fmt.Errorf("prepared invocation and shared allocation are required")
	}
	if input.qualification.work != (journalrecovery.InvocationWorkReservation{}) {
		return InvocationInput{}, fmt.Errorf("invocation work was already reserved")
	}
	var targets []invocationDocument
	for _, document := range input.Protection.Documents() {
		targets = append(targets, input.qualification.documents[document.PathAuthority().Key()])
	}
	forward, safety, err := input.qualification.compileWork(targets, correlation)
	if err != nil {
		return InvocationInput{}, err
	}
	input.qualification.work, err = budget.ReserveInvocation(forward, safety)
	if err != nil {
		return InvocationInput{}, err
	}
	return input, nil
}

func (qualified *invocationQualification) compileWork(targets []invocationDocument, correlation journalrecovery.InvocationWork) (journalrecovery.InvocationWork, journalrecovery.InvocationWork, error) {
	forward := &invocationWorkProjection{budget: journalrecovery.NewPhysicalCaptureBudget(nil)}
	safety := &invocationWorkProjection{budget: journalrecovery.NewPhysicalCaptureBudget(nil)}
	// Initial admission, immediately before Arm, and immediately before Bind.
	for range 3 {
		if err := qualified.authority.ReserveCheck(forward); err != nil {
			return journalrecovery.InvocationWork{}, journalrecovery.InvocationWork{}, err
		}
	}
	if err := qualified.registry.ReserveOpen(forward); err != nil {
		return journalrecovery.InvocationWork{}, journalrecovery.InvocationWork{}, err
	}
	if err := qualified.registry.ReserveRead(forward); err != nil {
		return journalrecovery.InvocationWork{}, journalrecovery.InvocationWork{}, err
	}
	for range 2 { // Arm and Bind.
		if err := qualified.registry.ReserveTransition(forward); err != nil {
			return journalrecovery.InvocationWork{}, journalrecovery.InvocationWork{}, err
		}
	}
	if err := qualified.registry.ReserveOpen(safety); err != nil {
		return journalrecovery.InvocationWork{}, journalrecovery.InvocationWork{}, err
	}
	if err := qualified.registry.ReserveRead(safety); err != nil { // Finalizer lookup.
		return journalrecovery.InvocationWork{}, journalrecovery.InvocationWork{}, err
	}
	for _, target := range targets {
		for range 2 { // Initial inspection and the launch-edge inspection.
			if err := qualified.reserveTargetQualification(target, forward); err != nil {
				return journalrecovery.InvocationWork{}, journalrecovery.InvocationWork{}, err
			}
			if err := reserveTargetInspection(target, forward); err != nil {
				return journalrecovery.InvocationWork{}, journalrecovery.InvocationWork{}, err
			}
		}
		for range 2 { // Restriction and fresh complete proof.
			if err := qualified.authority.ReserveCheck(safety); err != nil {
				return journalrecovery.InvocationWork{}, journalrecovery.InvocationWork{}, err
			}
			if err := qualified.registry.ReserveRead(safety); err != nil {
				return journalrecovery.InvocationWork{}, journalrecovery.InvocationWork{}, err
			}
			if err := qualified.reserveTargetQualification(target, safety); err != nil {
				return journalrecovery.InvocationWork{}, journalrecovery.InvocationWork{}, err
			}
			layout := target.layout
			if err := layout.ReserveDestinationAccess(journalrecovery.MaximumPhysicalPathDepth, safety); err != nil {
				return journalrecovery.InvocationWork{}, journalrecovery.InvocationWork{}, err
			}
			for range mutationfs.RootedAbsencePathObservationCount - 1 {
				if err := rootedpath.ChargeAbsolutePath(layout.CanonicalPath(), journalrecovery.MaximumPhysicalPathDepth, safety); err != nil {
					return journalrecovery.InvocationWork{}, journalrecovery.InvocationWork{}, err
				}
			}
			if err := reserveTargetInspection(target, safety); err != nil {
				return journalrecovery.InvocationWork{}, journalrecovery.InvocationWork{}, err
			}
		}
	}
	if err := qualified.authority.ReserveCheck(safety); err != nil { // Clear edge.
		return journalrecovery.InvocationWork{}, journalrecovery.InvocationWork{}, err
	}
	if err := qualified.registry.ReserveTransition(safety); err != nil {
		return journalrecovery.InvocationWork{}, journalrecovery.InvocationWork{}, err
	}
	if err := qualified.registry.ReserveRead(safety); err != nil { // Failed Clear readback.
		return journalrecovery.InvocationWork{}, journalrecovery.InvocationWork{}, err
	}
	forwardWork, err := forward.compiled()
	if err == nil {
		forwardWork, err = forwardWork.Plus(correlation)
	}
	if err != nil {
		return journalrecovery.InvocationWork{}, journalrecovery.InvocationWork{}, err
	}
	safetyWork, err := safety.compiled()
	if err != nil {
		return journalrecovery.InvocationWork{}, journalrecovery.InvocationWork{}, err
	}
	return forwardWork, safetyWork, nil
}

func (qualified *invocationQualification) reserveTargetQualification(target invocationDocument, budget mutation.PhysicalObservationBudget) error {
	for _, binding := range target.bindings {
		if err := binding.observation.ReserveObservation(budget); err != nil {
			return err
		}
		if err := qualified.owners[binding.owner].ReserveObservation(budget); err != nil {
			return err
		}
	}
	return target.layout.ReserveDestinationCapture(journalrecovery.MaximumPhysicalPathDepth, budget)
}

func reserveTargetInspection(target invocationDocument, budget mutation.PhysicalObservationBudget) error {
	if err := target.layout.ReserveDestinationAccess(journalrecovery.MaximumPhysicalPathDepth, budget); err != nil {
		return err
	}
	return budget.AdmitPhysicalWork(0, 1, 0)
}
