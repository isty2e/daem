//go:build unix

package configrelation

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	observeopencode "github.com/isty2e/daem/internal/assurance/observe/opencodeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	opencodeconfig "github.com/isty2e/daem/internal/realization/configrelation/opencode"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestOpenCodeOrderExecuteConvergesBothDocumentsAndIsRetryIdempotent(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			root := t.TempDir()
			input, directory := openCodeEffectInventory(t, root, scope)
			serverPath := filepath.Join(directory, "opencode.json")
			tuiPath := filepath.Join(directory, "tui.jsonc")
			writeOpenCodeEffectDocument(t, serverPath, `{"plugin":["beta@1","foreign-server@1","alpha@1"]}`)
			writeOpenCodeEffectDocument(
				t,
				tuiPath,
				"{\n  // retain\n  \"plugin\": [[\"beta@1\", {\"tui\": true}], \"foreign-tui@1\", \"alpha@1\"],\n}\n",
			)
			observation := mustOpenCodeEffectObservation(t, input, directory, scope)
			order := bindOpenCodeOrder(t, root, observation)
			defer order.Close()

			changed, err := order.Execute(t.Context(), storagecommit.Adapter{})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if changed != 2 {
				t.Fatalf("changed documents = %d, want 2", changed)
			}
			for _, document := range observation.Documents() {
				got, err := os.ReadFile(document.Path())
				if err != nil {
					t.Fatal(err)
				}
				candidate, _ := document.Candidate()
				if string(got) != string(candidate) {
					t.Fatalf("%s content = %q, want %q", document.Kind(), got, candidate)
				}
			}

			changed, err = order.Execute(t.Context(), storagecommit.Adapter{})
			if err != nil {
				t.Fatalf("idempotent retry: %v", err)
			}
			if changed != 0 {
				t.Fatalf("idempotent retry changed %d documents", changed)
			}
		})
	}
}

func TestOpenCodeOrderExecuteReportsPartialConvergenceAndFreshRetry(t *testing.T) {
	root := t.TempDir()
	input, directory := openCodeEffectInventory(t, root, target.ScopeProject)
	serverPath := filepath.Join(directory, "opencode.json")
	tuiPath := filepath.Join(directory, "tui.json")
	serverBaseline := `{"plugin":["beta@1","foreign-server@1","alpha@1"]}`
	tuiBaseline := `{"plugin":["beta@1","foreign-tui@1","alpha@1"]}`
	writeOpenCodeEffectDocument(t, serverPath, serverBaseline)
	writeOpenCodeEffectDocument(t, tuiPath, tuiBaseline)
	observation := mustOpenCodeEffectObservation(
		t,
		input,
		directory,
		target.ScopeProject,
	)
	order := bindOpenCodeOrder(t, root, observation)
	store := &failNthOpenCodeReplaceStore{
		RootedStore: storagecommit.Adapter{},
		failAt:      2,
	}
	changed, err := order.Execute(t.Context(), store)
	if err == nil || !strings.Contains(err.Error(), "injected replacement failure") {
		t.Fatalf("Execute error = %v", err)
	}
	if changed != 1 {
		t.Fatalf("partial changed documents = %d, want 1", changed)
	}
	if closeErr := order.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	serverCandidate, _ := observation.Documents()[0].Candidate()
	gotServer, err := os.ReadFile(serverPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotServer) != string(serverCandidate) {
		t.Fatalf("server partial result = %q, want candidate %q", gotServer, serverCandidate)
	}
	gotTUI, err := os.ReadFile(tuiPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotTUI) != tuiBaseline {
		t.Fatalf("TUI changed after second-write failure: %q", gotTUI)
	}

	fresh := mustOpenCodeEffectObservation(t, input, directory, target.ScopeProject)
	retry := bindOpenCodeOrder(t, root, fresh)
	defer retry.Close()
	changed, err = retry.Execute(t.Context(), storagecommit.Adapter{})
	if err != nil {
		t.Fatalf("fresh retry: %v", err)
	}
	if changed != 1 {
		t.Fatalf("fresh retry changed %d documents, want 1", changed)
	}
}

func TestOpenCodeOrderExecuteStopsBeforeLaterDocumentOnFirstWriteFailure(t *testing.T) {
	root := t.TempDir()
	input, directory := openCodeEffectInventory(t, root, target.ScopeProject)
	serverPath := filepath.Join(directory, "opencode.json")
	tuiPath := filepath.Join(directory, "tui.json")
	serverBaseline := `{"plugin":["beta@1","alpha@1"]}`
	tuiBaseline := `{"plugin":["beta@1","alpha@1"]}`
	writeOpenCodeEffectDocument(t, serverPath, serverBaseline)
	writeOpenCodeEffectDocument(t, tuiPath, tuiBaseline)
	observation := mustOpenCodeEffectObservation(
		t,
		input,
		directory,
		target.ScopeProject,
	)
	order := bindOpenCodeOrder(t, root, observation)
	defer order.Close()
	store := &failNthOpenCodeReplaceStore{
		RootedStore: storagecommit.Adapter{},
		failAt:      1,
	}

	changed, err := order.Execute(t.Context(), store)
	if err == nil || !strings.Contains(err.Error(), "injected replacement failure") {
		t.Fatalf("Execute error = %v", err)
	}
	if changed != 0 || store.calls != 1 {
		t.Fatalf("first failure = changed:%d calls:%d", changed, store.calls)
	}
	for path, want := range map[string]string{
		serverPath: serverBaseline,
		tuiPath:    tuiBaseline,
	} {
		got, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(got) != want {
			t.Fatalf("%s changed to %q, want %q", path, got, want)
		}
	}
}

func TestOpenCodeOrderExecuteRecoversFromUnknownVisibilityOnRetry(t *testing.T) {
	root := t.TempDir()
	input, directory := openCodeEffectInventory(t, root, target.ScopeProject)
	serverPath := filepath.Join(directory, "opencode.json")
	tuiPath := filepath.Join(directory, "tui.json")
	writeOpenCodeEffectDocument(t, serverPath, `{"plugin":["beta@1","alpha@1"]}`)
	tuiBaseline := `{"plugin":["beta@1","alpha@1"]}`
	writeOpenCodeEffectDocument(t, tuiPath, tuiBaseline)
	observation := mustOpenCodeEffectObservation(
		t,
		input,
		directory,
		target.ScopeProject,
	)
	order := bindOpenCodeOrder(t, root, observation)
	defer order.Close()
	store := &visibleErrorNthOpenCodeReplaceStore{
		RootedStore: storagecommit.Adapter{},
		failAt:      1,
	}

	changed, err := order.Execute(t.Context(), store)
	if err == nil || !strings.Contains(err.Error(), "injected error after visibility") {
		t.Fatalf("Execute error = %v", err)
	}
	if changed != 0 {
		t.Fatalf("unknown visibility claimed %d verified changes", changed)
	}
	serverCandidate, _ := observation.Documents()[0].Candidate()
	gotServer, readErr := os.ReadFile(serverPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(gotServer) != string(serverCandidate) {
		t.Fatalf("visible server candidate = %q, want %q", gotServer, serverCandidate)
	}
	gotTUI, readErr := os.ReadFile(tuiPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(gotTUI) != tuiBaseline {
		t.Fatalf("TUI changed after server write error: %q", gotTUI)
	}

	changed, err = order.Execute(t.Context(), storagecommit.Adapter{})
	if err != nil {
		t.Fatalf("same-observation retry: %v", err)
	}
	if changed != 1 {
		t.Fatalf("same-observation retry changed %d documents, want 1", changed)
	}
}

func TestOpenCodeOrderExecuteRejectsConcurrentBaselineAndDoesNotCreateMissingFiles(t *testing.T) {
	t.Run("concurrent baseline", func(t *testing.T) {
		root := t.TempDir()
		input, directory := openCodeEffectInventory(t, root, target.ScopeProject)
		serverPath := filepath.Join(directory, "opencode.json")
		tuiPath := filepath.Join(directory, "tui.json")
		writeOpenCodeEffectDocument(t, serverPath, `{"plugin":["beta@1","alpha@1"]}`)
		writeOpenCodeEffectDocument(t, tuiPath, `{"plugin":["alpha@1","beta@1"]}`)
		observation := mustOpenCodeEffectObservation(
			t,
			input,
			directory,
			target.ScopeProject,
		)
		order := bindOpenCodeOrder(t, root, observation)
		defer order.Close()

		concurrent := `{"plugin":["beta@1","concurrent@1","alpha@1"]}`
		writeOpenCodeEffectDocument(t, serverPath, concurrent)
		changed, err := order.Execute(t.Context(), storagecommit.Adapter{})
		if err == nil || !strings.Contains(err.Error(), "baseline revision changed") {
			t.Fatalf("Execute error = %v", err)
		}
		if changed != 0 {
			t.Fatalf("stale baseline changed %d documents", changed)
		}
		got, readErr := os.ReadFile(serverPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(got) != concurrent {
			t.Fatalf("concurrent server content changed to %q", got)
		}
	})

	t.Run("missing files", func(t *testing.T) {
		root := t.TempDir()
		input, directory := openCodeEffectInventory(t, root, target.ScopeProject)
		observation := mustOpenCodeEffectObservation(
			t,
			input,
			directory,
			target.ScopeProject,
		)
		order := bindOpenCodeOrder(t, root, observation)
		defer order.Close()
		changed, err := order.Execute(t.Context(), storagecommit.Adapter{})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if changed != 0 {
			t.Fatalf("missing input changed %d documents", changed)
		}
		for _, document := range observation.Documents() {
			if _, err := os.Lstat(document.Path()); !os.IsNotExist(err) {
				t.Fatalf("%s missing path stat error = %v", document.Kind(), err)
			}
		}
	})
}

func TestOpenCodeOrderExecuteReportsVisiblePostMismatch(t *testing.T) {
	for _, mismatchAt := range []int{1, 2} {
		t.Run(fmt.Sprintf("document-%d", mismatchAt), func(t *testing.T) {
			root := t.TempDir()
			input, directory := openCodeEffectInventory(t, root, target.ScopeProject)
			serverPath := filepath.Join(directory, "opencode.json")
			tuiPath := filepath.Join(directory, "tui.json")
			writeOpenCodeEffectDocument(t, serverPath, `{"plugin":["beta@1","alpha@1"]}`)
			writeOpenCodeEffectDocument(t, tuiPath, `{"plugin":["beta@1","alpha@1"]}`)
			observation := mustOpenCodeEffectObservation(
				t,
				input,
				directory,
				target.ScopeProject,
			)
			order := bindOpenCodeOrder(t, root, observation)
			defer order.Close()
			mismatchPath := serverPath
			if mismatchAt == 2 {
				mismatchPath = tuiPath
			}
			store := &mismatchNthOpenCodePostStore{
				RootedStore: storagecommit.Adapter{},
				path:        mismatchPath,
				mismatchAt:  mismatchAt,
				content:     []byte(`{"plugin":["beta@1","foreign@1","alpha@1"]}`),
			}

			changed, err := order.Execute(t.Context(), store)
			if err == nil || !strings.Contains(err.Error(), "post-observation") {
				t.Fatalf("Execute error = %v", err)
			}
			if changed != mismatchAt {
				t.Fatalf("visible writes = %d, want %d", changed, mismatchAt)
			}
			if mismatchAt == 1 {
				gotTUI, readErr := os.ReadFile(tuiPath)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if string(gotTUI) != `{"plugin":["beta@1","alpha@1"]}` {
					t.Fatalf("TUI changed after server post mismatch: %q", gotTUI)
				}
			} else {
				serverCandidate, _ := observation.Documents()[0].Candidate()
				gotServer, readErr := os.ReadFile(serverPath)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if string(gotServer) != string(serverCandidate) {
					t.Fatalf("server convergence was lost: %q", gotServer)
				}
			}
			gotMismatch, readErr := os.ReadFile(mismatchPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(gotMismatch) != string(store.content) {
				t.Fatalf("post mismatch = %q, want %q", gotMismatch, store.content)
			}
		})
	}
}

func TestOpenCodeOrderExecuteHonorsPreCanceledContext(t *testing.T) {
	root := t.TempDir()
	input, directory := openCodeEffectInventory(t, root, target.ScopeProject)
	serverPath := filepath.Join(directory, "opencode.json")
	tuiPath := filepath.Join(directory, "tui.json")
	serverBaseline := `{"plugin":["beta@1","alpha@1"]}`
	tuiBaseline := `{"plugin":["beta@1","alpha@1"]}`
	writeOpenCodeEffectDocument(t, serverPath, serverBaseline)
	writeOpenCodeEffectDocument(t, tuiPath, tuiBaseline)
	observation := mustOpenCodeEffectObservation(
		t,
		input,
		directory,
		target.ScopeProject,
	)
	order := bindOpenCodeOrder(t, root, observation)
	defer order.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	changed, err := order.Execute(ctx, storagecommit.Adapter{})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute error = %v", err)
	}
	if changed != 0 {
		t.Fatalf("canceled execution changed %d documents", changed)
	}
}

type failNthOpenCodeReplaceStore struct {
	mutationfs.RootedStore
	calls  int
	failAt int
}

func (store *failNthOpenCodeReplaceStore) ReplaceRootedFile(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	content []byte,
	mode fs.FileMode,
	expected mutationfs.EntryIdentity,
) error {
	store.calls++
	if store.calls == store.failAt {
		return errors.Join(
			errors.New("injected replacement failure"),
			capability.Close(),
		)
	}
	return store.RootedStore.ReplaceRootedFile(ctx, capability, content, mode, expected)
}

type mismatchNthOpenCodePostStore struct {
	mutationfs.RootedStore
	calls      int
	mismatchAt int
	path       string
	content    []byte
}

type visibleErrorNthOpenCodeReplaceStore struct {
	mutationfs.RootedStore
	calls  int
	failAt int
}

func (store *visibleErrorNthOpenCodeReplaceStore) ReplaceRootedFile(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	content []byte,
	mode fs.FileMode,
	expected mutationfs.EntryIdentity,
) error {
	store.calls++
	if err := store.RootedStore.ReplaceRootedFile(
		ctx,
		capability,
		content,
		mode,
		expected,
	); err != nil {
		return err
	}
	if store.calls == store.failAt {
		return errors.New("injected error after visibility")
	}
	return nil
}

func (store *mismatchNthOpenCodePostStore) ReplaceRootedFile(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	content []byte,
	mode fs.FileMode,
	expected mutationfs.EntryIdentity,
) error {
	store.calls++
	if err := store.RootedStore.ReplaceRootedFile(
		ctx,
		capability,
		content,
		mode,
		expected,
	); err != nil {
		return err
	}
	if store.calls == store.mismatchAt {
		return os.WriteFile(store.path, store.content, mode.Perm())
	}
	return nil
}

func openCodeEffectInventory(
	t *testing.T,
	root string,
	scope target.Scope,
) (observeopencode.InventoryInput, string) {
	t.Helper()
	input := observeopencode.InventoryInput{
		ManifestRoot: filepath.Join(root, "project"),
		ConfigRoot:   filepath.Join(root, "global"),
		Scope:        scope,
	}
	directory, err := opencodeconfig.ConfigDirectory(
		input.ManifestRoot,
		input.ConfigRoot,
		scope,
	)
	if err != nil {
		t.Fatalf("ConfigDirectory: %v", err)
	}
	return input, directory
}

func mustOpenCodeEffectObservation(
	t *testing.T,
	input observeopencode.InventoryInput,
	directory string,
	scope target.Scope,
) observeopencode.OrderObservation {
	t.Helper()
	capability, admitted := profile.Profile(target.TargetOpenCode).ExtensionOrder(
		desiredextension.CarrierOpenCodePlugin,
		scope,
	)
	if !admitted {
		t.Fatalf("OpenCode %s order capability is absent", scope)
	}
	specs := []struct {
		id     string
		source string
	}{
		{id: "alpha", source: "alpha@1"},
		{id: "beta", source: "beta@1"},
	}
	members := make([]hostrelation.RelationOrderMember, 0, len(specs))
	relations := make([]observeopencode.ScopedRelation, 0, len(specs))
	for _, spec := range specs {
		subject, err := topology.NewSubjectID(
			topology.SubjectHostRelation,
			"opencode.plugin-carrier",
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
		relation, err := observeopencode.NewScopedRelation(key, scope)
		if err != nil {
			t.Fatal(err)
		}
		identity, err := opencodeconfig.HostLoadIdentity(
			spec.source,
			filepath.Join(directory, "opencode.json"),
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
	observation, err := observeopencode.ReadOrder(observeopencode.OrderInput{
		Inventory:  input,
		Constraint: constraint,
		Relations:  relations,
	})
	if err != nil {
		t.Fatalf("ReadOrder: %v", err)
	}
	return observation
}

func bindOpenCodeOrder(
	t *testing.T,
	root string,
	observation observeopencode.OrderObservation,
) *BoundOpenCodeOrder {
	t.Helper()
	plan, err := NewOpenCodeOrderPlan(observation)
	if err != nil {
		t.Fatalf("NewOpenCodeOrderPlan: %v", err)
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

func writeOpenCodeEffectDocument(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
