package build

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/desired/instructions"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	sourceresolution "github.com/isty2e/daem/internal/supply/source/resolution"
	"github.com/isty2e/daem/internal/target"
)

func nativeDocumentLimits() configdocument.Limits {
	return configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}
}

func nativeDocumentConfig(t *testing.T, name string, spec source.Source, selected target.Target, scope target.Scope, path string, ownership hostconfig.Ownership, syntax hostconfig.Syntax) hostconfig.Config {
	t.Helper()
	config, err := hostconfig.New(hostconfig.Spec{
		Name: name, Source: spec, Target: selected, Scope: scope, Path: path, Ownership: ownership, Syntax: syntax,
	})
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func nativeDocumentResolver(t *testing.T, directory string) acquisition.Resolver {
	t.Helper()
	paths, err := daempaths.Resolve(filepath.Join(directory, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := sourceresolution.NewResolver(paths)
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

func nativeLocalSource(t *testing.T, directory, name string, payload []byte, mode os.FileMode) source.Source {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, payload, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	spec, err := source.NewLocalSource(path, source.LocalSourceModeVendor)
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func TestNativeDocumentProducerUsesExactSourcesAndNativeWholeIdentity(t *testing.T) {
	for _, fixture := range []struct {
		name    string
		target  target.Target
		scope   target.Scope
		path    string
		syntax  hostconfig.Syntax
		payload []byte
		mode    os.FileMode
	}{
		{"pi-opaque", target.TargetPi, target.ScopeProject, "prompts/review.md", hostconfig.SyntaxDefault, []byte("# Review\r\n\x00literal \xff bytes\n"), 0o755},
		{"codex-empty", target.TargetCodex, target.ScopeGlobal, "prompts/review.md", hostconfig.SyntaxDefault, nil, 0o644},
		{"known-json", target.TargetPi, target.ScopeGlobal, "themes/custom.json", hostconfig.SyntaxDefault, []byte("[ 1, true, null ]\n"), 0o600},
		{"known-line-comments", target.TargetPi, target.ScopeGlobal, "models.json", hostconfig.SyntaxDefault, []byte("// retained\n{\"models\": []}\n"), 0o644},
		{"explicit-toml", target.TargetCodex, target.ScopeProject, "custom.conf", hostconfig.SyntaxTOML, []byte("# keep\nanswer = 42\n"), 0o644},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			directory := t.TempDir()
			spec := nativeLocalSource(t, directory, "source", fixture.payload, fixture.mode)
			config := nativeDocumentConfig(t, "document", spec, fixture.target, fixture.scope, fixture.path, hostconfig.OwnershipDocument, fixture.syntax)
			environment := lockEnvironment(t, desired.Spec{HostConfigs: []hostconfig.Config{config}})
			var events []Event
			file, err := BuildNativeDocumentsWithOptions(context.Background(), environment, nativeDocumentResolver(t, directory), Options{Events: func(event Event) { events = append(events, event) }}, nativeDocumentLimits())
			if err != nil {
				t.Fatal(err)
			}
			if file.Version != lock.NativeVersion || file.Locked.Len() != 2 || lock.ValidateNative(file) != nil {
				t.Fatalf("invalid complete native lock: %#v", file)
			}
			resource, ok := file.Locked.ExactSupplySubject(config.ID())
			if !ok {
				t.Fatal("source resource was omitted")
			}
			placement, ok := resource.HostConfigPlacement()
			if !ok || placement.NativePath() != fixture.path || placement.RequestedSyntax() != fixture.syntax || placement.Target() != fixture.target || placement.Scope() != fixture.scope || placement.Ownership() != hostconfig.OwnershipDocument {
				t.Fatalf("declaration intent lost: %#v", placement)
			}
			whole, err := aggregate.HostConfigDocumentSubject(config.ID(), fixture.target, fixture.scope)
			if err != nil {
				t.Fatal(err)
			}
			projection, ok := file.Locked.Subject(whole)
			if !ok {
				t.Fatal("real producer did not use native whole-file identity")
			}
			realized, _ := projection.Realization()
			path, ok := realized.ManagedPathProjection()
			mode, exact := path.ExactPermissionMode()
			if !ok || path.Destination() != placement.Destination() || path.PermissionPolicy() != realization.PathPermissionsExact || !exact || mode.FileMode() != 0o600 {
				t.Fatalf("private whole-file projection lost: %#v", path)
			}
			identity, _ := resource.ExactSupply()
			view, err := access.OpenView(filepath.Join(directory, "source"))
			if err != nil {
				t.Fatal(err)
			}
			hash, err := view.Hash(context.Background())
			if err != nil || identity.ContentHash() != hash {
				t.Fatalf("source identity mismatch: %v", err)
			}
			observedSource, err := os.Stat(filepath.Join(directory, "source"))
			if err != nil {
				t.Fatal(err)
			}
			materialization, err := artifact.NewFileMaterialization(identity, fixture.payload, observedSource.Mode().Perm()&0o111 != 0, false)
			if err != nil || resource.ValidateFileMaterialization(materialization) != nil {
				t.Fatalf("source bytes were transformed or executable output admitted: %v", err)
			}
			if len(events) == 0 || events[len(events)-1].Kind != EventSnapshotValidated || events[len(events)-1].Count != 2 {
				t.Fatalf("missing complete success event: %#v", events)
			}
			assertNativeDocumentRoundtrip(t, directory, file)
		})
	}
}

func assertNativeDocumentRoundtrip(t *testing.T, directory string, file lock.File) {
	t.Helper()
	encoded, err := lockfile.MarshalNative(file)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "native.lock.toml")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	decoded, err := lockfile.LoadNative(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Version != file.Version || lock.BuildDelta(file, decoded).HasChanges() {
		t.Fatal("native lock roundtrip changed durable facts")
	}
	second, err := lockfile.MarshalNative(decoded)
	if err != nil || !bytes.Equal(encoded, second) {
		t.Fatalf("native encoding not canonical: %v", err)
	}
	if _, err := lockfile.Marshal(file); err == nil {
		t.Fatal("default writer admitted native schema")
	}
	_, err = lockfile.Load(context.Background(), path)
	var generation lockfile.UnsupportedVersionError
	if !errors.As(err, &generation) || generation.Found != lock.NativeVersion || generation.Supported != lock.CurrentVersion {
		t.Fatalf("default reader widened: %v", err)
	}
	for _, change := range []struct{ before, after string }{
		{`path = "`, `path = "other/`},
		{"exact_permission_mode = 384", "exact_permission_mode = 420"},
		{"[locked.subject.host_config_placement]", "[locked.subject.host_config_placement]\nunknown = true"},
	} {
		corrupted := strings.Replace(string(encoded), change.before, change.after, 1)
		if corrupted == string(encoded) {
			t.Fatalf("fault control did not change encoded artifact: %q", change.before)
		}
		if err := os.WriteFile(path, []byte(corrupted), 0o600); err != nil {
			t.Fatal(err)
		}
		if result, err := lockfile.LoadNative(context.Background(), path); err == nil || result.Version != 0 {
			t.Fatalf("forged native artifact yielded usable lock: %v", err)
		}
	}
}

func TestNativeDocumentBuildRefusesBeforeSourceEffectsAndNoLatePrefix(t *testing.T) {
	directory := t.TempDir()
	spec := nativeLocalSource(t, directory, "source", []byte("not JSON"), 0o600)
	for _, config := range []hostconfig.Config{
		nativeDocumentConfig(t, "keys", spec, target.TargetPi, target.ScopeProject, "custom.json", hostconfig.OwnershipKeys, hostconfig.SyntaxDefault),
		nativeDocumentConfig(t, "shared", spec, target.TargetPi, target.ScopeProject, "settings.json", hostconfig.OwnershipDocument, hostconfig.SyntaxDefault),
	} {
		calls := 0
		resolver := &nativeSessionCounter{underlying: nativeDocumentResolver(t, directory), sessions: &calls}
		file, err := BuildNativeDocumentsWithOptions(context.Background(), lockEnvironment(t, desired.Spec{HostConfigs: []hostconfig.Config{config}}), resolver, Options{}, nativeDocumentLimits())
		if err == nil || file.Version != 0 || calls != 0 {
			t.Fatalf("static refusal started source session or returned lock: sessions=%d err=%v", calls, err)
		}
	}
	good := nativeDocumentConfig(t, "a-good", spec, target.TargetPi, target.ScopeProject, "prompts/good.md", hostconfig.OwnershipDocument, hostconfig.SyntaxDefault)
	bad := nativeDocumentConfig(t, "z-invalid", spec, target.TargetPi, target.ScopeProject, "themes/bad.json", hostconfig.OwnershipDocument, hostconfig.SyntaxDefault)
	var successes int
	file, err := BuildNativeDocumentsWithOptions(context.Background(), lockEnvironment(t, desired.Spec{HostConfigs: []hostconfig.Config{good, bad}}), nativeDocumentResolver(t, directory), Options{Events: func(event Event) {
		if event.Kind == EventSnapshotValidated {
			successes++
		}
	}}, nativeDocumentLimits())
	if !errors.Is(err, configdocument.ErrInvalidDocument) || file.Version != 0 || file.Locked.Len() != 0 || successes != 0 {
		t.Fatalf("late invalid document returned a successful prefix: successes=%d err=%v", successes, err)
	}
}

type nativeSessionCounter struct {
	underlying acquisition.Resolver
	sessions   *int
}

func TestNativeDocumentDuplicateDestinationRefusesBeforeSourceSession(t *testing.T) {
	for _, selected := range []target.Target{target.TargetPi, target.TargetCodex} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			t.Run(string(selected)+"/"+string(scope), func(t *testing.T) {
				directory := t.TempDir()
				firstSource := nativeLocalSource(t, directory, "first", []byte("# first\n"), 0o600)
				secondSource := nativeLocalSource(t, directory, "second", []byte("# second\n"), 0o600)
				configs := []hostconfig.Config{
					nativeDocumentConfig(t, "first", firstSource, selected, scope, "prompts/review.md", hostconfig.OwnershipDocument, hostconfig.SyntaxDefault),
					nativeDocumentConfig(t, "second", secondSource, selected, scope, "prompts/review.md", hostconfig.OwnershipDocument, hostconfig.SyntaxDefault),
				}
				environment := lockEnvironment(t, desired.Spec{HostConfigs: configs})
				sessions, events := 0, 0
				resolver := &nativeSessionCounter{underlying: nativeDocumentResolver(t, directory), sessions: &sessions}
				file, err := BuildNativeDocumentsWithOptions(context.Background(), environment, resolver, Options{Events: func(Event) { events++ }}, nativeDocumentLimits())
				if err == nil || file.Version != 0 || file.Locked.Len() != 0 || sessions != 0 || events != 0 {
					t.Fatalf("duplicate destination reached source execution: sessions=%d events=%d version=%d err=%v", sessions, events, file.Version, err)
				}
			})
		}
	}
}

func TestNativeDocumentDestinationPreflightSeparatesTargetScopeAndPath(t *testing.T) {
	directory := t.TempDir()
	spec := nativeLocalSource(t, directory, "source", []byte("# content\n"), 0o600)
	var configs []hostconfig.Config
	for _, selected := range []target.Target{target.TargetPi, target.TargetCodex} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			for _, name := range []string{"review", "reviewer"} {
				configs = append(configs, nativeDocumentConfig(t, string(selected)+"-"+string(scope)+"-"+name, spec, selected, scope, "prompts/"+name+".md", hostconfig.OwnershipDocument, hostconfig.SyntaxDefault))
			}
		}
	}
	sessions := 0
	resolver := &nativeSessionCounter{underlying: nativeDocumentResolver(t, directory), sessions: &sessions}
	file, err := BuildNativeDocumentsWithOptions(context.Background(), lockEnvironment(t, desired.Spec{HostConfigs: configs}), resolver, Options{}, nativeDocumentLimits())
	if err != nil || sessions != 1 || file.Version != lock.NativeVersion || file.Locked.Len() != 2*len(configs) {
		t.Fatalf("disjoint logical destinations conflated: sessions=%d len=%d err=%v", sessions, file.Locked.Len(), err)
	}
}

func (counter *nativeSessionCounter) NewResolutionSession() (acquisition.Resolver, error) {
	*counter.sessions++
	return counter.underlying.(acquisition.ResolutionSessionProvider).NewResolutionSession()
}

func (counter *nativeSessionCounter) Resolve(context.Context, source.Source, acquisition.OperationOptions) (acquisition.Resolution, error) {
	return acquisition.Resolution{}, errors.New("outer resolver must not be used after session selection")
}

func TestNativeDocumentBuildSharesSessionWithOrdinarySourcesAndKeepsEmptyProfile(t *testing.T) {
	directory := t.TempDir()
	spec := nativeLocalSource(t, directory, "source", []byte("# both\n"), 0o600)
	config := nativeDocumentConfig(t, "native", spec, target.TargetPi, target.ScopeProject, "prompts/review.md", hostconfig.OwnershipDocument, hostconfig.SyntaxDefault)
	instruction := projectInstructions(t, "ordinary", spec, []target.Target{target.TargetClaudeCode})
	environment := lockEnvironment(t, desired.Spec{HostConfigs: []hostconfig.Config{config}, Instructions: []instructions.Instructions{instruction}})
	sessions := 0
	resolver := &nativeSessionCounter{underlying: nativeDocumentResolver(t, directory), sessions: &sessions}
	file, err := BuildNativeDocumentsWithOptions(context.Background(), environment, resolver, Options{}, nativeDocumentLimits())
	if err != nil || sessions != 1 || file.Locked.Len() != 4 {
		t.Fatalf("ordinary/native assembly split sessions or lost contracts: sessions=%d len=%d err=%v", sessions, file.Locked.Len(), err)
	}
	assertNativeDocumentRoundtrip(t, directory, file)
	if _, err := BuildWithOptions(context.Background(), environment, resolver, Options{}); err == nil || sessions != 1 {
		t.Fatalf("default builder performed native effects: %v", err)
	}
	empty, err := BuildNativeDocumentsWithOptions(context.Background(), lockEnvironment(t, desired.Spec{}), resolver, Options{}, nativeDocumentLimits())
	if err != nil || empty.Version != lock.NativeVersion || lock.ValidateNative(empty) != nil {
		t.Fatalf("last native removal guessed default profile: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := BuildNativeDocumentsWithOptions(ctx, environment, resolver, Options{}, nativeDocumentLimits()); !errors.Is(err, context.Canceled) || result.Version != 0 {
		t.Fatalf("pre-canceled build returned usable lock: %v", err)
	}
}

func TestNativeDocumentCollectionRejectsMissingOrRetargetedMembers(t *testing.T) {
	directory := t.TempDir()
	spec := nativeLocalSource(t, directory, "source", []byte("# content\n"), 0o600)
	config := nativeDocumentConfig(t, "native", spec, target.TargetPi, target.ScopeProject, "prompts/review.md", hostconfig.OwnershipDocument, hostconfig.SyntaxDefault)
	file, err := BuildNativeDocumentsWithOptions(context.Background(), lockEnvironment(t, desired.Spec{HostConfigs: []hostconfig.Config{config}}), nativeDocumentResolver(t, directory), Options{}, nativeDocumentLimits())
	if err != nil {
		t.Fatal(err)
	}
	resource, _ := file.Locked.ExactSupplySubject(config.ID())
	placement, _ := resource.HostConfigPlacement()
	retarget, err := profile.ResolveHostConfigPlacement(profile.HostConfigPlacementInput{
		Target: placement.Target(), Scope: placement.Scope(), Path: "prompts/other.md", Ownership: placement.Ownership(), Syntax: placement.RequestedSyntax(),
	})
	if err != nil {
		t.Fatal(err)
	}
	wrongProjection, err := lock.NewHostConfigDocumentSubjectContract(config.ID(), retarget)
	if err != nil {
		t.Fatal(err)
	}
	for _, subjects := range [][]lock.LockedSubjectContract{{resource}, {wrongProjection}, {resource, wrongProjection}} {
		if _, err := lock.NewNativeLockedSection(subjects, nil); err == nil {
			t.Fatal("incomplete or retargeted native collection admitted")
		}
	}
	if _, err := lock.NewLockedSection(file.Locked.Subjects(), nil); err == nil {
		t.Fatal("default family admission widened")
	}
	if resource.EntityID().Kind() != entity.KindHostConfig {
		t.Fatal("fixture is not native")
	}
}

func TestNativeDocumentProducerSharesRealGitSnapshotAndRefreshes(t *testing.T) {
	requireBuildGit(t)
	directory := t.TempDir()
	repository := filepath.Join(directory, "repository")
	runBuildGit(t, "", "init", repository)
	runBuildGit(t, repository, "checkout", "-b", "main")
	runBuildGit(t, repository, "config", "user.email", "daem@example.invalid")
	runBuildGit(t, repository, "config", "user.name", "Test")
	for _, name := range []string{"instructions.md", "document.md"} {
		if err := os.WriteFile(filepath.Join(repository, name), []byte("# first\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	first := commitBuildGit(t, repository, "initial exact files")
	config := nativeDocumentConfig(t, "native", mustGitSource(t, repository, "document.md", "main"), target.TargetPi, target.ScopeProject, "prompts/review.md", hostconfig.OwnershipDocument, hostconfig.SyntaxDefault)
	ordinary := projectInstructions(t, "ordinary", mustGitSource(t, repository, "instructions.md", "main"), []target.Target{target.TargetClaudeCode})
	environment := lockEnvironment(t, desired.Spec{HostConfigs: []hostconfig.Config{config}, Instructions: []instructions.Instructions{ordinary}})
	resolver := nativeDocumentResolver(t, directory)
	check := func(commit string) lock.File {
		t.Helper()
		events := newConcurrentSourceEventRecorder()
		file, err := BuildNativeDocumentsWithOptions(context.Background(), environment, resolver, Options{MaxParallelSourceOps: 4, SourceEvents: events.record}, nativeDocumentLimits())
		if err != nil {
			t.Fatal(err)
		}
		assertSourceEventCount(t, events.snapshot(), acquisition.EventFetch, 1)
		for _, id := range []entity.ID{config.ID(), ordinary.ID()} {
			resource, present := file.Locked.ExactSupplySubject(id)
			identity, supplied := resource.ExactSupply()
			if !present || !supplied || identity.ResolvedRef() != artifact.ResolvedRef(commit) {
				t.Fatalf("one build mixed repository snapshots: %q %#v", id, identity)
			}
		}
		return file
	}
	before := check(first)
	if err := os.WriteFile(filepath.Join(repository, "document.md"), []byte("# second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := commitBuildGit(t, repository, "change document source")
	after := check(second)
	if !lock.BuildDelta(before, after).HasChanges() {
		t.Fatal("new build reused stale source session")
	}
}

func TestNativeDocumentProducerBoundsAndMidAssemblyCancellation(t *testing.T) {
	directory := t.TempDir()
	spec := nativeLocalSource(t, directory, "source", []byte(`{"deep":{"value":1}}`), 0o600)
	config := nativeDocumentConfig(t, "native", spec, target.TargetPi, target.ScopeProject, "themes/review.json", hostconfig.OwnershipDocument, hostconfig.SyntaxDefault)
	environment := lockEnvironment(t, desired.Spec{HostConfigs: []hostconfig.Config{config}})
	for _, limits := range []configdocument.Limits{{MaximumBytes: 2, MaximumDepth: 64}, {MaximumBytes: 1 << 20, MaximumDepth: 1}} {
		file, err := BuildNativeDocumentsWithOptions(context.Background(), environment, nativeDocumentResolver(t, directory), Options{}, limits)
		if !errors.Is(err, configdocument.ErrLimitExceeded) || file.Version != 0 {
			t.Fatalf("document admission budget bypassed: %v", err)
		}
	}
	other := nativeDocumentConfig(t, "second", spec, target.TargetPi, target.ScopeProject, "themes/second.json", hostconfig.OwnershipDocument, hostconfig.SyntaxDefault)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var successes int
	file, err := BuildNativeDocumentsWithOptions(ctx, lockEnvironment(t, desired.Spec{HostConfigs: []hostconfig.Config{config, other}}), nativeDocumentResolver(t, directory), Options{Events: func(event Event) {
		if event.Stage == EventStageHostConfig && event.Kind == EventResourceLocked {
			cancel()
		}
		if event.Kind == EventSnapshotValidated {
			successes++
		}
	}}, nativeDocumentLimits())
	if !errors.Is(err, context.Canceled) || file.Version != 0 || file.Locked.Len() != 0 || successes != 0 {
		t.Fatalf("canceled assembly exposed successful prefix: %v", err)
	}
}

func TestNativeDocumentLockReadersSelectGenerationBeforeNestedDecode(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "input.lock.toml")
	for _, fixture := range []struct {
		version   int
		native    bool
		supported int
	}{
		{lock.CurrentVersion, true, lock.NativeVersion}, {lock.NativeVersion, false, lock.CurrentVersion},
	} {
		content := []byte(fmt.Sprintf("version = %d\n[locked.subject\nmalformed nested artifact", fixture.version))
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		reader := lockfile.Load
		if fixture.native {
			reader = lockfile.LoadNative
		}
		file, err := reader(context.Background(), path)
		var unsupported lockfile.UnsupportedVersionError
		if !errors.As(err, &unsupported) || unsupported.Found != int64(fixture.version) || unsupported.Supported != int64(fixture.supported) || file.Version != 0 {
			t.Fatalf("wrong generation reached nested decoder: %v", err)
		}
	}
}

func TestNativeDocumentProducerCancellationAfterLastResourceHasNoSuccess(t *testing.T) {
	directory := t.TempDir()
	spec := nativeLocalSource(t, directory, "source", []byte("# content\n"), 0o600)
	config := nativeDocumentConfig(t, "native", spec, target.TargetPi, target.ScopeProject, "prompts/review.md", hostconfig.OwnershipDocument, hostconfig.SyntaxDefault)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	successes := 0
	file, err := BuildNativeDocumentsWithOptions(ctx, lockEnvironment(t, desired.Spec{HostConfigs: []hostconfig.Config{config}}), nativeDocumentResolver(t, directory), Options{Events: func(event Event) {
		if event.Stage == EventStageHostConfig && event.Kind == EventResourceLocked {
			cancel()
		}
		if event.Kind == EventSnapshotValidated {
			successes++
		}
	}}, nativeDocumentLimits())
	if !errors.Is(err, context.Canceled) || file.Version != 0 || successes != 0 {
		t.Fatalf("cancellation after final resource became complete success: successes=%d err=%v", successes, err)
	}
}
