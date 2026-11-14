package namespace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeProtectionUsesStablePrefixesWithoutAssumingUnicodeIdentity(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("normalization-sensitive missing entries use Darwin observations")
	}
	for _, scenario := range []struct {
		host     target.Target
		relative string
		kind     ProtectionFailureKind
	}{
		{target.TargetCodex, "extension-settings/Café.toml", 0},
		{target.TargetCodex, "ſessions/item", ProtectionUnverified},
		{target.TargetCodex, "sessions/Café", ProtectionReserved},
		{target.TargetPi, "extensions/Päckage/index.ts", ProtectionUnverified},
		{target.TargetPi, "extensions/pkg/ındex.ts", ProtectionUnverified},
	} {
		t.Run(scenario.relative, func(t *testing.T) {
			base := t.TempDir()
			pi, codex := filepath.Join(base, "pi"), filepath.Join(base, "codex")
			selectedPath, role, prefix := codex, output.RootCodexHome, "@codex-home/"
			if scenario.host == target.TargetPi {
				selectedPath, role, prefix = pi, output.RootPiAgent, "@pi-agent/"
			}
			selection := nativeNamespaceRoot(t, selectedPath)
			roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: nativeNamespaceRoot(t, base), role: selection})
			address := nativeNamespaceAddress(t, scenario.host, target.ScopeGlobal, prefix+scenario.relative)
			value, err := Observe(t.Context(), []aggregate.DocumentAddress{address}, roots, protectionRoots(t, pi, codex), journalTestFilesystem(), imageBudget(t))
			if scenario.kind == 0 {
				if err != nil {
					t.Fatal("stable ordinary prefix became unknown reserved data", err)
				}
				if _, provisional := value.observation.Provisional(); !provisional {
					t.Fatal("region comparison promoted the future entry to exact authority")
				}
				return
			}
			var failure *ProtectionError
			if !errors.As(err, &failure) || failure.Kind() != scenario.kind || failure.Host() != scenario.host || errors.Is(err, configuration.ErrReserved) != (scenario.kind == ProtectionReserved) {
				t.Fatal("Unicode reservation evidence lost its uncertainty or known containment", err)
			}
		})
	}
}

func TestNativeProtectionFollowsOutboundPatternEndpoints(t *testing.T) {
	for _, scenario := range []struct {
		name, endpoint       string
		host                 target.Target
		packageLink, missing bool
	}{
		{name: "runtime database", host: target.TargetCodex, endpoint: "state_5.sqlite"},
		{name: "daemon metadata", host: target.TargetCodex, endpoint: "app-server-daemon/app-server.pid"},
		{name: "loose extension", host: target.TargetPi, endpoint: "extensions/pkg.ts"},
		{name: "package entrypoint", host: target.TargetPi, endpoint: "extensions/pkg/index.ts"},
		{name: "linked package", host: target.TargetPi, endpoint: "extensions/pkg", packageLink: true},
		{name: "future extension", host: target.TargetPi, endpoint: "extensions/pkg.ts", missing: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			base := t.TempDir()
			manifest := nativeNamespaceRoot(t, base)
			pi, codex := filepath.Join(base, "pi-global"), filepath.Join(base, "codex-global")
			root := pi
			if scenario.host == target.TargetCodex {
				root = codex
			}
			selected := filepath.Join(base, ".pi", "extra.json")
			if scenario.packageLink {
				selected = filepath.Join(base, ".pi", "index.ts")
			}
			if err := os.MkdirAll(filepath.Dir(selected), 0o700); err != nil {
				t.Fatal(err)
			}
			if !scenario.missing {
				if err := os.WriteFile(selected, []byte(`{"fixture":true}`), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			endpoint := filepath.Join(root, filepath.FromSlash(scenario.endpoint))
			if err := os.MkdirAll(filepath.Dir(endpoint), 0o700); err != nil {
				t.Fatal(err)
			}
			referent := selected
			if scenario.packageLink {
				referent = filepath.Dir(selected)
			}
			if err := os.Symlink(referent, endpoint); err != nil {
				t.Skipf("symlinks are unavailable: %v", err)
			}
			// Ordinary non-code entries must not become failed package probes.
			if scenario.host == target.TargetPi {
				if err := os.WriteFile(filepath.Join(pi, "extensions", "README.md"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			address := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/"+filepath.Base(selected))
			roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: manifest})
			reader := &nativeNamespaceReader{RootedReader: journalTestFilesystem()}
			_, err := Observe(t.Context(), []aggregate.DocumentAddress{address}, roots, protectionRoots(t, pi, codex), reader, imageBudget(t))
			var failure *ProtectionError
			if !errors.As(err, &failure) || failure.Kind() != ProtectionReserved || failure.Host() != scenario.host || !errors.Is(err, configuration.ErrReserved) || len(reader.capabilities) != 0 {
				t.Fatal("outbound pattern endpoint was read or lost its reserved classification", err)
			}
		})
	}
}

func TestNativeProtectionTracksModuleDirectoryBeforeFutureCreation(t *testing.T) {
	for _, folded := range []bool{false, true} {
		name := "outbound directory"
		if folded {
			name = "stored folded directory"
		}
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			manifest := nativeNamespaceRoot(t, base)
			pi, codex := filepath.Join(base, "pi"), filepath.Join(base, "codex")
			module := filepath.Join(base, ".pi")
			if err := os.MkdirAll(pi, 0o700); err != nil {
				t.Fatal(err)
			}
			if folded {
				module = filepath.Join(pi, "extenſions")
			}
			if err := os.Mkdir(module, 0o700); err != nil {
				t.Fatal(err)
			}
			if folded {
				if _, err := os.Stat(filepath.Join(pi, "extensions")); errors.Is(err, os.ErrNotExist) {
					t.Skip("test volume does not equate the stored module name")
				} else if err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(module, filepath.Join(base, ".pi")); err != nil {
					t.Skipf("symlinks are unavailable: %v", err)
				}
			} else if err := os.Symlink(module, filepath.Join(pi, "extensions")); err != nil {
				t.Skipf("symlinks are unavailable: %v", err)
			}
			roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: manifest})
			for _, leaf := range []string{"new.ts", "notes.json"} {
				address := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/"+leaf)
				_, err := Observe(t.Context(), []aggregate.DocumentAddress{address}, roots, protectionRoots(t, pi, codex), journalTestFilesystem(), imageBudget(t))
				if leaf == "notes.json" {
					if err != nil {
						t.Fatal("ordinary file below the module directory was reserved", err)
					}
					continue
				}
				var failure *ProtectionError
				if !errors.As(err, &failure) || failure.Kind() != ProtectionReserved || failure.Host() != target.TargetPi || !errors.Is(err, configuration.ErrReserved) {
					t.Fatal("a future entrypoint escaped the observed module directory", err)
				}
			}
		})
	}
}

func TestNativeProtectionFindsStoredRuntimeNameEquivalent(t *testing.T) {
	for _, spelling := range []struct{ stored, host string }{
		{"ſtate_5.sqlite", "state_5.sqlite"},
		{"ﬆate_5.sqlite", "state_5.sqlite"},
		{"ﬅate_5.sqlite", "state_5.sqlite"},
		{"thread_hiﬆory_5.sqlite", "thread_history_5.sqlite"},
	} {
		t.Run(spelling.stored, func(t *testing.T) {
			base := t.TempDir()
			manifest := nativeNamespaceRoot(t, base)
			pi, codex := filepath.Join(base, "pi"), filepath.Join(base, "codex")
			if err := os.MkdirAll(filepath.Join(base, ".pi"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(codex, 0o700); err != nil {
				t.Fatal(err)
			}
			stored := filepath.Join(codex, spelling.stored)
			if err := os.WriteFile(stored, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(codex, spelling.host)); errors.Is(err, os.ErrNotExist) {
				t.Skip("test volume does not equate the stored runtime name")
			} else if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(stored, filepath.Join(base, ".pi", "extra.json")); err != nil {
				t.Skipf("symlinks are unavailable: %v", err)
			}
			address := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/extra.json")
			roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: manifest})
			_, err := Observe(t.Context(), []aggregate.DocumentAddress{address}, roots, protectionRoots(t, pi, codex), journalTestFilesystem(), imageBudget(t))
			var failure *ProtectionError
			if !errors.As(err, &failure) || failure.Kind() != ProtectionReserved || failure.Host() != target.TargetCodex || !errors.Is(err, configuration.ErrReserved) {
				t.Fatal("a host-addressable runtime filename escaped discovery", err)
			}
		})
	}
}

func TestNativeRuntimeCandidateDoesNotDefineFilesystemIdentity(t *testing.T) {
	base := t.TempDir()
	manifest := nativeNamespaceRoot(t, base)
	pi, codex := filepath.Join(base, "pi"), filepath.Join(base, "codex")
	for _, directory := range []string{filepath.Join(base, ".pi"), codex} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	stored := filepath.Join(codex, "ſtate_5.sqlite")
	if err := os.WriteFile(stored, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(codex, "state_5.sqlite")); err == nil {
		t.Skip("test volume equates the candidate and stored names")
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if err := os.Symlink(stored, filepath.Join(base, ".pi", "extra.json")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	address := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/extra.json")
	roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: manifest})
	if _, err := Observe(t.Context(), []aggregate.DocumentAddress{address}, roots, protectionRoots(t, pi, codex), journalTestFilesystem(), imageBudget(t)); err != nil {
		t.Fatal("candidate normalization invented a filesystem equivalence", err)
	}
}

func TestNativeProtectionAllowsUnrelatedFilesBesideExtensionPackages(t *testing.T) {
	base := t.TempDir()
	manifest := nativeNamespaceRoot(t, base)
	pi, codex := filepath.Join(base, "pi-global"), filepath.Join(base, "codex-global")
	for _, relative := range []string{".pi", "pi-global/extensions/pkg"} {
		if err := os.MkdirAll(filepath.Join(base, relative), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, relative := range []string{".pi/extra.json", "pi-global/extensions/README.md", "pi-global/extensions/pkg/notes.json"} {
		if err := os.WriteFile(filepath.Join(base, relative), []byte(`{"fixture":true}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(base, ".pi", "extra.json"), filepath.Join(pi, "extensions", "metadata")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	address := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/extra.json")
	roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: manifest})
	if _, err := Observe(t.Context(), []aggregate.DocumentAddress{address}, roots, protectionRoots(t, pi, codex), journalTestFilesystem(), imageBudget(t)); err != nil {
		t.Fatal("ordinary extension-directory files became indeterminate package candidates", err)
	}
}

func TestNonDirectoryPackageExclusionDoesNotDiscardAncestorOrCleanupFailures(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, leafFailure := rootedpath.ReadDirectoryNamesBounded(t.Context(), file, 256, imageBudget(t))
	if !nonDirectoryObservation(file, leafFailure) {
		t.Fatal("a non-directory leaf did not exclude a package candidate", leafFailure)
	}
	child := filepath.Join(file, "child")
	_, ancestorFailure := rootedpath.ReadDirectoryNamesBounded(t.Context(), child, 256, imageBudget(t))
	if ancestorFailure == nil || nonDirectoryObservation(child, ancestorFailure) {
		t.Fatal("an unavailable ancestor became an irrelevant package candidate", ancestorFailure)
	}
	if nonDirectoryObservation(file, errors.Join(leafFailure, errors.New("fixture cleanup failed"))) {
		t.Fatal("a joined cleanup failure was discarded")
	}
}

func TestNativeProtectionChecksPatternAndRouteOnlyReservations(t *testing.T) {
	for _, scenario := range []struct {
		name         string
		host         target.Target
		relative     string
		routeOnly    bool
		outboundRoot bool
	}{
		{name: "extension entrypoint", host: target.TargetPi, relative: "extensions/pkg/index.ts"},
		{name: "versioned runtime database", host: target.TargetCodex, relative: "state_5.sqlite"},
		{name: "runtime route only", host: target.TargetCodex, relative: "sessions", routeOnly: true},
		{name: "outbound runtime route only", host: target.TargetCodex, relative: "sessions", routeOnly: true, outboundRoot: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			base := t.TempDir()
			manifest := nativeNamespaceRoot(t, base)
			pi, codex := filepath.Join(base, "pi-global"), filepath.Join(base, "codex-global")
			hostRoot := codex
			if scenario.host == target.TargetPi {
				hostRoot = pi
			}
			reserved := filepath.Join(hostRoot, filepath.FromSlash(scenario.relative))
			selected := filepath.Join(base, ".pi", "extra.json")
			if err := os.MkdirAll(filepath.Dir(reserved), 0o700); err != nil {
				t.Fatal(err)
			}
			if scenario.routeOnly {
				data := reserved
				if scenario.outboundRoot {
					data = filepath.Join(base, "runtime-data")
				}
				if err := os.Mkdir(data, 0o700); err != nil {
					t.Fatal(err)
				}
				if scenario.outboundRoot {
					if err := os.Symlink(data, reserved); err != nil {
						t.Skipf("symlinks are unavailable: %v", err)
					}
				}
				ordinary := filepath.Join(base, "ordinary.json")
				if err := os.WriteFile(ordinary, []byte(`{"fixture":true}`), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(ordinary, filepath.Join(data, "extra.json")); err != nil {
					t.Skipf("symlinks are unavailable: %v", err)
				}
				if err := os.Symlink(data, filepath.Dir(selected)); err != nil {
					t.Skipf("symlinks are unavailable: %v", err)
				}
			} else {
				if err := os.WriteFile(reserved, []byte(`{"fixture":true}`), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Dir(selected), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(reserved, selected); err != nil {
					t.Skipf("symlinks are unavailable: %v", err)
				}
			}
			alias := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/extra.json")
			roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: manifest})
			reader := &nativeNamespaceReader{RootedReader: journalTestFilesystem()}
			value, err := Observe(t.Context(), []aggregate.DocumentAddress{alias}, roots, protectionRoots(t, pi, codex), reader, imageBudget(t))
			var failure *ProtectionError
			if !errors.Is(err, configuration.ErrReserved) || !errors.As(err, &failure) || failure.Host() != scenario.host || failure.Kind() != ProtectionReserved || value.Observation().Validate() == nil || len(reader.capabilities) != 0 {
				t.Fatal("physical pattern or route-only reservation did not refuse file access", err)
			}
		})
	}
}

func TestNativeProtectionChecksFutureRuntimeNamesBeforeAnyDirectoryExists(t *testing.T) {
	base := t.TempDir()
	manifest := nativeNamespaceRoot(t, base)
	alias := nativeNamespaceAddress(t, target.TargetCodex, target.ScopeProject, ".codex/state_99.sqlite")
	roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: manifest})
	reader := &nativeNamespaceReader{RootedReader: journalTestFilesystem()}
	_, err := Observe(t.Context(), []aggregate.DocumentAddress{alias}, roots, protectionRoots(t, filepath.Join(base, "pi-global"), filepath.Join(base, ".codex")), reader, imageBudget(t))
	var failure *ProtectionError
	if !errors.As(err, &failure) || failure.Kind() != ProtectionReserved || failure.Host() != target.TargetCodex || len(reader.capabilities) != 0 {
		t.Fatal("future runtime name depended on an existing listed endpoint", err)
	}
	if _, err := os.Stat(filepath.Join(base, ".codex")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("runtime protection created a missing host directory", err)
	}
}

func TestNativeProtectionKeepsConfirmedReservationAheadOfOtherUnavailableRoots(t *testing.T) {
	for _, reservedHost := range []target.Target{target.TargetPi, target.TargetCodex} {
		t.Run(string(reservedHost), func(t *testing.T) {
			base := t.TempDir()
			manifest := nativeNamespaceRoot(t, base)
			if err := os.Mkdir(filepath.Join(base, ".pi"), 0o700); err != nil {
				t.Fatal(err)
			}
			selected := filepath.Join(base, ".pi", "extra.json")
			if err := os.WriteFile(selected, []byte(`{"fixture":true}`), 0o600); err != nil {
				t.Fatal(err)
			}
			var selectedRoots []ReservationSelector
			for _, host := range []target.Target{target.TargetPi, target.TargetCodex} {
				reference, _ := configuration.RootReferenceForScope(host, target.ScopeGlobal)
				selector, err := UnavailableReservationSelector(reference, "selected location", errors.New("fixture location unavailable"))
				if host == reservedHost {
					directory := filepath.Join(base, string(host)+"-global")
					if err := os.Mkdir(directory, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(selected, filepath.Join(directory, "auth.json")); err != nil {
						t.Skipf("symlinks are unavailable: %v", err)
					}
					selector, err = NewReservationSelector(reference, directory)
				}
				if err != nil {
					t.Fatal(err)
				}
				selectedRoots = append(selectedRoots, selector)
			}
			protection, err := NewReservationSelectors(selectedRoots)
			if err != nil {
				t.Fatal(err)
			}
			alias := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/extra.json")
			roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: manifest})
			_, err = Observe(t.Context(), []aggregate.DocumentAddress{alias}, roots, protection, journalTestFilesystem(), imageBudget(t))
			var failure *ProtectionError
			if !errors.As(err, &failure) || failure.Kind() != ProtectionReserved || failure.Host() != reservedHost || !errors.Is(err, configuration.ErrReserved) {
				t.Fatal("unavailable foreign root hid a confirmed reserved endpoint", err)
			}
		})
	}
}

func TestNativeProtectionMissingSelectorsAndCancellationDoNotRead(t *testing.T) {
	base := t.TempDir()
	manifest := nativeNamespaceRoot(t, base)
	alias := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/extra.json")
	roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: manifest})
	reader := &nativeNamespaceReader{RootedReader: journalTestFilesystem()}
	_, err := Observe(t.Context(), []aggregate.DocumentAddress{alias}, roots, ReservationSelectors{}, reader, imageBudget(t))
	var failure *ProtectionError
	if !errors.As(err, &failure) || failure.Kind() != ProtectionUnverified || failure.Uncertainty() != ProtectionSelectorMissing || errors.Is(err, configuration.ErrReserved) || len(reader.capabilities) != 0 {
		t.Fatal("missing protection input became checked-empty protection or a file read", err)
	}
	value, err := Observe(t.Context(), []aggregate.DocumentAddress{alias}, roots, fixtureProtection(t), reader, imageBudget(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	budget := &cancelProtectionBudget{Budget: imageBudget(t), cancel: cancel}
	if err := newPhysicalObserver(value.protection, budget).requireUnreserved(ctx, value.aliases); err != context.Canceled || reader.imageReads != 0 {
		t.Fatal("cancellation during protection became an unavailable-location error", err)
	}
}

type cancelProtectionBudget struct {
	*physicalwork.Budget
	cancel context.CancelFunc
}

func TestNativeProtectionAllocationFailureIsNotAnUnavailableHostLocation(t *testing.T) {
	base := t.TempDir()
	roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: nativeNamespaceRoot(t, base)})
	alias := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/extra.json")
	value, err := Observe(t.Context(), []aggregate.DocumentAddress{alias}, roots, fixtureProtection(t), journalTestFilesystem(), imageBudget(t))
	if err != nil {
		t.Fatal(err)
	}
	quota := errors.New("removal path-component work exceeds operation limit fixture")
	budget := &refuseProtectionBudget{Budget: imageBudget(t), failure: quota}
	err = newPhysicalObserver(value.protection, budget).requireUnreserved(t.Context(), value.aliases)
	var failure *ProtectionError
	if !errors.As(err, &failure) || failure.Kind() != ProtectionUnverified || failure.Uncertainty() != ProtectionWorkLimit || !errors.Is(err, quota) || errors.Is(err, configuration.ErrReserved) {
		t.Fatal("allocation failure lost its cause or became a reserved hit", err)
	}
	if !strings.Contains(failure.Error(), "filesystem-work limit") || strings.Contains(failure.Error(), "removal") || strings.Contains(failure.Error(), "could not be checked") {
		t.Fatal("allocation failure was presented as an inaccessible location", failure.Error())
	}
}

type refuseProtectionBudget struct {
	*physicalwork.Budget
	failure error
}

func (budget *refuseProtectionBudget) AdmitPathComponents(int) error { return budget.failure }

func (budget *cancelProtectionBudget) AdmitPathComponents(count int) error {
	if budget.cancel != nil {
		budget.cancel()
		budget.cancel = nil
	}
	return budget.Budget.AdmitPathComponents(count)
}

func TestNativeProtectionFollowsUnselectedAuthenticationEndpoints(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		authLink  bool
		future    bool
		unrelated bool
	}{
		{name: "alias to authentication file"},
		{name: "authentication endpoint aliases ordinary file", authLink: true},
		{name: "authentication endpoint aliases future file", authLink: true, future: true},
		{name: "unrelated sibling", authLink: true, unrelated: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			base := t.TempDir()
			manifest := nativeNamespaceRoot(t, base)
			pi, codex := filepath.Join(base, "pi-global"), filepath.Join(base, "codex-global")
			for _, directory := range []string{filepath.Join(base, ".pi"), codex} {
				if err := os.Mkdir(directory, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			selected := filepath.Join(base, ".pi", "extra.json")
			auth := filepath.Join(codex, "auth.json")
			if scenario.authLink {
				targetPath := selected
				if scenario.unrelated {
					targetPath = filepath.Join(base, ".pi", "other.json")
				}
				if err := os.Symlink(targetPath, auth); err != nil {
					t.Skipf("symlinks are unavailable: %v", err)
				}
				if !scenario.future {
					if err := os.WriteFile(selected, []byte(`{"fixture":true}`), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				if err := os.WriteFile(auth, []byte(`{"fixture":true}`), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(auth, selected); err != nil {
					t.Skipf("symlinks are unavailable: %v", err)
				}
			}
			alias := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/extra.json")
			roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: manifest})
			reader := &nativeNamespaceReader{RootedReader: journalTestFilesystem()}
			value, err := Observe(t.Context(), []aggregate.DocumentAddress{alias}, roots, protectionRoots(t, pi, codex), reader, imageBudget(t))
			if scenario.unrelated {
				if err != nil || value.Observation().Validate() != nil {
					t.Fatal("unrelated document became a reserved endpoint", err)
				}
				return
			}
			var refusal *ProtectionError
			if !errors.Is(err, configuration.ErrReserved) || !errors.As(err, &refusal) || refusal.Host() != target.TargetCodex || value.Observation().Validate() == nil || len(reader.capabilities) != 0 || reader.imageReads != 0 {
				t.Fatal("unselected authentication endpoint was not refused before file access", err)
			}
		})
	}
}

func TestNativeProtectionRechecksEndpointLinksBeforeReferentAcquisition(t *testing.T) {
	base := t.TempDir()
	manifest := nativeNamespaceRoot(t, base)
	pi, codex := filepath.Join(base, "pi-global"), filepath.Join(base, "codex-global")
	for _, directory := range []string{filepath.Join(base, ".pi"), codex} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	selected := filepath.Join(base, ".pi", "extra.json")
	if err := os.WriteFile(selected, []byte(`{"fixture":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/extra.json")
	roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: manifest})
	reader := &nativeNamespaceReader{RootedReader: journalTestFilesystem()}
	value, err := Observe(t.Context(), []aggregate.DocumentAddress{alias}, roots, protectionRoots(t, pi, codex), reader, imageBudget(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(selected, filepath.Join(codex, "auth.json")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	capability, err := value.AcquireReferent(t.Context(), imageBudget(t))
	if capability != nil || !errors.Is(err, configuration.ErrReserved) || reader.imageReads != 0 {
		t.Fatal("changed reserved endpoint supplied referent access", err)
	}
	if root, _, err := value.WriterBinding(t.Context(), imageBudget(t)); root != nil || !errors.Is(err, configuration.ErrReserved) {
		t.Fatal("changed reserved endpoint supplied a writer binding", err)
	}
}

func protectionRoots(t *testing.T, pi, codex string) ReservationSelectors {
	t.Helper()
	fixture := fixtureProtection(t)
	for host, path := range map[target.Target]string{target.TargetPi: pi, target.TargetCodex: codex} {
		reference, err := configuration.RootReferenceForScope(host, target.ScopeGlobal)
		if err != nil {
			t.Fatal(err)
		}
		selector, err := NewReservationSelector(reference, path)
		if err != nil {
			t.Fatal(err)
		}
		fixture.selectors[reference] = selector
	}
	return fixture
}

func fixtureProtection(t *testing.T) ReservationSelectors {
	t.Helper()
	base := t.TempDir()
	contracts, err := configuration.NativeProtectionLocations()
	if err != nil {
		t.Fatal(err)
	}
	var selected []ReservationSelector
	for _, contract := range contracts {
		selector, err := NewReservationSelector(contract.Reference(), filepath.Join(base, contract.Reference().Identity()))
		if err != nil {
			t.Fatal(err)
		}
		selected = append(selected, selector)
	}
	result, err := NewReservationSelectors(selected)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
