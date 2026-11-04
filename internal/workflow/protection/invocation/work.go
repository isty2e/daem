package invocation

import (
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
)

type invocationWorkProjection struct {
	budget  *physicalwork.Budget
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

func (work *invocationWorkProjection) compiled() (physicalwork.InvocationWork, error) {
	artifact, err := workmeasure.NewArtifactWork(work.entries, work.bytes)
	if err != nil {
		return physicalwork.InvocationWork{}, err
	}
	return physicalwork.NewInvocationWork(work.paths, artifact)
}

// ReserveWork compiles this invocation's fixed lifecycle from owner-held
// layouts. Correlation work belongs to its checkpoint owner. All invocations
// must be reserved against the shared budget before the first execution.
func (input InvocationInput) ReserveWork(budget *physicalwork.Budget, correlation physicalwork.InvocationWork) (InvocationInput, error) {
	if budget == nil || !input.matchesPreparation() {
		return InvocationInput{}, fmt.Errorf("prepared invocation and shared allocation are required")
	}
	if input.qualification.work != (physicalwork.InvocationWorkReservation{}) {
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

func (qualified *invocationQualification) compileWork(targets []invocationDocument, correlation physicalwork.InvocationWork) (physicalwork.InvocationWork, physicalwork.InvocationWork, error) {
	forward := &invocationWorkProjection{budget: physicalwork.NewCaptureBudget(nil)}
	safety := &invocationWorkProjection{budget: physicalwork.NewCaptureBudget(nil)}
	// Initial admission, immediately before Arm, and immediately before Bind.
	for range 3 {
		if err := qualified.authority.ReserveCheck(forward); err != nil {
			return physicalwork.InvocationWork{}, physicalwork.InvocationWork{}, err
		}
	}
	if err := qualified.registry.ReserveOpen(forward); err != nil {
		return physicalwork.InvocationWork{}, physicalwork.InvocationWork{}, err
	}
	if err := qualified.registry.ReserveRead(forward); err != nil {
		return physicalwork.InvocationWork{}, physicalwork.InvocationWork{}, err
	}
	for range 2 { // Arm and Bind.
		if err := qualified.registry.ReserveTransition(forward); err != nil {
			return physicalwork.InvocationWork{}, physicalwork.InvocationWork{}, err
		}
	}
	if err := qualified.registry.ReserveOpen(safety); err != nil {
		return physicalwork.InvocationWork{}, physicalwork.InvocationWork{}, err
	}
	if err := qualified.registry.ReserveRead(safety); err != nil { // Finalizer lookup.
		return physicalwork.InvocationWork{}, physicalwork.InvocationWork{}, err
	}
	for _, target := range targets {
		for range 2 { // Initial inspection and the launch-edge inspection.
			if err := qualified.reserveTargetQualification(target, forward); err != nil {
				return physicalwork.InvocationWork{}, physicalwork.InvocationWork{}, err
			}
			if err := reserveTargetInspection(target, forward); err != nil {
				return physicalwork.InvocationWork{}, physicalwork.InvocationWork{}, err
			}
		}
		for range 2 { // Restriction and fresh complete proof.
			if err := qualified.authority.ReserveCheck(safety); err != nil {
				return physicalwork.InvocationWork{}, physicalwork.InvocationWork{}, err
			}
			if err := qualified.registry.ReserveRead(safety); err != nil {
				return physicalwork.InvocationWork{}, physicalwork.InvocationWork{}, err
			}
			if err := qualified.reserveTargetQualification(target, safety); err != nil {
				return physicalwork.InvocationWork{}, physicalwork.InvocationWork{}, err
			}
			layout := target.layout
			if err := layout.ReserveDestinationAccess(physicalwork.MaximumPhysicalPathDepth, safety); err != nil {
				return physicalwork.InvocationWork{}, physicalwork.InvocationWork{}, err
			}
			for range mutationfs.RootedAbsencePathObservationCount - 1 {
				if err := rootedpath.ChargeAbsolutePath(layout.CanonicalPath(), physicalwork.MaximumPhysicalPathDepth, safety); err != nil {
					return physicalwork.InvocationWork{}, physicalwork.InvocationWork{}, err
				}
			}
			if err := reserveTargetInspection(target, safety); err != nil {
				return physicalwork.InvocationWork{}, physicalwork.InvocationWork{}, err
			}
		}
	}
	if err := qualified.authority.ReserveCheck(safety); err != nil { // Clear edge.
		return physicalwork.InvocationWork{}, physicalwork.InvocationWork{}, err
	}
	if err := qualified.registry.ReserveTransition(safety); err != nil {
		return physicalwork.InvocationWork{}, physicalwork.InvocationWork{}, err
	}
	if err := qualified.registry.ReserveRead(safety); err != nil { // Failed Clear readback.
		return physicalwork.InvocationWork{}, physicalwork.InvocationWork{}, err
	}
	forwardWork, err := forward.compiled()
	if err == nil {
		forwardWork, err = forwardWork.Plus(correlation)
	}
	if err != nil {
		return physicalwork.InvocationWork{}, physicalwork.InvocationWork{}, err
	}
	safetyWork, err := safety.compiled()
	if err != nil {
		return physicalwork.InvocationWork{}, physicalwork.InvocationWork{}, err
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
	return target.layout.ReserveDestinationCapture(physicalwork.MaximumPhysicalPathDepth, budget)
}

func reserveTargetInspection(target invocationDocument, budget mutation.PhysicalObservationBudget) error {
	if err := target.layout.ReserveDestinationAccess(physicalwork.MaximumPhysicalPathDepth, budget); err != nil {
		return err
	}
	return budget.AdmitPhysicalWork(0, 1, 0)
}
