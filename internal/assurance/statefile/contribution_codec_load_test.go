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

func TestContributionBoundStateCodecDoesNotWidenFilesystemReaders(t *testing.T) {
	state := nativeAggregateState(t, "ordinary")
	codec := contributionStateCodec(t, t.Context(), []aggregate.CodecContractID{state.Contribution().CodecContractID()}, configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 16})
	content, err := codec.Encode(contributionCodecSnapshot(t, state))
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
			if snapshot, err := reader.load(t.Context(), path); err == nil || len(snapshot.ManagedAggregates()) != 0 {
				t.Fatal("explicit codec widened a default filesystem reader or returned partial state")
			}
			unchanged, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(content, unchanged) {
				t.Fatalf("default-reader refusal rewrote native state: %v", err)
			}
		})
	}
}
