package recovery

import (
	"errors"
	"testing"
)

type invocationPathEnvelope struct {
	used  int
	limit int
	cause error
}

func (envelope *invocationPathEnvelope) AdmitPathComponents(count int) error {
	if count > envelope.limit-envelope.used {
		return envelope.cause
	}
	envelope.used += count
	return nil
}

func TestInvocationReservationPrechargesOnceAndSeparatesSafety(t *testing.T) {
	shared := &invocationPathEnvelope{limit: 100, cause: errors.New("shared limit")}
	parent := NewPhysicalCaptureBudget(shared)
	work, err := NewArtifactWork(3, 7)
	if err != nil {
		t.Fatal(err)
	}
	paths := []int{2, 3}
	forwardWork, err := NewInvocationWork(paths, work)
	if err != nil {
		t.Fatal(err)
	}
	paths[0] = 100
	safetyWork, err := NewInvocationWork([]int{4}, work)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := parent.ReserveInvocation(forwardWork, safetyWork)
	if err != nil {
		t.Fatal(err)
	}
	if shared.used != 9 || parent.pathComponents != 9 || parent.entries != 6 || parent.bytes != 14 {
		t.Fatal("invocation admission was not fully precharged")
	}
	copy := reservation
	forward, safety, err := reservation.Take()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := copy.Take(); err == nil {
		t.Fatal("copied reservation minted a second allocation")
	}
	if _, err := parent.ReserveInvocation(forwardWork, safetyWork); err == nil {
		t.Fatal("reservation continued after transfer")
	}
	if err := forward.AdmitPhysicalWork(5, 3, 7); err != nil {
		t.Fatal(err)
	}
	if err := forward.AdmitPhysicalWork(1, 0, 0); err == nil {
		t.Fatal("forward overspent its allocation")
	}
	if err := safety.AdmitPhysicalWork(4, 3, 7); err != nil {
		t.Fatal("forward exhaustion consumed safety", err)
	}
	if err := safety.AdmitPhysicalWork(1, 0, 0); err == nil {
		t.Fatal("safety received excess path credit")
	}
	if err := safety.AdmitPhysicalWork(0, 1, 0); err == nil {
		t.Fatal("safety credit was replenished")
	}
	if shared.used != 9 {
		t.Fatal("child consumption charged the shared envelope again")
	}
}

func TestInvocationReservationFailureRetainsChargesWithoutCapability(t *testing.T) {
	want := errors.New("shared limit")
	shared := &invocationPathEnvelope{limit: 3, cause: want}
	parent := NewPhysicalCaptureBudget(shared)
	work, err := NewInvocationWork([]int{2}, ArtifactWork{})
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := parent.ReserveInvocation(work, work)
	if !errors.Is(err, want) || shared.used != 2 || parent.pathComponents != 2 {
		t.Fatal("partial admission replenished credit", err)
	}
	if _, _, err := reservation.Take(); err == nil {
		t.Fatal("failed admission leaked a capability")
	}
	if _, err := parent.ReserveInvocation(work, InvocationWork{}); !errors.Is(err, want) {
		t.Fatal("retry bypassed spent shared credit", err)
	}
}

func TestInvocationReservationsTransferOnceWithinFrozenPlanningPhase(t *testing.T) {
	parent := NewPhysicalCaptureBudget(nil)
	first, err := parent.ReserveInvocation(InvocationWork{}, InvocationWork{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := parent.ReserveInvocation(InvocationWork{}, InvocationWork{})
	if err != nil {
		t.Fatal(err)
	}
	copy := first
	if _, _, err := first.Take(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := copy.Take(); err == nil {
		t.Fatal("copied reservation duplicated prepaid credit")
	}
	if _, _, err := second.Take(); err != nil {
		t.Fatal("a separately prepaid invocation was lost", err)
	}
	if _, _, err := (InvocationWorkReservation{}).Take(); err == nil {
		t.Fatal("zero reservation became available")
	}
}

func TestInvocationWorkRejectsInvalidPassesAndArtifactWork(t *testing.T) {
	for _, paths := range [][]int{{-1}, {MaximumPhysicalPathDepth + 1}, {int(^uint(0) >> 1)}} {
		if _, err := NewInvocationWork(paths, ArtifactWork{}); err == nil {
			t.Fatal("invalid path work was accepted")
		}
	}
	for _, artifact := range []ArtifactWork{{entries: -1}, {bytes: -1}, {entries: MaximumPhysicalEntries + 1}, {bytes: MaximumPhysicalBytes + 1}} {
		if _, err := NewInvocationWork(nil, artifact); err == nil {
			t.Fatal("invalid artifact work was accepted")
		}
	}
	paths := make([]int, maximumPhysicalPathComponentVisits/MaximumPhysicalPathDepth+1)
	for index := range paths {
		paths[index] = MaximumPhysicalPathDepth
	}
	if _, err := NewInvocationWork(paths, ArtifactWork{}); err == nil {
		t.Fatal("aggregate path capacity was bypassed")
	}
}

func TestInvocationReservationRefusesAggregateOverdrawBeforeSharedCharge(t *testing.T) {
	for _, dimension := range []string{"path", "entries", "bytes"} {
		t.Run(dimension, func(t *testing.T) {
			shared := &invocationPathEnvelope{limit: 100, cause: errors.New("shared limit")}
			parent := NewPhysicalCaptureBudget(shared)
			if dimension == "path" {
				parent.pathComponentLimit = 3
			}
			if dimension == "entries" {
				parent.entryLimit = 3
			}
			if dimension == "bytes" {
				parent.byteLimit = 3
			}
			artifact, err := NewArtifactWork(2, 2)
			if err != nil {
				t.Fatal(err)
			}
			work, err := NewInvocationWork([]int{2}, artifact)
			if err != nil {
				t.Fatal(err)
			}
			reservation, err := parent.ReserveInvocation(work, work)
			if err == nil || shared.used != 0 || parent.pathComponents != 0 || parent.entries != 0 || parent.bytes != 0 {
				t.Fatal("aggregate refusal leaked credit", err)
			}
			if _, _, err := reservation.Take(); err == nil {
				t.Fatal("aggregate refusal returned a capability")
			}
		})
	}
}
