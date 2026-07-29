//go:build unix

package configrelation

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	observepipackage "github.com/isty2e/daem/internal/assurance/observe/pipackage"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestPiOrderExecutePublishesExactCandidateAndIsRetryIdempotent(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			root := t.TempDir()
			settings := piOrderSettings(root, scope)
			path, err := observepipackage.SettingsPath(settings)
			if err != nil {
				t.Fatal(err)
			}
			writePiOrderSettings(t, path, `{
  "packages": [
    {"source":"npm:b@1","extensions":["b.ts"]},
    "npm:foreign@1",
    "npm:a@1"
  ],
  "retained": true
}`)
			if err := os.Chmod(path, 0o640); err != nil {
				t.Fatal(err)
			}
			observation := mustPiOrderObservation(
				t,
				settings,
				root,
				scope,
				[]piOrderSpec{{id: "a", source: "npm:a@1"}, {id: "b", source: "npm:b@1"}},
			)
			order := bindPiOrder(t, root, observation)
			defer order.Close()

			changed, err := order.Execute(t.Context(), storagecommit.Adapter{})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if !changed {
				t.Fatal("Execute reported no change")
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			candidate, _ := observation.Candidate()
			if string(got) != string(candidate) {
				t.Fatalf("published content = %q, want %q", got, candidate)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o640 {
				t.Fatalf("mode = %o, want 640", info.Mode().Perm())
			}

			changed, err = order.Execute(t.Context(), storagecommit.Adapter{})
			if err != nil {
				t.Fatalf("idempotent retry: %v", err)
			}
			if changed {
				t.Fatal("exact-candidate retry reported a second write")
			}
		})
	}
}

func TestPiOrderExecuteRejectsConcurrentBaselineWithoutWriting(t *testing.T) {
	root := t.TempDir()
	settings := piOrderSettings(root, target.ScopeProject)
	path, err := observepipackage.SettingsPath(settings)
	if err != nil {
		t.Fatal(err)
	}
	writePiOrderSettings(t, path, `{"packages":["npm:b@1","npm:a@1"]}`)
	observation := mustPiOrderObservation(
		t,
		settings,
		root,
		target.ScopeProject,
		[]piOrderSpec{{id: "a", source: "npm:a@1"}, {id: "b", source: "npm:b@1"}},
	)
	order := bindPiOrder(t, root, observation)
	defer order.Close()

	concurrent := []byte(`{"packages":["npm:b@1","npm:concurrent@1","npm:a@1"]}`)
	writePiOrderSettings(t, path, string(concurrent))
	changed, err := order.Execute(t.Context(), storagecommit.Adapter{})
	if err == nil || !strings.Contains(err.Error(), "baseline revision changed") {
		t.Fatalf("Execute error = %v", err)
	}
	if changed {
		t.Fatal("stale baseline reported a write")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(concurrent) {
		t.Fatalf("concurrent content changed to %q", got)
	}
}

func TestPiOrderExecuteDoesNotCreateMissingSettings(t *testing.T) {
	root := t.TempDir()
	settings := piOrderSettings(root, target.ScopeProject)
	path, err := observepipackage.SettingsPath(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	observation := mustPiOrderObservation(
		t,
		settings,
		root,
		target.ScopeProject,
		[]piOrderSpec{{id: "a", source: "npm:a@1"}},
	)
	order := bindPiOrder(t, root, observation)
	defer order.Close()

	changed, err := order.Execute(t.Context(), storagecommit.Adapter{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if changed {
		t.Fatal("missing settings reported a write")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("missing settings stat error = %v", err)
	}
}

func TestPiOrderExecuteSurfacesWriteFailuresAndRecoversVisibleCandidate(t *testing.T) {
	root := t.TempDir()
	settings := piOrderSettings(root, target.ScopeProject)
	path, err := observepipackage.SettingsPath(settings)
	if err != nil {
		t.Fatal(err)
	}
	baseline := `{"packages":["npm:b@1","npm:a@1"]}`
	writePiOrderSettings(t, path, baseline)
	observation := mustPiOrderObservation(
		t,
		settings,
		root,
		target.ScopeProject,
		[]piOrderSpec{{id: "a", source: "npm:a@1"}, {id: "b", source: "npm:b@1"}},
	)

	t.Run("before visibility", func(t *testing.T) {
		order := bindPiOrder(t, root, observation)
		defer order.Close()
		store := rejectPiOrderReplaceStore{RootedStore: storagecommit.Adapter{}}
		changed, err := order.Execute(t.Context(), store)
		if err == nil || !strings.Contains(err.Error(), "injected before visibility") {
			t.Fatalf("Execute error = %v", err)
		}
		if changed {
			t.Fatal("failed replacement reported visible change")
		}
		got, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(got) != baseline {
			t.Fatalf("pre-visibility failure changed content to %q", got)
		}
	})

	t.Run("after visibility", func(t *testing.T) {
		writePiOrderSettings(t, path, baseline)
		order := bindPiOrder(t, root, observation)
		defer order.Close()
		store := visiblePiOrderErrorStore{RootedStore: storagecommit.Adapter{}}
		changed, err := order.Execute(t.Context(), store)
		if err == nil || !strings.Contains(err.Error(), "injected after visibility") {
			t.Fatalf("Execute error = %v", err)
		}
		if changed {
			t.Fatal("write error claimed verified convergence")
		}
		candidate, _ := observation.Candidate()
		got, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(got) != string(candidate) {
			t.Fatalf("visible candidate = %q, want %q", got, candidate)
		}
		changed, err = order.Execute(t.Context(), storagecommit.Adapter{})
		if err != nil || changed {
			t.Fatalf("retry = changed:%t error:%v", changed, err)
		}
	})
}

func TestPiOrderExecuteRejectsPostObservationMismatch(t *testing.T) {
	root := t.TempDir()
	settings := piOrderSettings(root, target.ScopeProject)
	path, err := observepipackage.SettingsPath(settings)
	if err != nil {
		t.Fatal(err)
	}
	writePiOrderSettings(t, path, `{"packages":["npm:b@1","npm:a@1"]}`)
	observation := mustPiOrderObservation(
		t,
		settings,
		root,
		target.ScopeProject,
		[]piOrderSpec{{id: "a", source: "npm:a@1"}, {id: "b", source: "npm:b@1"}},
	)
	order := bindPiOrder(t, root, observation)
	defer order.Close()
	mismatch := []byte(`{"packages":["npm:a@1","npm:foreign@1","npm:b@1"]}`)
	store := mismatchingPiOrderPostStore{
		RootedStore: storagecommit.Adapter{},
		path:        path,
		content:     mismatch,
	}

	changed, err := order.Execute(t.Context(), store)
	if err == nil || !strings.Contains(err.Error(), "post-observation") {
		t.Fatalf("Execute error = %v", err)
	}
	if !changed {
		t.Fatal("post-observation failure lost the visible-write fact")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(mismatch) {
		t.Fatalf("post-observation mismatch = %q, want %q", got, mismatch)
	}
}

func TestPiOrderExecuteRejectsUnsafeConcurrentEntryChanges(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{
			name: "symlink",
			mutate: func(t *testing.T, path string) {
				targetPath := filepath.Join(filepath.Dir(path), "target.json")
				writePiOrderSettings(t, targetPath, `{"packages":["npm:b@1","npm:a@1"]}`)
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(targetPath, path); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			},
		},
		{
			name: "directory",
			mutate: func(t *testing.T, path string) {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "oversized",
			mutate: func(t *testing.T, path string) {
				content := make([]byte, observepipackage.MaximumSettingsBytes+1)
				for index := range content {
					content[index] = ' '
				}
				if err := os.WriteFile(path, content, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "invalid UTF-8",
			mutate: func(t *testing.T, path string) {
				if err := os.WriteFile(path, []byte{0xff}, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			settings := piOrderSettings(root, target.ScopeProject)
			path, err := observepipackage.SettingsPath(settings)
			if err != nil {
				t.Fatal(err)
			}
			writePiOrderSettings(t, path, `{"packages":["npm:b@1","npm:a@1"]}`)
			observation := mustPiOrderObservation(
				t,
				settings,
				root,
				target.ScopeProject,
				[]piOrderSpec{{id: "a", source: "npm:a@1"}, {id: "b", source: "npm:b@1"}},
			)
			order := bindPiOrder(t, root, observation)
			defer order.Close()
			test.mutate(t, path)

			changed, err := order.Execute(t.Context(), storagecommit.Adapter{})
			if err == nil {
				t.Fatal("Execute accepted an unsafe concurrent entry")
			}
			if changed {
				t.Fatal("unsafe concurrent entry reported a write")
			}
		})
	}
}

func TestPiOrderExecuteHonorsPreCanceledContext(t *testing.T) {
	root := t.TempDir()
	settings := piOrderSettings(root, target.ScopeProject)
	path, err := observepipackage.SettingsPath(settings)
	if err != nil {
		t.Fatal(err)
	}
	baseline := `{"packages":["npm:b@1","npm:a@1"]}`
	writePiOrderSettings(t, path, baseline)
	observation := mustPiOrderObservation(
		t,
		settings,
		root,
		target.ScopeProject,
		[]piOrderSpec{{id: "a", source: "npm:a@1"}, {id: "b", source: "npm:b@1"}},
	)
	order := bindPiOrder(t, root, observation)
	defer order.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	changed, err := order.Execute(ctx, storagecommit.Adapter{})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute error = %v", err)
	}
	if changed {
		t.Fatal("canceled execution reported a write")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != baseline {
		t.Fatalf("canceled execution changed content to %q", got)
	}
}

func TestPiOrderExecuteRejectsMissingPostObservation(t *testing.T) {
	root := t.TempDir()
	settings := piOrderSettings(root, target.ScopeProject)
	path, err := observepipackage.SettingsPath(settings)
	if err != nil {
		t.Fatal(err)
	}
	writePiOrderSettings(t, path, `{"packages":["npm:b@1","npm:a@1"]}`)
	observation := mustPiOrderObservation(
		t,
		settings,
		root,
		target.ScopeProject,
		[]piOrderSpec{{id: "a", source: "npm:a@1"}, {id: "b", source: "npm:b@1"}},
	)
	order := bindPiOrder(t, root, observation)
	defer order.Close()
	store := missingPiOrderPostStore{
		RootedStore: storagecommit.Adapter{},
		path:        path,
	}

	changed, err := order.Execute(t.Context(), store)
	if err == nil || !strings.Contains(err.Error(), "post-observation") {
		t.Fatalf("Execute error = %v", err)
	}
	if !changed {
		t.Fatal("missing post-observation lost the visible-write fact")
	}
	if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
		t.Fatalf("post path stat error = %v", statErr)
	}
}

type rejectPiOrderReplaceStore struct {
	mutationfs.RootedStore
}

func (store rejectPiOrderReplaceStore) ReplaceRootedFile(
	_ context.Context,
	capability rootedpath.CommitCapability,
	_ []byte,
	_ fs.FileMode,
	_ mutationfs.EntryIdentity,
) error {
	return errors.Join(
		errors.New("injected before visibility"),
		capability.Close(),
	)
}

type visiblePiOrderErrorStore struct {
	mutationfs.RootedStore
}

func (store visiblePiOrderErrorStore) ReplaceRootedFile(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	content []byte,
	mode fs.FileMode,
	expected mutationfs.EntryIdentity,
) error {
	if err := store.RootedStore.ReplaceRootedFile(
		ctx,
		capability,
		content,
		mode,
		expected,
	); err != nil {
		return err
	}
	return errors.New("injected after visibility")
}

type mismatchingPiOrderPostStore struct {
	mutationfs.RootedStore
	path    string
	content []byte
}

type missingPiOrderPostStore struct {
	mutationfs.RootedStore
	path string
}

func (store missingPiOrderPostStore) ReplaceRootedFile(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	content []byte,
	mode fs.FileMode,
	expected mutationfs.EntryIdentity,
) error {
	if err := store.RootedStore.ReplaceRootedFile(
		ctx,
		capability,
		content,
		mode,
		expected,
	); err != nil {
		return err
	}
	return os.Remove(store.path)
}

func (store mismatchingPiOrderPostStore) ReplaceRootedFile(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	content []byte,
	mode fs.FileMode,
	expected mutationfs.EntryIdentity,
) error {
	if err := store.RootedStore.ReplaceRootedFile(
		ctx,
		capability,
		content,
		mode,
		expected,
	); err != nil {
		return err
	}
	return os.WriteFile(store.path, store.content, mode.Perm())
}

type piOrderSpec struct {
	id     string
	source string
}

func mustPiOrderObservation(
	t *testing.T,
	settings observepipackage.SettingsInput,
	commandRoot string,
	scope target.Scope,
	specs []piOrderSpec,
) observepipackage.OrderObservation {
	t.Helper()
	capability, admitted := profile.Profile(target.TargetPi).ExtensionOrder(
		desiredextension.CarrierPiPackage,
		scope,
	)
	if !admitted {
		t.Fatalf("Pi %s order capability is absent", scope)
	}
	members := make([]hostrelation.RelationOrderMember, 0, len(specs))
	relations := make([]observepipackage.ScopedRelation, 0, len(specs))
	for _, spec := range specs {
		subject, err := topology.NewSubjectID(
			topology.SubjectHostRelation,
			"pi.package-carrier",
			spec.id,
		)
		if err != nil {
			t.Fatal(err)
		}
		subjectKey, err := hostrelation.NewSubjectKey(spec.source)
		if err != nil {
			t.Fatal(err)
		}
		managedKey, err := hostrelation.NewManagedInstanceKey("host-relation:v1:" + spec.id)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := hostrelation.NewExpectedRelation(subjectKey, managedKey)
		if err != nil {
			t.Fatal(err)
		}
		key, err := observerelation.NewCorrelationKey(subject, expected)
		if err != nil {
			t.Fatal(err)
		}
		relation, err := observepipackage.NewScopedRelation(key, scope, commandRoot)
		if err != nil {
			t.Fatal(err)
		}
		identity, err := observepipackage.HostLoadIdentityForInput(
			spec.source,
			commandRoot,
			scope,
		)
		if err != nil {
			t.Fatal(err)
		}
		loadIdentity, err := hostrelation.NewHostLoadIdentity(identity)
		if err != nil {
			t.Fatal(err)
		}
		member, err := hostrelation.NewRelationOrderMember(subject, loadIdentity)
		if err != nil {
			t.Fatal(err)
		}
		relations = append(relations, relation)
		members = append(members, member)
	}
	constraint, err := hostrelation.NewRelationOrderConstraint(
		capability.ClassID(),
		capability.MemberIdentityContract(),
		capability.RuntimeMeaning(),
		members,
	)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := observepipackage.ReadOrder(observepipackage.OrderInput{
		Settings:   settings,
		Constraint: constraint,
		Relations:  relations,
	})
	if err != nil {
		t.Fatalf("ReadOrder: %v", err)
	}
	return observation
}

func piOrderSettings(root string, scope target.Scope) observepipackage.SettingsInput {
	return observepipackage.SettingsInput{
		ConfigRoot:  filepath.Join(root, "agent"),
		WorkDir:     root,
		ProjectRoot: filepath.Join(root, "project"),
		Scope:       scope,
	}
}

func bindPiOrder(
	t *testing.T,
	root string,
	observation observepipackage.OrderObservation,
) *BoundPiOrder {
	t.Helper()
	plan, err := NewPiOrderPlan(observation)
	if err != nil {
		t.Fatalf("NewPiOrderPlan: %v", err)
	}
	if _, err := plan.PhysicalAuthority(); err != nil {
		t.Fatalf("PhysicalAuthority: %v", err)
	}
	selected, err := rootedpath.CaptureRoot(root)
	if err != nil {
		t.Fatalf("CaptureRoot: %v", err)
	}
	t.Cleanup(func() {
		if err := selected.Close(); err != nil {
			t.Errorf("close selected root: %v", err)
		}
	})
	order, err := plan.Bind(selected, root)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	return order
}

func writePiOrderSettings(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
