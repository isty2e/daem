package nativeconfig

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
)

func TestSelectedNativeObservationDoesNotReadOtherDeclaredDocuments(t *testing.T) {
	root := t.TempDir()
	paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	environment, err := declarationmanifest.Decode([]byte(`version = 1
targets = ["codex"]
[[host_config]]
id = "selected"
scope = "project"
path = "selected.json"
source = "source.json"
[[host_config]]
id = "unselected"
scope = "project"
path = "blocked.json"
source = "other-source.json"
`))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".codex", "selected.json"), []byte(`{"chosen":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".codex", "blocked.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	config := environment.HostConfigs()[0]
	placement, err := profile.ResolveHostConfigPlacement(profile.HostConfigPlacementInput{
		Target: config.Target(), Scope: config.Scope(), Path: config.Path(), Ownership: config.Ownership(), Syntax: config.Syntax(),
	})
	if err != nil {
		t.Fatal(err)
	}
	address, err := aggregate.NewDocumentAddress(placement.Target(), placement.Scope(), placement.Destination())
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := LoadPersistenceEpoch(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := ObserveSelectedDocuments(t.Context(), paths, &epoch, []aggregate.DocumentAddress{address})
	if err != nil {
		t.Fatal(err)
	}
	facts, present := observation.NativeInput()
	if !present || len(facts.Documents.Namespaces()) != 1 {
		t.Fatal("selected observation did not retain one native namespace")
	}
	document, _, present := facts.Documents.Document(address)
	if !present || string(document.Content()) != `{"chosen":true}` {
		t.Fatal("selected observation did not retain the actual native bytes")
	}
	before, err := epoch.Snapshot()
	if err != nil || !before.Equal(facts.Documents.BeforeState()) {
		t.Fatal("selected observation replaced its complete persistence epoch", err)
	}
	if err := observation.Close(); err != nil {
		t.Fatal(err)
	}
	if _, present := observation.NativeInput(); present {
		t.Fatal("closed observation still lends native evidence")
	}
	blocked := environment.HostConfigs()[1]
	blockedPlacement, err := profile.ResolveHostConfigPlacement(profile.HostConfigPlacementInput{Target: blocked.Target(), Scope: blocked.Scope(), Path: blocked.Path(), Ownership: blocked.Ownership(), Syntax: blocked.Syntax()})
	if err != nil {
		t.Fatal(err)
	}
	blockedAddress, _ := aggregate.NewDocumentAddress(blockedPlacement.Target(), blockedPlacement.Scope(), blockedPlacement.Destination())
	if observed, err := ObserveSelectedDocuments(t.Context(), paths, &epoch, []aggregate.DocumentAddress{address, blockedAddress}); err == nil || observed != nil {
		t.Fatal("an actually selected invalid document became a successful partial read", err)
	}
	if _, err := os.Stat(paths.StateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read-only observation created metadata", err)
	}
}
