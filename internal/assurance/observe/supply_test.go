package observe

import (
	"testing"

	"github.com/isty2e/daem/internal/topology"
)

func TestExactSupplyObservationRequiresResourceSubject(t *testing.T) {
	resourceSubject, err := topology.NewSubjectID(topology.SubjectResource, "skill", "oracle")
	if err != nil {
		t.Fatal(err)
	}
	observation, err := NewExactSupplyObservation(resourceSubject, true)
	if err != nil {
		t.Fatal(err)
	}
	if observation.Subject() != resourceSubject || !observation.Stale() {
		t.Fatalf("observation = %#v, want stale resource subject", observation)
	}

	projectionSubject, err := topology.NewSubjectID(topology.SubjectProjection, "skill.project.agents", "skill:oracle")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewExactSupplyObservation(projectionSubject, false); err == nil {
		t.Fatal("NewExactSupplyObservation accepted a projection subject")
	}
	if _, err := NewExactSupplyObservation(topology.SubjectID{}, false); err == nil {
		t.Fatal("NewExactSupplyObservation accepted a zero subject")
	}
}
