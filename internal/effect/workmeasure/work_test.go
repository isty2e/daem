package workmeasure

import "testing"

func TestArtifactWorkRejectsNegativeMeasurement(t *testing.T) {
	for _, counts := range []struct {
		entries int
		bytes   int64
	}{{entries: -1}, {bytes: -1}} {
		if _, err := NewArtifactWork(counts.entries, counts.bytes); err == nil {
			t.Fatal("negative artifact work was accepted")
		}
	}
}

func TestArtifactWorkPreservesMeasurementWithoutAllocatingCapacity(t *testing.T) {
	work, err := NewArtifactWork(1_000_000, 1<<40)
	if err != nil || work.Entries() != 1_000_000 || work.Bytes() != 1<<40 {
		t.Fatal("non-negative measurement was capped at an allocation limit", err)
	}
	copy := work
	if !work.Equal(copy) || work.Equal(ArtifactWork{}) {
		t.Fatal("measurement equality changed")
	}
}
