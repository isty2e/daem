package authoring

import (
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/realization/profile"
)

func TestPiNativeAuthoringPersistsChoiceAndKeepsExistingIntent(t *testing.T) {
	request := AddMCPServerRequest{Name: "context7", Targets: []string{"pi"}, Scope: "project", Command: "node", Args: []string{"server.js"}}
	initial := ManifestDocument{Content: []byte("version = 1\ntargets = [\"pi\"]\n")}
	change, err := BuildAddMCPServerChangeWithPiVersion(initial, request, profile.ObservePiMCPVersion("1.0.2"))
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := declarationmanifest.Decode(change.Content)
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized.Extensions()) != 0 || len(normalized.MCPServers()) != 1 || normalized.MCPServers()[0].Bindings()[0].Backend() != desiredmcp.BackendNative {
		t.Fatalf("native choice was not persisted: %s", change.Content)
	}
	if len(change.Warnings) != 1 {
		t.Fatalf("native warning = %#v", change.Warnings)
	}
	retained, err := BuildAddMCPServerChangeWithPiVersion(ManifestDocument{Content: change.Content}, request, profile.PiMCPVersion{})
	if err != nil || retained.ChangeKind != "unchanged" || string(retained.Content) != string(change.Content) {
		t.Fatalf("existing native authoring = %#v, %v", retained, err)
	}

	for _, backendField := range []string{"", "backend = \"adapter\"\n"} {
		legacy := []byte("version = 1\ntargets = [\"pi\"]\n\n" + piProviderExtensionBlock("provider", "project", "npm:pi-mcp-adapter@2.15.0") + "\n[[mcp_server]]\nname = \"context7\"\ntargets = [\"pi\"]\nscope = \"project\"\n" + backendField + "transport = \"stdio\"\ncommand = \"node\"\nargs = [\"server.js\"]\n")
		retained, err := BuildAddMCPServerChangeWithPiVersion(ManifestDocument{Content: legacy}, request, profile.ObservePiMCPVersion("1.0.2"))
		if err != nil || retained.ChangeKind != "unchanged" || string(retained.Content) != string(legacy) {
			t.Fatalf("legacy authoring changed existing intent: %#v, %v", retained, err)
		}
	}
}

func TestPiNativeAuthoringFallbackDeclaresAdapterWithDiagnostic(t *testing.T) {
	for _, output := range []string{"", "0.99.0", "1.0.1", "2.0.0", "unobservable"} {
		change, err := BuildAddMCPServerChangeWithPiVersion(
			ManifestDocument{Content: []byte("version = 1\ntargets = [\"pi\"]\n")},
			AddMCPServerRequest{Name: "context7", Targets: []string{"pi"}, Scope: "project", Command: "node"},
			profile.ObservePiMCPVersion(output),
		)
		if err != nil {
			t.Fatal(err)
		}
		normalized, err := declarationmanifest.Decode(change.Content)
		if err != nil {
			t.Fatal(err)
		}
		if len(normalized.Extensions()) != 1 || normalized.Extensions()[0].Source().Ref() != "npm:pi-mcp-adapter@^2.13.0" || normalized.MCPServers()[0].Bindings()[0].Backend() != desiredmcp.BackendAdapter || len(change.Warnings) != 2 {
			t.Fatalf("fallback for %q = %s, %#v", output, change.Content, change.Warnings)
		}
	}
}

func TestPiNativeAuthoringExplicitProviderDominatesHostVersion(t *testing.T) {
	change, err := BuildAddMCPServerChangeWithPiVersion(
		ManifestDocument{Content: []byte("version = 1\ntargets = [\"pi\"]\n\n" + piProviderExtensionBlock("provider", "global", "npm:pi-mcp-adapter@2.15.0"))},
		AddMCPServerRequest{Name: "context7", Targets: []string{"pi"}, Scope: "project", Command: "node"},
		profile.ObservePiMCPVersion("1.0.2"),
	)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := declarationmanifest.Decode(change.Content)
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized.Extensions()) != 1 || normalized.MCPServers()[0].Bindings()[0].Backend() != desiredmcp.BackendAdapter {
		t.Fatalf("explicit provider was replaced: %s", change.Content)
	}
}

func TestPiNativeAuthoringRefusesMixedBackendPeers(t *testing.T) {
	for _, test := range []struct {
		backend string
		version string
	}{
		{"native", "0.85.1"},
		{"adapter", "1.0.2"},
	} {
		content := "version = 1\ntargets = [\"pi\"]\n\n[[mcp_server]]\nname = \"existing\"\ntargets = [\"pi\"]\nscope = \"project\"\nbackend = \"" + test.backend + "\"\ntransport = \"stdio\"\ncommand = \"node\"\n"
		_, err := BuildAddMCPServerChangeWithPiVersion(
			ManifestDocument{Content: []byte(content)},
			AddMCPServerRequest{Name: "new", Targets: []string{"pi"}, Scope: "project", Command: "node"},
			profile.ObservePiMCPVersion(test.version),
		)
		if err == nil {
			t.Fatalf("mixed peer %s/%s was accepted", test.backend, test.version)
		}
	}
}

func TestPiNativeAuthoringObservationIsNeededOnlyForNewAutomaticPi(t *testing.T) {
	request := AddMCPServerRequest{Name: "context7", Targets: []string{"pi"}, Scope: "project", Command: "node"}
	for _, test := range []struct {
		content string
		want    bool
	}{
		{"version = 1\ntargets = [\"pi\"]\n", true},
		{"version = 1\ntargets = [\"pi\"]\n\n" + piProviderExtensionBlock("provider", "project", "npm:pi-mcp-adapter@2.15.0"), false},
		{"version = 1\ntargets = [\"pi\"]\n\n[[mcp_server]]\nname = \"context7\"\ntargets = [\"pi\"]\nscope = \"project\"\nbackend = \"native\"\ntransport = \"stdio\"\ncommand = \"node\"\n", false},
	} {
		needed, err := requiresPiVersionForAuthoring(ManifestDocument{Content: []byte(test.content)}, request)
		if err != nil || needed != test.want {
			t.Fatalf("observation needed = %t, %v; want %t", needed, err, test.want)
		}
	}
	request.Targets = []string{"codex"}
	needed, err := requiresPiVersionForAuthoring(ManifestDocument{Content: []byte("version = 1\ntargets = [\"codex\"]\n")}, request)
	if err != nil || needed {
		t.Fatalf("non-Pi observation needed = %t, %v", needed, err)
	}
}
