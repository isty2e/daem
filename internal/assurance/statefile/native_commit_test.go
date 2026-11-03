package statefile_test

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
)

func TestSnapshotCodecPreservesNativePublicationIdentity(t *testing.T) {
	before := durable.EmptySnapshot()
	after, err := before.AdvanceNativeDocumentCommit()
	if err != nil {
		t.Fatal(err)
	}
	codec := statefile.Codec{}
	for _, snapshot := range []durable.Snapshot{before, after} {
		encoded, err := codec.Encode(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := codec.Decode(encoded)
		if err != nil || !decoded.Equal(snapshot) {
			t.Fatal("state codec lost native publication identity", err)
		}
	}
}
