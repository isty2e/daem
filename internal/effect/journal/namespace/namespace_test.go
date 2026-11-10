package namespace

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeNamespaceFollowsAliasesOnlyIntoExplicitTrustedRoots(t *testing.T) {
	root := t.TempDir()
	manifest := nativeNamespaceRoot(t, filepath.Join(root, "project"))
	pi := nativeNamespaceRoot(t, filepath.Join(root, "pi"))
	if err := os.Mkdir(filepath.Join(manifest.SelectedPath, ".pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	referent := filepath.Join(pi.SelectedPath, "shared.json")
	before := []byte("{\"model\":true}\n")
	if err := os.WriteFile(referent, before, 0o600); err != nil {
		t.Fatal(err)
	}
	aliasPath := filepath.Join(manifest.SelectedPath, ".pi", "alias.json")
	if err := os.Symlink(referent, aliasPath); err != nil {
		t.Fatal(err)
	}
	aliasInfo, err := os.Lstat(aliasPath)
	if err != nil {
		t.Fatal(err)
	}
	aliases := []aggregate.DocumentAddress{
		nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/alias.json"),
		nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/shared.json"),
	}
	roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: manifest, output.RootPiAgent: pi})
	reader := &nativeNamespaceReader{RootedReader: journalTestFilesystem()}
	namespace, err := Observe(t.Context(), aliases, roots, fixtureProtection(t), reader, imageBudget(t))
	if err != nil || namespace.Observation().Validate() != nil {
		t.Fatalf("actual alias qualification: %v", err)
	}
	for _, alias := range namespace.Aliases() {
		binding, known := alias.Binding()
		if !known || binding.Validate() != nil || binding.Location() != alias.Location() || binding.RootReference() != alias.Root().RootReference() {
			t.Fatal("forward observation lost its selected complete binding")
		}
		request, err := alias.PhysicalAuthorityRequest()
		if err != nil || request.Target != string(alias.Address().Target()) || request.Scope != string(alias.Address().Scope()) {
			t.Fatal("qualified alias lost its authority request", request, err)
		}
		wantPath := referent
		if alias.Address().Scope() == target.ScopeProject {
			wantPath = aliasPath
		}
		if request.Path != wantPath {
			t.Fatal("alias authority lost its selected entry route", request.Path, wantPath)
		}
		authority, err := mutation.NewPhysicalAuthoritySet(request)
		if err != nil {
			t.Fatal(err)
		}
		observations, err := authority.Namespaces()
		if err != nil {
			t.Fatal(err)
		}
		entry, err := mutation.ObserveDirectoryEntryAuthority(wantPath)
		if err != nil {
			t.Fatal(err)
		}
		entryCovered, referentCovered := false, false
		for _, observed := range observations {
			entryCovered = entryCovered || observed.Equal(entry)
			referentCovered = referentCovered || observed.Equal(namespace.Observation())
		}
		if !entryCovered || !referentCovered {
			t.Fatal("native authority did not cover both its alias entry and writer referent")
		}
	}
	piAuthority, err := pi.Root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	returned := namespace.Addresses()
	returned[0], aliases[0] = aggregate.DocumentAddress{}, aggregate.DocumentAddress{}
	capability, err := namespace.AcquireReferent(t.Context(), imageBudget(t))
	if err != nil {
		t.Fatal(err)
	}
	if !capability.Destination().Root().Equal(piAuthority) {
		_ = capability.Close()
		t.Fatal("referent writer was not selected from the explicitly supplied containing root")
	}
	content, _, _, readErr := journalTestFilesystem().ReadRootedRegularFileUpTo(t.Context(), capability, 4096)
	closeErr := capability.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(content, before) {
		t.Fatalf("rooted referent read: %v, %v", readErr, closeErr)
	}
	for _, observed := range reader.capabilities {
		if observed.Validate() == nil {
			t.Fatal("namespace observation leaked a usable validation capability")
		}
	}
	afterInfo, err := os.Lstat(aliasPath)
	if err != nil || !os.SameFile(aliasInfo, afterInfo) || afterInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("namespace observation replaced the logical symlink: %v", err)
	}
	after, err := os.ReadFile(referent)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("namespace observation changed host bytes: %v", err)
	}
	if _, err := pi.Root.Authority(); err != nil {
		t.Fatalf("borrowed root was closed: %v", err)
	}
}

func TestNativeNamespaceRefusesEscapesWithoutDiscoveringWiderAuthority(t *testing.T) {
	manifest := nativeNamespaceRoot(t, filepath.Join(t.TempDir(), "project"))
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(manifest.SelectedPath, ".pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(manifest.SelectedPath, ".pi", "alias.json")); err != nil {
		t.Fatal(err)
	}
	reader := &nativeNamespaceReader{RootedReader: journalTestFilesystem()}
	result, err := Observe(t.Context(), []aggregate.DocumentAddress{nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/alias.json")}, fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: manifest}), fixtureProtection(t), reader, imageBudget(t))
	if err == nil || result.observation.Validate() == nil || result.writerRoot != nil || len(reader.capabilities) != 0 {
		t.Fatal("escaped referent discovered or returned write authority")
	}
	content, err := os.ReadFile(outside)
	if err != nil || string(content) != "outside" {
		t.Fatalf("escape refusal changed external bytes: %v", err)
	}
}

func TestNativeNamespaceRevalidatesSelectedRootAndFinalAlias(t *testing.T) {
	for _, change := range []string{"root selector", "final alias", "closed root"} {
		t.Run(change, func(t *testing.T) {
			root := t.TempDir()
			physical := nativeNamespaceRoot(t, filepath.Join(root, "physical"))
			other := nativeNamespaceRoot(t, filepath.Join(root, "other"))
			for _, selection := range []RootSelection{physical, other} {
				if err := os.WriteFile(filepath.Join(selection.SelectedPath, "custom.json"), []byte(selection.SelectedPath), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			selector := filepath.Join(root, "selected")
			if err := os.Symlink(physical.SelectedPath, selector); err != nil {
				t.Fatal(err)
			}
			selectedRoot, err := rootedpath.CaptureRoot(selector)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = selectedRoot.Close() })
			roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootPiAgent: {SelectedPath: selector, Root: selectedRoot}})
			namespace, err := Observe(t.Context(), []aggregate.DocumentAddress{nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/custom.json")}, roots, fixtureProtection(t), journalTestFilesystem(), imageBudget(t))
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "root selector":
				if err := os.Remove(selector); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(other.SelectedPath, selector); err != nil {
					t.Fatal(err)
				}
			case "final alias":
				path := filepath.Join(physical.SelectedPath, "custom.json")
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(other.SelectedPath, "custom.json"), path); err != nil {
					t.Fatal(err)
				}
			case "closed root":
				if err := selectedRoot.Close(); err != nil {
					t.Fatal(err)
				}
			}
			capability, err := namespace.AcquireReferent(t.Context(), imageBudget(t))
			if err == nil || capability != nil {
				t.Fatal("changed root/alias retained an acquisitional namespace proof")
			}
		})
	}
}

func TestNativeNamespaceRejectsStaleRoleGrantEvenWithAnotherTrustedWriter(t *testing.T) {
	root := t.TempDir()
	previous := nativeNamespaceRoot(t, filepath.Join(root, "previous"))
	current := nativeNamespaceRoot(t, filepath.Join(root, "current"))
	if err := os.WriteFile(filepath.Join(current.SelectedPath, "custom.json"), []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}
	selector := filepath.Join(root, "selected")
	if err := os.Symlink(previous.SelectedPath, selector); err != nil {
		t.Fatal(err)
	}
	roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootPiAgent: {SelectedPath: selector, Root: previous.Root}, output.RootCodexHome: current})
	if err := os.Remove(selector); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(current.SelectedPath, selector); err != nil {
		t.Fatal(err)
	}
	result, err := Observe(t.Context(), []aggregate.DocumentAddress{nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/custom.json")}, roots, fixtureProtection(t), journalTestFilesystem(), imageBudget(t))
	if err == nil || result.writerRoot != nil || result.observation.Validate() == nil {
		t.Fatal("a different trusted writer disguised a stale logical root grant")
	}
}

func TestNativeNamespaceAbsentSuffixObservationHasNoCreationEffects(t *testing.T) {
	pi := nativeNamespaceRoot(t, filepath.Join(t.TempDir(), "pi"))
	alias := nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/new/nested.json")
	namespace, err := Observe(t.Context(), []aggregate.DocumentAddress{alias}, fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootPiAgent: pi}), fixtureProtection(t), journalTestFilesystem(), imageBudget(t))
	if err != nil || namespace.observation.Validate() != nil {
		t.Fatalf("missing ordinary suffix: %v", err)
	}
	capability, err := namespace.AcquireReferent(t.Context(), imageBudget(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := capability.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(pi.SelectedPath, "new")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("passive namespace observation created a parent: %v", err)
	}
}

func TestNativeNamespaceRefusesDisagreementKindsMissingRootsAndRoleMismatch(t *testing.T) {
	for _, change := range []string{"different files", "directory", "dangling", "missing role", "wrong role", "containing project only", "coincident project only", "different locator"} {
		t.Run(change, func(t *testing.T) {
			pi := nativeNamespaceRoot(t, filepath.Join(t.TempDir(), "pi"))
			for _, name := range []string{"a.json", "b.json"} {
				if err := os.WriteFile(filepath.Join(pi.SelectedPath, name), []byte(name), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			aliases := []aggregate.DocumentAddress{nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/a.json")}
			roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootPiAgent: pi})
			switch change {
			case "different files":
				aliases = append(aliases, nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/b.json"))
			case "directory":
				if err := os.Mkdir(filepath.Join(pi.SelectedPath, "directory.json"), 0o700); err != nil {
					t.Fatal(err)
				}
				aliases[0] = nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/directory.json")
			case "dangling":
				if err := os.Symlink("missing", filepath.Join(pi.SelectedPath, "dangling.json")); err != nil {
					t.Fatal(err)
				}
				aliases[0] = nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/dangling.json")
			case "missing role":
				roots = Roots{}
			case "wrong role":
				aliases[0] = nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@codex-home/a.json")
				roots = fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootCodexHome: pi})
			case "containing project only":
				project := nativeNamespaceRoot(t, filepath.Dir(pi.SelectedPath))
				roots = fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: project})
			case "coincident project only":
				roots = fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: pi})
			case "different locator":
				reference, err := output.NewRootReference("pi-agent", "fixture-different-locator-v1")
				if err != nil {
					t.Fatal(err)
				}
				roots, err = (Roots{}).With(reference, pi)
				if err != nil {
					t.Fatal(err)
				}
			}
			result, err := Observe(t.Context(), aliases, roots, fixtureProtection(t), journalTestFilesystem(), imageBudget(t))
			if err == nil || result.writerRoot != nil || result.observation.Validate() == nil {
				t.Fatal("unqualified or non-file namespace returned a result")
			}
		})
	}
}

func TestNativeNamespaceKeepsCoincidentLogicalBindings(t *testing.T) {
	selected := nativeNamespaceRoot(t, t.TempDir())
	if err := os.Mkdir(filepath.Join(selected.SelectedPath, ".pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(selected.SelectedPath, ".pi", "a.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: selected, output.RootPiAgent: selected})
	aliases := []aggregate.DocumentAddress{
		nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/a.json"),
		nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/.pi/a.json"),
	}
	namespace, err := Observe(t.Context(), aliases, roots, fixtureProtection(t), journalTestFilesystem(), imageBudget(t))
	if err != nil {
		t.Fatal(err)
	}
	qualified, err := namespace.QualifyRoots(t.Context(), imageBudget(t))
	if err != nil || len(namespace.Aliases()) != 2 || len(qualified.grants) != 2 {
		t.Fatalf("coincident bindings collapsed: %v", err)
	}
}

func TestNativeNamespaceBudgetAndMidObservationCancellationReturnNoCapability(t *testing.T) {
	pi := nativeNamespaceRoot(t, filepath.Join(t.TempDir(), "pi"))
	if err := os.WriteFile(filepath.Join(pi.SelectedPath, "custom.json"), []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	aliases := []aggregate.DocumentAddress{nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/custom.json")}
	reader := &nativeNamespaceReader{RootedReader: journalTestFilesystem()}
	limitErr := errors.New("test traversal exhausted")
	if result, err := Observe(t.Context(), aliases, fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootPiAgent: pi}), fixtureProtection(t), reader, &nativeNamespaceBudget{err: limitErr}); !errors.Is(err, limitErr) || result.writerRoot != nil || len(reader.capabilities) != 0 {
		t.Fatalf("exhausted traversal admitted observation: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reader.afterIdentity = cancel
	if result, err := Observe(ctx, aliases, fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootPiAgent: pi}), fixtureProtection(t), reader, imageBudget(t)); !errors.Is(err, context.Canceled) || result.writerRoot != nil {
		t.Fatalf("canceled observation returned a namespace: %v", err)
	}
	for _, capability := range reader.capabilities {
		if capability.Validate() == nil {
			t.Fatal("cancellation leaked a validation capability")
		}
	}
}

func nativeNamespaceRoot(t *testing.T, path string) RootSelection {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := rootedpath.CaptureRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return RootSelection{SelectedPath: path, Root: root}
}

func nativeNamespaceAddress(t *testing.T, selected target.Target, scope target.Scope, text string) aggregate.DocumentAddress {
	t.Helper()
	destination, err := output.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	address, err := aggregate.NewDocumentAddress(selected, scope, destination)
	if err != nil {
		t.Fatal(err)
	}
	return address
}

type nativeNamespaceReader struct {
	mutationfs.RootedReader
	capabilities  []rootedpath.CommitCapability
	afterIdentity func()
	imageReads    int
}

func (reader *nativeNamespaceReader) CaptureRootedEntryIdentity(ctx context.Context, capability rootedpath.CommitCapability) (mutationfs.EntryIdentity, error) {
	reader.capabilities = append(reader.capabilities, capability)
	identity, err := reader.RootedReader.CaptureRootedEntryIdentity(ctx, capability)
	if reader.afterIdentity != nil {
		reader.afterIdentity()
	}
	return identity, err
}

func (reader *nativeNamespaceReader) ReadRootedRegularFileUpTo(ctx context.Context, capability rootedpath.CommitCapability, maximumBytes int64) ([]byte, fs.FileMode, mutationfs.EntryIdentity, error) {
	reader.imageReads++
	return reader.RootedReader.ReadRootedRegularFileUpTo(ctx, capability, maximumBytes)
}

type nativeNamespaceBudget struct{ err error }

func (budget *nativeNamespaceBudget) AdmitPathComponents(int) error { return budget.err }
