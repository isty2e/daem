package pipackage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestObserveNPMVersionClassifiesExactAbsentAndUnsafeArtifacts(t *testing.T) {
	root := resolvedTempDir(t)
	agentRoot := filepath.Join(root, "agent")
	inventory := mustVersionInventory(t, agentRoot, root)
	carrier := mustVersionCarrier(t)
	packagePath := filepath.Join(agentRoot, "npm", "node_modules", "pi-mcp-adapter")

	absent := ObserveNPMVersion(context.Background(), inventory, carrier)
	if absent.State() != VersionAbsent {
		t.Fatalf("absent state = %q, want absent", absent.State())
	}

	writePackageManifest(t, packagePath, `{"name":"pi-mcp-adapter","version":"2.15.0"}`)
	exact := ObserveNPMVersion(context.Background(), inventory, carrier)
	if exact.State() != VersionExact ||
		exact.PackageName() != "pi-mcp-adapter" ||
		exact.Version() != "2.15.0" {
		t.Fatalf("exact observation = %#v", exact)
	}

	if err := os.Remove(filepath.Join(packagePath, "package.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(
		filepath.Join(root, "missing.json"),
		filepath.Join(packagePath, "package.json"),
	); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	unsafe := ObserveNPMVersion(context.Background(), inventory, carrier)
	if unsafe.State() != VersionUnobservable || unsafe.Detail() == "" {
		t.Fatalf("unsafe observation = %#v", unsafe)
	}
}

func TestObserveNPMVersionRejectsMismatchedAndMalformedManifests(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "wrong name", content: `{"name":"fork","version":"2.15.0"}`},
		{name: "missing version", content: `{"name":"pi-mcp-adapter"}`},
		{name: "duplicate version", content: `{"name":"pi-mcp-adapter","version":"2.15.0","version":"2.14.0"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := resolvedTempDir(t)
			agentRoot := filepath.Join(root, "agent")
			inventory := mustVersionInventory(t, agentRoot, root)
			packagePath := filepath.Join(
				agentRoot,
				"npm",
				"node_modules",
				"pi-mcp-adapter",
			)
			writePackageManifest(t, packagePath, test.content)
			observation := ObserveNPMVersion(
				context.Background(),
				inventory,
				mustVersionCarrier(t),
			)
			if observation.State() != VersionUnobservable {
				t.Fatalf("observation = %#v, want unobservable", observation)
			}
		})
	}
}

func mustVersionInventory(t *testing.T, agentRoot string, workDir string) Inventory {
	t.Helper()
	if err := os.MkdirAll(agentRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	inventory, err := ReadSettings(SettingsInput{
		ConfigRoot: agentRoot,
		WorkDir:    workDir,
		Scope:      target.ScopeGlobal,
	})
	if err != nil {
		t.Fatal(err)
	}
	return inventory
}

func mustVersionCarrier(t *testing.T) extensiontopology.Carrier {
	t.Helper()
	source, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindHostSource,
		"npm:pi-mcp-adapter@^2.13.0",
	)
	if err != nil {
		t.Fatal(err)
	}
	key, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		target.ScopeGlobal,
		source,
	)
	if err != nil {
		t.Fatal(err)
	}
	carrier, err := extensiontopology.NewCarrier(key)
	if err != nil {
		t.Fatal(err)
	}
	return carrier
}

func writePackageManifest(t *testing.T, packagePath string, content string) {
	t.Helper()
	if err := os.MkdirAll(packagePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(packagePath, "package.json"),
		[]byte(content),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
}
