package nativeconfig

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/output"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func TestSelectedObservationChecksUnselectedPrivateStores(t *testing.T) {
	for _, row := range []struct {
		name, selected string
		failure        journalnamespace.ProtectionFailureKind
		invalidMeta    bool
		emptyConfig    bool
	}{
		{name: "authentication", selected: "data/opencode/auth.json", failure: journalnamespace.ProtectionReserved},
		{name: "explicit database WAL", selected: "private/selected.db-wal", failure: journalnamespace.ProtectionReserved},
		{name: "user workspace", selected: "data/opencode/worktree/team/settings.json"},
		{name: "ordinary configuration", selected: "configuration/opencode/preferences.json"},
		{name: "unavailable metadata", selected: "ordinary.json", failure: journalnamespace.ProtectionUnverified, invalidMeta: true},
		{name: "empty override core configuration", selected: "opencode.json", emptyConfig: true},
		{name: "empty override is not an installation directory", selected: "package.json", emptyConfig: true},
	} {
		t.Run(row.name, func(t *testing.T) {
			root, home := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			for variable, directory := range map[string]string{
				"XDG_CONFIG_HOME": "configuration", "XDG_DATA_HOME": "data",
				"XDG_STATE_HOME": "state", "XDG_CACHE_HOME": "cache",
			} {
				t.Setenv(variable, filepath.Join(root, directory))
			}
			t.Setenv("OPENCODE_DB", filepath.Join(root, "private", "selected.db"))
			if row.emptyConfig {
				t.Setenv("OPENCODE_CONFIG_DIR", "")
			}
			if row.invalidMeta {
				t.Setenv("OPENCODE_PLUGIN_META_FILE", "")
			}
			selected := filepath.Join(root, filepath.FromSlash(row.selected))
			if err := os.MkdirAll(filepath.Dir(selected), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(selected, []byte(`{"ordinary":true}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(root, ".pi"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(selected, filepath.Join(root, ".pi", "shared.json")); err != nil {
				t.Skipf("symlinks are unavailable: %v", err)
			}
			paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
			if err != nil {
				t.Fatal(err)
			}
			epoch, err := LoadPersistenceEpoch(t.Context(), paths)
			if err != nil {
				t.Fatal(err)
			}
			destination, _ := output.Parse(".pi/shared.json")
			address, err := aggregate.NewDocumentAddress(target.TargetPi, target.ScopeProject, destination)
			if err != nil {
				t.Fatal(err)
			}
			observed, err := ObserveSelectedDocuments(t.Context(), paths, &epoch, []aggregate.DocumentAddress{address})
			if row.failure != 0 {
				var failure *journalnamespace.ProtectionError
				if !errors.As(err, &failure) || failure.Kind() != row.failure || failure.Host() != target.TargetOpenCode {
					t.Fatal("selected observation lost the unselected host's protection", err)
				}
				if row.failure == journalnamespace.ProtectionReserved && !errors.Is(err, configuration.ErrReserved) {
					t.Fatal("reserved resource lost its contract classification", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				input, present := observed.NativeInput()
				if !present {
					t.Fatal("ordinary document lost its native evidence")
				}
				document, _, present := input.Documents.Evidence().Document(address)
				if !present || string(document.Content()) != `{"ordinary":true}` {
					t.Fatal("ordinary settings/workspaces did not retain their bytes")
				}
				if row.emptyConfig && row.selected == "opencode.json" {
					groups := input.Documents.Evidence().Namespaces()
					if len(groups) != 1 || groups[0].RequireCompletePhysicalRules() != nil || !slices.ContainsFunc(groups[0].PhysicalRoles(), func(role configuration.DocumentSpec) bool {
						return role.Target() == target.TargetOpenCode && role.Class() == configuration.SharedConfiguration && role.Grammar() == configuration.JSONC
					}) {
						t.Fatal("a core-only empty override lost its global document role")
					}
				}
				if err := observed.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := os.Stat(paths.StateDir); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("protection planning created metadata", err)
			}
		})
	}
}
