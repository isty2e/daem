package nativeconfig

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/statefile"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/output"
	nativehostpath "github.com/isty2e/daem/internal/output/hostpath/native"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

type pointDocumentFixture struct {
	paths               daempaths.Paths
	address             aggregate.DocumentAddress
	preferred, fallback string
	operation           *DocumentObservation
	input               readiness.NativeAssessmentInput
}

func observePointDocument(t *testing.T, selected target.Target) pointDocumentFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	for _, directory := range []string{home, project} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for key, value := range map[string]string{
		"HOME": home, "PI_CODING_AGENT_DIR": filepath.Join(home, "pi"), "CODEX_HOME": filepath.Join(home, "codex"),
		"CLAUDE_CONFIG_DIR": filepath.Join(home, "claude"), "OPENCODE_CONFIG_DIR": filepath.Join(home, "opencode"),
		"XDG_CONFIG_HOME": filepath.Join(home, "config"), "XDG_DATA_HOME": filepath.Join(home, "data"),
		"XDG_STATE_HOME": filepath.Join(home, "state"), "XDG_CACHE_HOME": filepath.Join(home, "cache"),
		"OPENCODE_DB": filepath.Join(home, "opencode.db"), "OPENCODE_PLUGIN_META_FILE": filepath.Join(home, "plugins.json"),
		"CLAUDE_SECURESTORAGE_CONFIG_DIR": filepath.Join(home, "secure"), "CLAUDE_CODE_PLUGIN_CACHE_DIR": filepath.Join(home, "plugins"),
	} {
		t.Setenv(key, value)
	}
	paths, err := daempaths.Resolve(filepath.Join(project, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	alias := aggregate.ClaudeGlobalMCPConfigPath
	if selected == target.TargetAntigravityCLI {
		alias = aggregate.AntigravityGlobalMCPConfigPath
	}
	destination, err := output.Parse(alias)
	if err != nil {
		t.Fatal(err)
	}
	address, err := aggregate.NewDocumentAddress(selected, target.ScopeGlobal, destination)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := configuration.RootBindingForAlias(configuration.AliasInput{Target: selected, Scope: target.ScopeGlobal, Destination: destination})
	if err != nil {
		t.Fatal(err)
	}
	locations, err := nativehostpath.CaptureLocations(project)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := locations.PointCandidates(binding.RootReference())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(candidates[1]), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidates[1], []byte(`{"source":"fallback","mcpServers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	epoch, err := LoadPersistenceEpoch(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := ObserveSelectedDocuments(t.Context(), paths, &epoch, []aggregate.DocumentAddress{address})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := operation.Close(); err != nil {
			t.Error(err)
		}
	})
	input, present := operation.NativeInput()
	if !present || input.Documents == nil {
		t.Fatal("point observation did not retain its evidence")
	}
	return pointDocumentFixture{paths: paths, address: address, preferred: candidates[0], fallback: candidates[1], operation: operation, input: input}
}

func (fixture pointDocumentFixture) namespace(t *testing.T) journalnamespace.DocumentNamespace {
	t.Helper()
	namespaces := fixture.input.Documents.Evidence().Namespaces()
	if len(namespaces) != 1 {
		t.Fatal("point observation did not contain exactly one physical document")
	}
	return namespaces[0]
}

func TestSelectedNativeObservationUsesQualifiedPointEndpoints(t *testing.T) {
	for _, selected := range []target.Target{target.TargetClaudeCode, target.TargetAntigravityCLI} {
		t.Run(string(selected), func(t *testing.T) {
			fixture := observePointDocument(t, selected)
			namespace := fixture.namespace(t)
			wantPath, err := filepath.EvalSymlinks(fixture.fallback)
			if err != nil {
				t.Fatal(err)
			}
			path, err := namespace.WriterPath()
			if err != nil || path != wantPath {
				t.Fatal("point writer did not use the captured endpoint spelling", path, err)
			}
			pin := namespace.WriterPin()
			reference, bound := pin.RootReference()
			if !bound || pin.Coordinate() != configuration.PointSelf() {
				t.Fatal("point writer was lowered to a directory child", pin)
			}
			roots, err := namespace.QualifyRoots(t.Context(), fixture.input.Budget)
			if err != nil {
				t.Fatal(err)
			}
			grant, supplied := roots.Selection(reference)
			alternative, point := grant.PointAlternative()
			want := configuration.PointFallback
			if fixture.preferred == fixture.fallback {
				want = configuration.PointPreferred
			}
			if !supplied || !point || alternative != want {
				t.Fatal("point writer lost its qualified alternative", alternative)
			}
			document, _, present := fixture.input.Documents.Evidence().Document(fixture.address)
			if !present || !document.Exists() || !bytes.Contains(document.Content(), []byte(`"fallback"`)) {
				t.Fatal("point supplier did not read the selected endpoint")
			}
		})
	}
}

func TestRetainedLivePointObservationRefusesAChangedChoice(t *testing.T) {
	fixture := observePointDocument(t, target.TargetClaudeCode)
	namespace := fixture.namespace(t)
	if err := os.WriteFile(fixture.preferred, []byte(`{"source":"preferred"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if capability, err := namespace.AcquireReferent(t.Context(), fixture.input.Budget); err == nil {
		_ = capability.Close()
		t.Fatal("retained live selection silently adopted a changed endpoint choice")
	}
	if err := fixture.operation.Close(); err != nil {
		t.Fatal(err)
	}
	if _, present := fixture.operation.NativeInput(); present {
		t.Fatal("closed observation exposed its released roots")
	}

	epoch, err := LoadPersistenceEpoch(t.Context(), fixture.paths)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := ObserveSelectedDocuments(t.Context(), fixture.paths, &epoch, []aggregate.DocumentAddress{fixture.address})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fresh.Close() }()
	input, present := fresh.NativeInput()
	if !present || input.Documents == nil {
		t.Fatal("fresh point observation has no loaded evidence")
	}
	document, _, present := input.Documents.Evidence().Document(fixture.address)
	if !present || !bytes.Contains(document.Content(), []byte(`"preferred"`)) {
		t.Fatal("fresh point observation did not use the current preferred endpoint")
	}
}

func TestSelectedPointObservationDoesNotCreateAMissingSlot(t *testing.T) {
	fixture := observePointDocument(t, target.TargetClaudeCode)
	if err := fixture.operation.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(fixture.fallback); err != nil {
		t.Fatal(err)
	}
	epoch, err := LoadPersistenceEpoch(t.Context(), fixture.paths)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := ObserveSelectedDocuments(t.Context(), fixture.paths, &epoch, []aggregate.DocumentAddress{fixture.address})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = operation.Close() }()
	input, present := operation.NativeInput()
	if !present || input.Documents == nil {
		t.Fatal("missing point slot did not produce observation evidence")
	}
	document, _, present := input.Documents.Evidence().Document(fixture.address)
	if !present || document.Exists() {
		t.Fatal("missing point slot became a fabricated regular file")
	}
	if _, err := os.Stat(fixture.fallback); !os.IsNotExist(err) {
		t.Fatal("point observation created its missing selected slot", err)
	}
}

type pointRecoveryFixture struct {
	journal journalrecord.Native
	profile recoveryoperation.Profile
	read    journalqualification.ReadOptions
}

func (fixture pointDocumentFixture) recorded(t *testing.T, spelling string) pointRecoveryFixture {
	t.Helper()
	ctx := t.Context()
	namespace := fixture.namespace(t)
	qualified, err := namespace.QualifyRoots(ctx, fixture.input.Budget)
	if err != nil {
		t.Fatal(err)
	}
	manifest, present := qualified.Manifest()
	if !present {
		t.Fatal("point fixture lost its independent manifest root")
	}
	aliases := namespace.Aliases()
	if len(aliases) != 1 {
		t.Fatal("point fixture has competing aliases")
	}
	alias := aliases[0]
	binding, bound := alias.Binding()
	if !bound {
		t.Fatal("point fixture has no recorded binding policy")
	}
	groupAlias := journalrecord.NewBoundDocumentGroupAlias(alias.Address(), binding, alias.Root().Provenance(), alias.Observation())
	draft, err := journalrecord.BeginDocumentGroup(fixture.input.Owner, "point-read-fixture", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	draft, err = draft.WithBoundNamespace(ctx, namespace.Observation(), namespace.WriterPin(), spelling, []journalrecord.DocumentGroupAlias{groupAlias}, manifest.Provenance())
	if err != nil {
		t.Fatal(err)
	}
	document, mode, present := fixture.input.Documents.Evidence().Document(fixture.address)
	if !present {
		t.Fatal("point fixture has no actual loaded image")
	}
	comparison, err := journaldocument.NewSelectedComparison(ctx, journaldocument.ComparisonInput{
		Before: document, Expected: document, BeforeMode: mode, ExpectedMode: mode, Limits: DocumentLimits(),
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := draft.WithComparison(comparison, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	group, _, err := candidate.Seal(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]journalrecord.RootCapture, 0, 2)
	for _, grant := range []journalnamespace.RootGrant{manifest, alias.Root()} {
		alternative, _ := grant.PointAlternative()
		row, err := journalrecord.NewRootCapture(grant.RootReference(), grant.Provenance(), alternative)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	codecs, err := Codecs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	codec, err := statefile.NewNativeCodec(codecs.Contributions())
	if err != nil {
		t.Fatal(err)
	}
	before := fixture.input.Documents.Evidence().BeforeState()
	journal, err := journalrecord.NewNative17Capture(ctx, "point-read-fixture", time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), fixture.input.Owner, manifest.RootReference(), rows, before, before, []journalrecord.SealedDocumentGroup{group}, codec)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := journal.PrepareDocuments(ctx, []journaldocument.Comparison{comparison}, DocumentLimits(), codecs)
	if err != nil {
		t.Fatal(err)
	}
	content, err := prepared.Encode(ctx, codec)
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(artifacts, "journal.json")
	if err := os.WriteFile(journalPath, content, journalrecord.Mode); err != nil {
		t.Fatal(err)
	}
	filesystem := storagecommit.Adapter{}
	snapshot, err := filesystem.ReadRegularFileSnapshotUpTo(ctx, journalPath, journalrecord.MaximumBytes)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := journalrecord.DecodeNativeSnapshot(ctx, snapshot, journalPath, codec)
	if err != nil {
		t.Fatal(err)
	}
	journal, present = decoded.Native()
	if !present {
		t.Fatal("point record decoded as another operation family")
	}
	session, err := journal.BeginLoadedDocuments(ctx, DocumentLimits(), codecs)
	if err != nil {
		t.Fatal(err)
	}
	demand, present, err := session.Next(ctx)
	if err != nil || !present {
		t.Fatal("point record cannot reconstruct its actual image", err)
	}
	for _, image := range []journalrecord.DocumentImage{demand.Original(), demand.Expected()} {
		reference, err := image.FileReference()
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(artifacts, filepath.FromSlash(reference))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, document.Content(), journalrecord.Mode); err != nil {
			t.Fatal(err)
		}
	}
	artifactRoot, err := rootedpath.CaptureRoot(artifacts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = artifactRoot.Close() })
	locations, err := nativehostpath.CaptureLocations(fixture.paths.ManifestRoot)
	if err != nil {
		t.Fatal(err)
	}
	protection, err := SelectProtectionRoots(locations)
	if err != nil {
		t.Fatal(err)
	}
	budget := physicalwork.NewCaptureBudget(nil)
	read := journalqualification.ReadOptions{
		Protection: protection, Rules: journalqualification.RecordedRuleExpectations(), Artifacts: artifactRoot,
		Filesystem: filesystem, Budget: budget, Limits: DocumentLimits(), Codecs: codecs,
	}
	profile := recoveryoperation.Profile{
		StateCodec: codec, Codecs: codecs, Limits: DocumentLimits(), Manifest: manifest.Provenance().PhysicalRoot(),
		ResolvePoint: locations.ResolvePoint, Protection: protection, Budget: budget,
	}
	if err := fixture.operation.Close(); err != nil {
		t.Fatal(err)
	}
	return pointRecoveryFixture{journal: journal, profile: profile, read: read}
}

func TestRecordedPointRecoveryKeepsFallbackAfterPreferredAppears(t *testing.T) {
	fixture := observePointDocument(t, target.TargetClaudeCode)
	spelling, err := fixture.namespace(t).WriterPath()
	if err != nil {
		t.Fatal(err)
	}
	recorded := fixture.recorded(t, spelling)
	if err := os.WriteFile(fixture.preferred, []byte(`{"source":"preferred"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := recorded.profile
	missing.ResolvePoint = nil
	if roots, err := missing.OpenRoots(t.Context(), recorded.journal); err == nil || roots != nil {
		t.Fatal("point record opened without an independent point resolver", err)
	}
	resolver := recorded.profile.ResolvePoint
	var alternatives []configuration.PointAlternative
	recorded.profile.ResolvePoint = func(reference output.RootReference, alternative configuration.PointAlternative) (string, error) {
		alternatives = append(alternatives, alternative)
		return resolver(reference, alternative)
	}
	roots, err := recorded.profile.OpenRoots(t.Context(), recorded.journal)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = roots.Close() }()
	if len(alternatives) != 1 || alternatives[0] != configuration.PointFallback {
		t.Fatal("recovery did not reopen the recorded alternative", alternatives)
	}
	namespaces, err := journalqualification.LoadNamespaces(recorded.journal, t.Context(), recorded.read, roots.Selection())
	if err != nil || len(namespaces) != 1 {
		t.Fatal("recorded point namespace could not be qualified", err)
	}
	if path, err := namespaces[0].WriterPath(); err != nil || path != spelling {
		t.Fatal("recorded writer migrated to the latest endpoint", path, err)
	}
	loaded, err := journalqualification.LoadDocuments(recorded.journal, t.Context(), recorded.read)
	if err != nil || loaded.Len() != 1 {
		t.Fatal("qualified point record could not reconstruct its actual images", err)
	}
	capability, err := namespaces[0].AcquireReferent(t.Context(), recorded.read.Budget)
	if err != nil {
		t.Fatal(err)
	}
	content, _, _, readErr := recorded.read.Filesystem.ReadRootedRegularFileUpTo(t.Context(), capability, DocumentLimits().MaximumBytes)
	closeErr := capability.Close()
	if readErr != nil || closeErr != nil || !bytes.Contains(content, []byte(`"fallback"`)) {
		t.Fatal("recovery selected preferred instead of the recorded fallback", readErr, closeErr)
	}
}

func TestRecordedPointNamespaceRefusesDifferentWriterSpelling(t *testing.T) {
	fixture := observePointDocument(t, target.TargetClaudeCode)
	spelling, err := fixture.namespace(t).WriterPath()
	if err != nil {
		t.Fatal(err)
	}
	recorded := fixture.recorded(t, filepath.Join(filepath.Dir(spelling), "another.json"))
	roots, err := recorded.profile.OpenRoots(t.Context(), recorded.journal)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = roots.Close() }()
	if namespaces, err := journalqualification.LoadNamespaces(recorded.journal, t.Context(), recorded.read, roots.Selection()); err == nil || len(namespaces) != 0 {
		t.Fatal("point namespace accepted spelling unrelated to its qualified endpoint", err)
	}
}
