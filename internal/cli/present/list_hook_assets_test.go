package clipresent

import (
	"slices"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func TestListRowsOrdersHookAssetsAfterHooksBeforeMCPAndExtensions(t *testing.T) {
	environment, err := declarationmanifest.Decode([]byte(`version = 1
targets = ["codex", "opencode"]
[hook_asset.zz_asset]
source = "missing/asset.sh"
kind = "file"
[[hook]]
name = "zz_hook"
event = "PreToolUse"
command = "{hook_file:zz_asset}"
targets = ["codex"]
[[mcp_server]]
name = "aa_server"
targets = ["codex"]
scope = "project"
transport = "stdio"
command = "node"
[[extension]]
id = "aa_extension"
carrier = "opencode-plugin"
targets = ["opencode"]
scope = "project"
source = { host_source = "plugins/missing.ts" }
`))
	if err != nil {
		t.Fatal(err)
	}
	selection, err := targetselection.ForAvailableTargets(environment.Targets(), nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := ListRows(environment, nil, selection)
	var kinds []string
	for _, row := range rows {
		kinds = append(kinds, row.Kind)
	}
	want := []string{"hook", "hook_asset", "mcp-server", "extension"}
	if !slices.Equal(kinds, want) {
		t.Fatalf("row kinds = %q, want %q", kinds, want)
	}
	asset := rows[1]
	if asset.Key != "zz_asset" || asset.InstallName != "-" || asset.Scope != "project" ||
		asset.Source != "local:missing/asset.sh?mode=vendor" || asset.Targets != "codex" || asset.Group != "-" {
		t.Fatalf("hook asset projection = %+v", asset)
	}
}
