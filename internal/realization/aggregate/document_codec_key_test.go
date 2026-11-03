package aggregate

import (
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestDocumentCodecKeySeparatesOwnerFromSelectionAndAdmission(t *testing.T) {
	document, err := NewDocumentAddress(target.TargetCodex, target.ScopeProject, outputtest.Parse(t, ".codex/config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	native, _ := HostConfigCodecForSyntax("toml")
	key, err := NewDocumentCodecKey(document, native)
	if err != nil || key.Validate() != nil || key.DocumentAddress() != document || key.CodecContractID() != native {
		t.Fatal("valid codec owner changed its canonical identity", err)
	}
	other, err := NewDocumentCodecKey(document, MCPCodecCodexProjectStdioCommand)
	if err != nil || key == other || key.Compare(other) == 0 || key.Compare(other) != -other.Compare(key) {
		t.Fatal("one logical document conflated distinct codec owners", err)
	}
	var derived []DocumentCodecKey
	for _, name := range []string{"theme", "other"} {
		path, err := output.NewKeyPath(name)
		if err != nil {
			t.Fatal(err)
		}
		footprint, err := output.NewKeyFootprint(path)
		if err != nil {
			t.Fatal(err)
		}
		contract, err := NewHostConfigProjectionContract(target.TargetCodex, target.ScopeProject, document.AggregateRoot(), footprint, native)
		if err != nil {
			t.Fatal(err)
		}
		selection, err := NewSelection([]ProjectionContract{contract})
		if err != nil {
			t.Fatal(err)
		}
		if contract.DocumentCodecKey() != key || selection.DocumentCodecKey() != key {
			t.Fatal("selected key frontier changed its codec observation owner")
		}
		derived = append(derived, selection.DocumentCodecKey())
	}
	if derived[0] != derived[1] || (Selection{}).DocumentCodecKey().Validate() == nil {
		t.Fatal("codec owner became a complete frontier or zero selection gained identity")
	}
	for _, id := range []CodecContractID{"", " bad ", "bad\x00codec"} {
		if result, err := NewDocumentCodecKey(document, id); err == nil || result.Validate() == nil {
			t.Fatal("invalid codec token yielded a usable identity")
		}
	}
	if result, err := NewDocumentCodecKey(DocumentAddress{}, native); err == nil || result.Validate() == nil {
		t.Fatal("unknown document yielded a usable codec owner")
	}
	if _, err := NewDocumentCodecKey(document, "external-codec-v1"); err != nil {
		t.Fatal("identity validation silently became catalog admission", err)
	}
}
