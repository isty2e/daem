package statefile

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestStatefileReadersPreserveNativeContributions(t *testing.T) {
	state := nativeAggregateState(t, "ordinary")
	expected := contributionCodecSnapshot(t, state)
	codec := contributionStateCodec(t, t.Context(), []aggregate.CodecContractID{state.Contribution().CodecContractID()}, configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 16})
	content, err := codec.Encode(expected)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, reader := range []struct {
		name string
		load func(context.Context, string) (durable.Snapshot, error)
	}{
		{name: "required", load: Load},
		{name: "optional", load: LoadOptional},
	} {
		t.Run(reader.name, func(t *testing.T) {
			snapshot, err := reader.load(t.Context(), path)
			if err != nil || !snapshot.Equal(expected) {
				t.Fatalf("default filesystem reader lost native state: %v", err)
			}
			if encoded, err := Marshal(snapshot); err != nil || !bytes.Equal(encoded, content) {
				t.Fatalf("default filesystem state cannot round-trip: %v", err)
			}
			unchanged, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(content, unchanged) {
				t.Fatalf("state observation rewrote native state: %v", err)
			}
		})
	}
}
