package statefile

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
)

func TestPhysicalCodecRequiresAnExplicitSelection(t *testing.T) {
	if _, err := NewPhysicalCodec(nil); err == nil {
		t.Fatal("missing physical codec was accepted")
	}
	var codec PhysicalCodec
	if _, err := codec.EncodeState(durable.EmptySnapshot()); err == nil {
		t.Fatal("unselected physical codec encoded State")
	}
	if _, err := codec.DecodeState(nil); err == nil {
		t.Fatal("unselected physical codec decoded State")
	}
}
