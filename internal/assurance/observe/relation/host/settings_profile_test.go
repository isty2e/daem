package relationhost

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe/codexplugin"
	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

func settingsProfileKey(t *testing.T, carrier desiredextension.Carrier, selected target.Target, scope target.Scope, source string) desiredextension.CarrierKey {
	t.Helper()
	kind := desiredextension.SourceKindHostSource
	if carrier == desiredextension.CarrierCodexPlugin || carrier == desiredextension.CarrierClaudeCodePlugin {
		kind = desiredextension.SourceKindMarketplace
	}
	ref, err := desiredextension.NewSourceRef(kind, source)
	if err != nil {
		t.Fatal(err)
	}
	key, err := desiredextension.NewCarrierKey(carrier, selected, scope, ref)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func settingsProfilePaths(t *testing.T) daempaths.Paths {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(root, "global-pi"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "global-codex"))
	paths, err := daempaths.Resolve(filepath.Join(root, "workspace", "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func settingsProfileDocument(t *testing.T, syntax configdocument.Syntax, text string) configdocument.Document {
	t.Helper()
	document, err := configdocument.Parse(t.Context(), []byte(text), syntax, configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64})
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func TestNativeSettingsProfileOperationMatrix(t *testing.T) {
	for _, row := range []struct {
		carrier    desiredextension.Carrier
		target     target.Target
		scopes     []target.Scope
		source     string
		exclusions int
	}{
		{desiredextension.CarrierPiPackage, target.TargetPi, []target.Scope{target.ScopeProject, target.ScopeGlobal}, "npm:@acme/tools@1", 0},
		{desiredextension.CarrierCodexPlugin, target.TargetCodex, []target.Scope{target.ScopeGlobal}, "alpha@market", 0},
		{desiredextension.CarrierClaudeCodePlugin, target.TargetClaudeCode, []target.Scope{target.ScopeProject, target.ScopeGlobal}, "alpha@market", 1},
		{desiredextension.CarrierAntigravityCLIPlugin, target.TargetAntigravityCLI, []target.Scope{target.ScopeGlobal}, "acme/tools", 2},
		{desiredextension.CarrierOpenCodePlugin, target.TargetOpenCode, []target.Scope{target.ScopeProject, target.ScopeGlobal}, "@acme/tools@1", 4},
	} {
		for _, scope := range row.scopes {
			for _, operation := range []lock.OperationKind{lock.OperationInstall, lock.OperationRemove, lock.OperationRefresh} {
				t.Run(string(row.carrier)+"/"+string(scope)+"/"+string(operation), func(t *testing.T) {
					paths := settingsProfilePaths(t)
					key := settingsProfileKey(t, row.carrier, row.target, scope, row.source)
					selected, err := NativeSettingsProfileFor(key, operation)
					if err != nil {
						t.Fatal(err)
					}
					want := lock.ActuationDelegatedHostRoute
					if row.carrier == desiredextension.CarrierOpenCodePlugin && operation == lock.OperationRemove {
						want = lock.ActuationDirectProjection
					}
					if err := selected.RequireActuation(want); err != nil {
						t.Fatal(err)
					}
					if err := selected.RequireActuation(lock.ActuationNoMutation); err == nil {
						t.Fatal("profile grants a different actuation")
					}
					exclusions, err := selected.ExclusionPaths(paths)
					count := row.exclusions
					if row.carrier == desiredextension.CarrierClaudeCodePlugin && scope == target.ScopeProject {
						count++
					}
					if err != nil || len(exclusions) != count {
						t.Fatal("wrong host exclusion facts", err, exclusions)
					}
					_, err = selected.BindInvocationRewrites(paths, nil)
					if (err == nil) != (want == lock.ActuationDelegatedHostRoute) {
						t.Fatal("direct removal gained an invocation rewrite", err)
					}
					if operation == lock.OperationRefresh {
						fence, err := selected.FencePaths(paths)
						if row.carrier == desiredextension.CarrierPiPackage {
							count = 2
						}
						if row.carrier == desiredextension.CarrierCodexPlugin {
							count = 1
						}
						if err != nil || len(fence) != count {
							t.Fatal("wrong declaration-independent protection fence", err, fence)
						}
					}
					if _, err := NativeSettingsProfileFor(key, lock.OperationObserve); err == nil {
						t.Fatal("unsupported operation gained a profile")
					}
				})
			}
		}
	}
	if _, err := NativeSettingsProfileFor(desiredextension.CarrierKey{}, lock.OperationInstall); err == nil {
		t.Fatal("invalid key gained a profile")
	}
	if _, err := (NativeSettingsProfile{}).ExclusionPaths(daempaths.Paths{}); err == nil {
		t.Fatal("zero profile granted an exclusion envelope")
	}
}

func TestNativeSettingsPiBindingRetainsOperationAndAliasSemantics(t *testing.T) {
	paths := settingsProfilePaths(t)
	project, _ := pipackage.SettingsDocumentAddress(target.ScopeProject)
	global, _ := pipackage.SettingsDocumentAddress(target.ScopeGlobal)
	groups := [][]aggregate.DocumentAddress{{project, global}}
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		placement, err := profile.ResolveHostConfigPlacement(profile.HostConfigPlacementInput{Target: target.TargetPi, Scope: scope, Path: "settings.json", Ownership: hostconfig.OwnershipKeys})
		if err != nil {
			t.Fatal(err)
		}
		address, err := aggregate.NewDocumentAddress(placement.Target(), placement.Scope(), placement.Destination())
		wanted, other := project, global
		if scope == target.ScopeGlobal {
			wanted, other = global, project
		}
		if err != nil || address != wanted {
			t.Fatal("Pi host and placement identity disagree", err)
		}
		key := settingsProfileKey(t, desiredextension.CarrierPiPackage, target.TargetPi, scope, "npm:@acme/tools@1")
		selected, _ := NativeSettingsProfileFor(key, lock.OperationInstall)
		bound, err := selected.BindInvocationRewrites(paths, groups)
		if err != nil {
			t.Fatal(err)
		}
		before := settingsProfileDocument(t, configdocument.JSON, `{"packages":[],"foreign":1}`)
		after := settingsProfileDocument(t, configdocument.JSON, `{"packages":["npm:@acme/tools@1"],"foreign":1}`)
		if same, err := bound.PreservesOutside(t.Context(), other, before, after); err != nil || !same {
			t.Fatal("operation-scope rewrite lost physical alias coverage", err)
		}
		strict, err := selected.BindInvocationRewrites(paths, nil)
		if err != nil {
			t.Fatal(err)
		}
		if same, err := strict.PreservesOutside(t.Context(), wanted, before, after); err != nil || same {
			t.Fatal("no retained match gained an exception", err)
		}
	}
	key := settingsProfileKey(t, desiredextension.CarrierPiPackage, target.TargetPi, target.ScopeProject, "./tools")
	before := settingsProfileDocument(t, configdocument.JSON, `{"packages":[]}`)
	after := settingsProfileDocument(t, configdocument.JSON, `{"packages":["../tools"]}`)
	install, _ := NativeSettingsProfileFor(key, lock.OperationInstall)
	installed, err := install.BindInvocationRewrites(paths, groups)
	if err != nil {
		t.Fatal(err)
	}
	if same, err := installed.PreservesOutside(t.Context(), global, before, after); err != nil || !same {
		t.Fatal("install did not use operation-scope local context", err)
	}
	refresh, _ := NativeSettingsProfileFor(key, lock.OperationRefresh)
	refreshed, err := refresh.BindInvocationRewrites(paths, groups)
	if err != nil {
		t.Fatal(err)
	}
	if same, err := refreshed.PreservesOutside(t.Context(), project, before, after); err != nil || !same {
		t.Fatal("refresh lost project local context", err)
	}
	if same, err := refreshed.PreservesOutside(t.Context(), global, before, after); err != nil || same {
		t.Fatal("refresh widened per-alias local context", err)
	}
}

func TestNativeSettingsCodexKeepsPluginAndMarketplaceRewritesDistinct(t *testing.T) {
	paths := settingsProfilePaths(t)
	key := settingsProfileKey(t, desiredextension.CarrierCodexPlugin, target.TargetCodex, target.ScopeGlobal, "alpha@market")
	address, _ := codexplugin.ConfigDocumentAddress()
	original := "[plugins.'alpha@market']\nenabled=true\n[plugins.'beta@market']\nenabled=true\n[marketplaces.market]\nsource='/old'\n"
	before := settingsProfileDocument(t, configdocument.TOML, original)
	plugin := settingsProfileDocument(t, configdocument.TOML, strings.Replace(original, "enabled=true", "enabled=false", 1))
	market := settingsProfileDocument(t, configdocument.TOML, strings.Replace(original, "'/old'", "'/new'", 1))
	for _, operation := range []lock.OperationKind{lock.OperationInstall, lock.OperationRemove, lock.OperationRefresh} {
		selected, _ := NativeSettingsProfileFor(key, operation)
		bound, err := selected.BindInvocationRewrites(paths, [][]aggregate.DocumentAddress{{address}})
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range []struct {
			after configdocument.Document
			want  bool
		}{{plugin, operation != lock.OperationRefresh}, {market, operation == lock.OperationRefresh}} {
			if same, err := bound.PreservesOutside(t.Context(), address, before, row.after); err != nil || same != row.want {
				t.Fatalf("%s mixed plugin and marketplace permissions: same=%t want=%t err=%v", operation, same, row.want, err)
			}
		}
	}
}
