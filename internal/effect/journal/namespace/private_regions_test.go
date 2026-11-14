package namespace

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/output"
	nativehostpath "github.com/isty2e/daem/internal/output/hostpath/native"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func capturedProtection(t *testing.T, work, home string, values map[string]string) ReservationSelectors {
	t.Helper()
	locations, err := nativehostpath.NewLocations(work, home, values)
	if err != nil {
		t.Fatal(err)
	}
	contracts, err := configuration.NativeProtectionLocations()
	if err != nil {
		t.Fatal(err)
	}
	var selectors []ReservationSelector
	for _, contract := range contracts {
		location, err := locations.ResolveProtection(contract.Reference())
		var selector ReservationSelector
		if err != nil {
			selector, err = UnavailableReservationSelector(contract.Reference(), location.Source(), err)
		} else if location.Absent() {
			selector, err = AbsentReservationSelector(contract.Reference(), location.Source())
		} else {
			selector, err = NewReservationSelector(contract.Reference(), location.Path())
		}
		if err != nil {
			t.Fatal(err)
		}
		selectors = append(selectors, selector.DescribedBy(location.Source()))
	}
	result, err := NewReservationSelectors(selectors)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func observePrivateTarget(t *testing.T, base, selected string, protection ReservationSelectors) (DocumentNamespace, *nativeNamespaceReader, error) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(base, ".pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(selected, filepath.Join(base, ".pi", "shared.json")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	manifest := nativeNamespaceRoot(t, base)
	roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: manifest})
	address := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/shared.json")
	reader := &nativeNamespaceReader{RootedReader: journalTestFilesystem()}
	observed, err := Observe(t.Context(), []aggregate.DocumentAddress{address}, roots, protection, reader, imageBudget(t))
	return observed, reader, err
}

func TestPrivateStoreProtectionIsIndependentOfTheSelectedHost(t *testing.T) {
	for _, row := range []struct {
		name, relative string
		reserved       bool
	}{
		{"authentication", "data/opencode/auth.json", true},
		{"channel database", "data/opencode/opencode-local.db", true},
		{"runtime state", "state/opencode/model.json", true},
		{"cached code", "cache/opencode/bin/helper", true},
		{"workspace", "data/opencode/worktree/team/settings.json", false},
		{"ordinary configuration", "config/opencode/preferences.json", false},
		{"plugin configuration", "config/opencode/plugins/settings.json", false},
		{"plugin script", "config/opencode/plugins/reviewer.ts", true},
		{"non-directory plugin search", "config/opencode/plugin", false},
	} {
		t.Run(row.name, func(t *testing.T) {
			base, home := t.TempDir(), t.TempDir()
			selected := filepath.Join(base, filepath.FromSlash(row.relative))
			if err := os.MkdirAll(filepath.Dir(selected), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(selected, []byte(`{"theme":"dark"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			protection := capturedProtection(t, base, home, map[string]string{
				"XDG_CONFIG_HOME": filepath.Join(base, "config"), "XDG_DATA_HOME": filepath.Join(base, "data"),
				"XDG_STATE_HOME": filepath.Join(base, "state"), "XDG_CACHE_HOME": filepath.Join(base, "cache"),
			})
			observed, reader, err := observePrivateTarget(t, base, selected, protection)
			if row.reserved {
				var failure *ProtectionError
				if !errors.Is(err, configuration.ErrReserved) || !errors.As(err, &failure) || failure.Kind() != ProtectionReserved || failure.Host() != target.TargetOpenCode || reader.imageReads != 0 {
					t.Fatal("an unselected private store reached document bytes or lost its resource classification", err, reader.imageReads)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			image, _, _, err := observed.ReadReferent(t.Context(), 1024, imageBudget(t))
			if err != nil || string(image.Content()) != `{"theme":"dark"}` {
				t.Fatal("ordinary settings or user workspaces were over-reserved", image, err)
			}
		})
	}
}

func TestProjectLoaderProtectionUsesTheSharedFacetProjection(t *testing.T) {
	for _, row := range []struct {
		path string
		host target.Target
	}{
		{".pi/extensions/package/index.ts", target.TargetPi},
		{".opencode/plugins/reviewer.ts", target.TargetOpenCode},
		{".opencode/package.json", target.TargetOpenCode},
	} {
		t.Run(row.path, func(t *testing.T) {
			base, home := t.TempDir(), t.TempDir()
			selected := filepath.Join(base, filepath.FromSlash(row.path))
			if err := os.MkdirAll(filepath.Dir(selected), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(selected, []byte("fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
			_, reader, err := observePrivateTarget(t, base, selected, capturedProtection(t, base, home, nil))
			var failure *ProtectionError
			if !errors.Is(err, configuration.ErrReserved) || !errors.As(err, &failure) || failure.Host() != row.host || reader.imageReads != 0 {
				t.Fatal("project code escaped the common protection rules", err, reader.imageReads)
			}
		})
	}
}

func TestScopedInstalledPackagesCannotEscapeThroughAnotherHostAlias(t *testing.T) {
	base, home := t.TempDir(), t.TempDir()
	configurationRoot := filepath.Join(base, "configuration", "opencode")
	scope := filepath.Join(configurationRoot, "node_modules", "@fixture")
	if err := os.MkdirAll(scope, 0o700); err != nil {
		t.Fatal(err)
	}
	checkout := filepath.Join(base, "checkout")
	if err := os.MkdirAll(checkout, 0o700); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(checkout, "index.js")
	if err := os.WriteFile(selected, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(checkout, filepath.Join(scope, "package")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	protection := capturedProtection(t, base, home, map[string]string{"XDG_CONFIG_HOME": filepath.Dir(configurationRoot)})
	_, reader, err := observePrivateTarget(t, base, selected, protection)
	var failure *ProtectionError
	if !errors.Is(err, configuration.ErrReserved) || !errors.As(err, &failure) || failure.Host() != target.TargetOpenCode || reader.imageReads != 0 {
		t.Fatal("scoped package directory links escaped installed-code protection", err, reader.imageReads)
	}
}

func TestNonDirectorySearchRootsDoNotBlockUnrelatedConfiguration(t *testing.T) {
	for _, row := range []struct {
		name, relative   string
		home, linkedFile bool
	}{
		{"global npm", "config/opencode/node_modules", false, false},
		{"linked npm", "config/opencode/node_modules", false, true},
		{"project npm", ".opencode/node_modules", false, false},
		{"project loader", ".pi/extensions", false, false},
		{"global loader", ".pi/agent/extensions", true, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			base, home := t.TempDir(), t.TempDir()
			anchor := base
			if row.home {
				anchor = home
			}
			directory := filepath.Join(anchor, filepath.FromSlash(row.relative))
			if err := os.MkdirAll(filepath.Dir(directory), 0o700); err != nil {
				t.Fatal(err)
			}
			selected := filepath.Join(base, "ordinary.json")
			if err := os.WriteFile(selected, []byte(`{"ordinary":true}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if row.linkedFile {
				file := filepath.Join(base, "search-file")
				if err := os.WriteFile(file, []byte("not a directory"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(file, directory); err != nil {
					t.Skipf("symlinks are unavailable: %v", err)
				}
			} else if err := os.WriteFile(directory, []byte("not a directory"), 0o600); err != nil {
				t.Fatal(err)
			}
			protection := capturedProtection(t, base, home, map[string]string{"XDG_CONFIG_HOME": filepath.Join(base, "config")})
			observed, _, err := observePrivateTarget(t, base, selected, protection)
			if err != nil {
				t.Fatal("a non-directory search root blocked unrelated configuration", err)
			}
			if image, _, _, err := observed.ReadReferent(t.Context(), 1024, imageBudget(t)); err != nil || string(image.Content()) != `{"ordinary":true}` {
				t.Fatal("unrelated configuration was not readable", err)
			}
		})
	}
}

func TestExplicitPrivateFilesProtectRelatedFilesWithoutReservingTheirParent(t *testing.T) {
	for _, row := range []struct {
		name, selected, reference string
		databaseLink              bool
	}{
		{"database", "private/selected.db", "opencode-database-override", false},
		{"database journal", "private/selected.db-journal", "opencode-database-override", false},
		{"resolved database WAL", "vault/real.db-wal", "opencode-database-override", true},
		{"route database WAL", "private/selected.db-wal", "opencode-database-override", true},
		{"metadata", "private/meta.json", "opencode-plugin-metadata-override", false},
		{"ordinary sibling", "private/preferences.json", "", false},
	} {
		t.Run(row.name, func(t *testing.T) {
			base, home := t.TempDir(), t.TempDir()
			for _, dir := range []string{"private", "vault"} {
				if err := os.MkdirAll(filepath.Join(base, dir), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			database := filepath.Join(base, "private", "selected.db")
			if row.databaseLink {
				if err := os.Symlink(filepath.Join(base, "vault", "real.db"), database); err != nil {
					t.Skipf("symlinks are unavailable: %v", err)
				}
			} else if err := os.WriteFile(database, []byte("fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
			selected := filepath.Join(base, filepath.FromSlash(row.selected))
			if err := os.WriteFile(selected, []byte(`{"ordinary":true}`), 0o600); err != nil {
				t.Fatal(err)
			}
			protection := capturedProtection(t, base, home, map[string]string{
				"OPENCODE_DB": database, "OPENCODE_PLUGIN_META_FILE": filepath.Join(base, "private", "meta.json"),
			})
			observed, reader, err := observePrivateTarget(t, base, selected, protection)
			if row.reference == "" {
				if err != nil {
					t.Fatal("file selectors reserved an unrelated file in their parent directory", err)
				}
				if _, _, _, err := observed.ReadReferent(t.Context(), 1024, imageBudget(t)); err != nil {
					t.Fatal(err)
				}
				return
			}
			var failure *ProtectionError
			if !errors.Is(err, configuration.ErrReserved) || !errors.As(err, &failure) || failure.Host() != target.TargetOpenCode || failure.Reference().Identity() != row.reference || reader.imageReads != 0 {
				t.Fatal("explicit private file or companion reached document bytes", err, reader.imageReads)
			}
			for _, suffix := range []string{"-journal", "-wal"} {
				if strings.HasSuffix(row.selected, suffix) && !strings.HasSuffix(failure.Endpoint(), suffix) {
					t.Fatal("related-file refusal lost the affected companion label", failure.Endpoint())
				}
			}
		})
	}
}

func TestListedDatabaseCompanionsUseTheSameProtectionForDifferentHosts(t *testing.T) {
	for _, row := range []struct {
		host     target.Target
		variable string
		store    string
		database string
	}{
		{target.TargetOpenCode, "XDG_DATA_HOME", "data/opencode", "opencode-local.db"},
		{target.TargetCodex, "CODEX_HOME", "codex", "state_5.sqlite"},
	} {
		t.Run(string(row.host), func(t *testing.T) {
			base, home := t.TempDir(), t.TempDir()
			store := filepath.Join(base, filepath.FromSlash(row.store))
			if err := os.MkdirAll(store, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(base, "vault"), 0o700); err != nil {
				t.Fatal(err)
			}
			databaseTarget := filepath.Join(base, "vault", "real.db")
			if err := os.Symlink(databaseTarget, filepath.Join(store, row.database)); err != nil {
				t.Skipf("symlinks are unavailable: %v", err)
			}
			selected := databaseTarget + "-wal"
			if err := os.WriteFile(selected, []byte("fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
			value := store
			if row.host == target.TargetOpenCode {
				value = filepath.Dir(store)
			}
			protection := capturedProtection(t, base, home, map[string]string{row.variable: value})
			_, reader, err := observePrivateTarget(t, base, selected, protection)
			var failure *ProtectionError
			if !errors.Is(err, configuration.ErrReserved) || !errors.As(err, &failure) || failure.Host() != row.host || reader.imageReads != 0 {
				t.Fatal("a listed database's physical journal was treated as ordinary data", err, reader.imageReads)
			}
		})
	}
}

func TestRawDefinitionDirectoryDoesNotUseTheJsonJoinedLocation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Win32 resolves lexical parents before reparse traversal")
	}
	base, home := t.TempDir(), t.TempDir()
	for _, directory := range []string{"actual/inner", "actual/extra/plugins", "extra/plugins"} {
		if err := os.MkdirAll(filepath.Join(base, filepath.FromSlash(directory)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(base, "actual", "inner"), filepath.Join(base, "link")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	selected := filepath.Join(base, "actual", "extra", "plugins", "reviewer.ts")
	if err := os.WriteFile(selected, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	protection := capturedProtection(t, base, home, map[string]string{"OPENCODE_CONFIG_DIR": filepath.Join(base, "link") + "/../extra"})
	_, reader, err := observePrivateTarget(t, base, selected, protection)
	var failure *ProtectionError
	if !errors.Is(err, configuration.ErrReserved) || !errors.As(err, &failure) || failure.Reference().Identity() != "opencode-additional-sources" || reader.imageReads != 0 {
		t.Fatal("raw directory enumeration was replaced by JSON's lexical join", err, reader.imageReads)
	}
}

func TestPrivateStoreLinksAreRecheckedBeforeReferentAcquisition(t *testing.T) {
	base, home := t.TempDir(), t.TempDir()
	selected := filepath.Join(base, "ordinary.json")
	if err := os.WriteFile(selected, []byte(`{"ordinary":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(base, "data", "opencode")
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	protection := capturedProtection(t, base, home, map[string]string{"XDG_DATA_HOME": filepath.Dir(data)})
	observed, reader, err := observePrivateTarget(t, base, selected, protection)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(selected, filepath.Join(data, "auth.json")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	capability, err := observed.AcquireReferent(t.Context(), imageBudget(t))
	if capability != nil || !errors.Is(err, configuration.ErrReserved) || reader.imageReads != 0 {
		t.Fatal("a newly linked private endpoint bypassed acquisition protection", err, reader.imageReads)
	}
	if writer, _, err := observed.WriterBinding(t.Context(), imageBudget(t)); writer != nil || !errors.Is(err, configuration.ErrReserved) {
		t.Fatal("a private-store link supplied a writer binding", err)
	}
}
