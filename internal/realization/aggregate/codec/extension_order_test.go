package aggregatecodec

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
)

func TestExtensionOrderIdentityResolverUsesHostRuntimeIdentity(t *testing.T) {
	root := t.TempDir()
	xdgConfigHome := filepath.Join(root, "config")
	t.Setenv("XDG_CONFIG_HOME", xdgConfigHome)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("HOME", "")
	paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	resolve := ExtensionOrderIdentityResolver(paths)

	tests := []struct {
		name      string
		extension desiredextension.Extension
		want      string
	}{
		{
			name: "OpenCode package selector",
			extension: orderIdentityExtension(
				t,
				desiredextension.CarrierOpenCodePlugin,
				target.TargetOpenCode,
				target.ScopeProject,
				"@acme/plugin@2.4.0",
			),
			want: "@acme/plugin",
		},
		{
			name: "Pi npm selector",
			extension: orderIdentityExtension(
				t,
				desiredextension.CarrierPiPackage,
				target.TargetPi,
				target.ScopeProject,
				"npm:@acme/plugin@2.4.0",
			),
			want: "npm:@acme/plugin",
		},
		{
			name: "Pi project local",
			extension: orderIdentityExtension(
				t,
				desiredextension.CarrierPiPackage,
				target.TargetPi,
				target.ScopeProject,
				filepath.Join("plugins", "local"),
			),
			want: "local:project:" + filepath.Join(root, "plugins", "local"),
		},
		{
			name: "Pi global local",
			extension: orderIdentityExtension(
				t,
				desiredextension.CarrierPiPackage,
				target.TargetPi,
				target.ScopeGlobal,
				filepath.Join(root, "plugins", "global"),
			),
			want: "local:global:" + filepath.Join(root, "plugins", "global"),
		},
		{
			name: "OpenCode project local",
			extension: orderIdentityExtension(
				t,
				desiredextension.CarrierOpenCodePlugin,
				target.TargetOpenCode,
				target.ScopeProject,
				"./plugins/local.mjs",
			),
			want: (&url.URL{
				Scheme: "file",
				Path: filepath.ToSlash(filepath.Join(
					root,
					".opencode",
					"plugins",
					"local.mjs",
				)),
			}).String(),
		},
		{
			name: "OpenCode global local",
			extension: orderIdentityExtension(
				t,
				desiredextension.CarrierOpenCodePlugin,
				target.TargetOpenCode,
				target.ScopeGlobal,
				"./plugins/global.mjs",
			),
			want: (&url.URL{
				Scheme: "file",
				Path: filepath.ToSlash(filepath.Join(
					xdgConfigHome,
					"opencode",
					"plugins",
					"global.mjs",
				)),
			}).String(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			identity, err := resolve(test.extension.CarrierKey())
			if err != nil {
				t.Fatal(err)
			}
			if got := string(identity); got != test.want {
				t.Fatalf("identity = %q, want %q", got, test.want)
			}
		})
	}
}

func TestExtensionOrderIdentityResolverCollapsesHostAliases(t *testing.T) {
	root := t.TempDir()
	paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	resolve := ExtensionOrderIdentityResolver(paths)

	tests := []struct {
		name    string
		carrier desiredextension.Carrier
		target  target.Target
		left    string
		right   string
	}{
		{
			name:    "OpenCode package versions",
			carrier: desiredextension.CarrierOpenCodePlugin,
			target:  target.TargetOpenCode,
			left:    "@acme/plugin@1.0.0",
			right:   "@acme/plugin@2.0.0",
		},
		{
			name:    "Pi npm versions",
			carrier: desiredextension.CarrierPiPackage,
			target:  target.TargetPi,
			left:    "npm:@acme/plugin@1.0.0",
			right:   "npm:@acme/plugin@2.0.0",
		},
		{
			name:    "Pi git refs",
			carrier: desiredextension.CarrierPiPackage,
			target:  target.TargetPi,
			left:    "github:acme/plugin@v1",
			right:   "https://github.com/acme/plugin.git@v2",
		},
		{
			name:    "OpenCode relative file spellings",
			carrier: desiredextension.CarrierOpenCodePlugin,
			target:  target.TargetOpenCode,
			left:    "./plugins/nested/../local.mjs",
			right:   "./plugins/local.mjs",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			left, err := resolve(orderIdentityExtension(
				t,
				test.carrier,
				test.target,
				target.ScopeProject,
				test.left,
			).CarrierKey())
			if err != nil {
				t.Fatalf("resolve left alias: %v", err)
			}
			right, err := resolve(orderIdentityExtension(
				t,
				test.carrier,
				test.target,
				target.ScopeProject,
				test.right,
			).CarrierKey())
			if err != nil {
				t.Fatalf("resolve right alias: %v", err)
			}
			if left != right {
				t.Fatalf("alias identities differ: %q != %q", left, right)
			}
		})
	}
}

func TestExtensionOrderIdentityResolverRejectsUnnormalizedPiLocalSource(t *testing.T) {
	root := t.TempDir()
	paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	resolve := ExtensionOrderIdentityResolver(paths)

	tests := []struct {
		name   string
		scope  target.Scope
		source string
	}{
		{name: "global relative", scope: target.ScopeGlobal, source: "plugins/local"},
		{name: "project absolute", scope: target.ScopeProject, source: filepath.Join(root, "plugins", "local")},
		{name: "home relative", scope: target.ScopeGlobal, source: "~/plugins/local"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			extension := orderIdentityExtension(
				t,
				desiredextension.CarrierPiPackage,
				target.TargetPi,
				test.scope,
				test.source,
			)
			_, err := resolve(extension.CarrierKey())
			if err == nil || !strings.Contains(err.Error(), "manifest") {
				t.Fatalf("error = %v, want manifest normalization rejection", err)
			}
		})
	}
}

func orderIdentityExtension(
	t *testing.T,
	carrier desiredextension.Carrier,
	selectedTarget target.Target,
	scope target.Scope,
	source string,
) desiredextension.Extension {
	t.Helper()
	return desiredtest.Extension(t, desiredextension.Spec{
		Name:    "example",
		Carrier: carrier,
		Target:  selectedTarget,
		Scope:   scope,
		Source: desiredtest.ExtensionSource(
			t,
			desiredextension.SourceKindHostSource,
			source,
		),
	})
}
