package journal

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeWriterPartitionPathOrdering(t *testing.T) {
	for _, test := range []struct {
		name    string
		paths   []documentWriterPath
		refuses bool
	}{
		{name: "one group aliases", paths: []documentWriterPath{{components: []string{"a"}, group: 0}, {components: []string{"a", "b"}, group: 0}, {components: []string{"a"}, group: 0}}},
		{name: "ordinary projection siblings", paths: []documentWriterPath{{components: []string{"a"}, group: -1}, {components: []string{"a"}, group: -1}}},
		{name: "disjoint sibling names", paths: []documentWriterPath{{components: []string{"a", "b"}, group: 0}, {components: []string{"a-b"}, group: -1}}},
		{name: "different volumes", paths: []documentWriterPath{{volume: "one", components: []string{"a"}, group: 0}, {volume: "two", components: []string{"a"}, group: -1}}},
		{name: "same physical file", paths: []documentWriterPath{{components: []string{"a"}, group: 0}, {components: []string{"a"}, group: -1}}, refuses: true},
		{name: "different native groups", paths: []documentWriterPath{{components: []string{"a"}, group: 0}, {components: []string{"a"}, group: 1}}, refuses: true},
		{name: "ordinary ancestor", paths: []documentWriterPath{{components: []string{"a"}, group: -1}, {components: []string{"a", "b"}, group: 0}}, refuses: true},
		{name: "native ancestor", paths: []documentWriterPath{{components: []string{"a"}, group: 0}, {components: []string{"a", "b"}, group: -1}}, refuses: true},
		{name: "same owner descendants retain covering owner", paths: []documentWriterPath{{components: []string{"a"}, group: 0}, {components: []string{"a", "b"}, group: 0}, {components: []string{"a", "c"}, group: -1}}, refuses: true},
		{name: "lexical sibling must not hide ancestor", paths: []documentWriterPath{{components: []string{"a"}, group: -1}, {components: []string{"a-b"}, group: 1}, {components: []string{"a", "b"}, group: 0}}, refuses: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var permute func([]documentWriterPath, int)
			permute = func(paths []documentWriterPath, position int) {
				if position == len(paths) {
					if err := validateDocumentWriterPaths(t.Context(), slices.Clone(paths)); (err != nil) != test.refuses {
						t.Fatalf("path partition depends on input ordering or has wrong overlap result: %v", err)
					}
					return
				}
				for index := position; index < len(paths); index++ {
					paths[position], paths[index] = paths[index], paths[position]
					permute(paths, position+1)
					paths[position], paths[index] = paths[index], paths[position]
				}
			}
			permute(slices.Clone(test.paths), 0)
		})
	}
}

func TestNativeWriterPartitionProtectsSelectedRoutesAndIndependentRoots(t *testing.T) {
	fixture := nativeJournalFixture(t)
	group := &fixture.record.DocumentGroups[0]
	group.Writer.Root = fixture.record.ManifestRootProvenance
	group.Writer.RelativePath = "physical/config.json"
	alias := group.Aliases[0]
	aliasRoot := alias.LogicalRoot.Provenance
	address, err := alias.Address.canonical()
	if err != nil {
		t.Fatal(err)
	}
	aliasPath := filepath.Join(aliasRoot.PhysicalRoot, filepath.FromSlash(address.AggregateRoot().RelativePath()))
	for _, test := range []struct {
		name    string
		writer  string
		refuses bool
	}{
		{name: "selected alias route", writer: aliasPath, refuses: true},
		{name: "selected alias parent", writer: filepath.Dir(aliasPath), refuses: true},
		{name: "physical referent", writer: filepath.Join(group.Writer.Root.PhysicalRoot, "physical", "config.json"), refuses: true},
		{name: "physical parent", writer: filepath.Join(group.Writer.Root.PhysicalRoot, "physical"), refuses: true},
		{name: "different selected root", writer: filepath.Join(t.TempDir(), "independent.json")},
	} {
		t.Run(test.name, func(t *testing.T) {
			record := fixture.record
			record.Entries = []recoveryEntry{{Scope: string(target.ScopeGlobal), Path: alias.Address.Destination, GlobalPathBinding: &recoveryGlobalPathBinding{ResolvedPath: test.writer}}}
			if err := record.validateWriterPartition(t.Context()); (err != nil) != test.refuses {
				t.Fatalf("rooted writer/selected route partition: %v", err)
			}
		})
	}
}

func aliasedMixedWriterFixture(t *testing.T) mixedJournalFixture {
	t.Helper()
	fixture := mixedStageFixture(t)
	root := fixture.whole.namespace.roots.Manifest
	ordinaryPath := filepath.Join(root.SelectedPath, "AGENTS.md")
	before := fixture.comparison.before.Content()
	requireWholeGroupWrite(t, ordinaryPath, before, 0o644)
	if err := os.Chmod(ordinaryPath, 0o644); err != nil {
		t.Fatal(err)
	}
	address := fixture.whole.namespace.Addresses()[0]
	aliasPath := filepath.Join(root.SelectedPath, filepath.FromSlash(address.AggregateRoot().RelativePath()))
	if err := os.Remove(aliasPath); err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(filepath.Dir(aliasPath), ordinaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(relative, aliasPath); err != nil {
		t.Fatal(err)
	}
	namespace, err := ObserveNativeDocumentNamespace(t.Context(), fixture.whole.namespace.Addresses(), fixture.whole.namespace.roots, journalTestFilesystem(), fixture.whole.input.budget)
	if err != nil {
		t.Fatal(err)
	}
	fixture.whole.namespace = namespace
	exact, known := namespace.Observation().Exact()
	if !known {
		t.Fatal("existing aliased file requires exact namespace evidence")
	}
	binding, err := ownership.NewDocumentBinding(fixture.whole.input.owner, address.AggregateRoot(), exact)
	if err != nil {
		t.Fatal(err)
	}
	present, err := ownership.PresentDocumentBinding(binding)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := ownership.NewExactDocumentBindingIntent(fixture.whole.input.owner, address.AggregateRoot(), present, present)
	if err != nil {
		t.Fatal(err)
	}
	fixture.whole.input.bindings = []ownership.DocumentBindingIntent{intent}
	group, comparison, err := prepareWholeDocumentGroup(t.Context(), namespace, fixture.whole.input)
	if err != nil {
		t.Fatal(err)
	}
	fixture.journal.record.DocumentGroups, fixture.comparison = []recoveryDocumentGroup{group}, comparison
	hash := artifact.HashFileContent(before)
	entry := recoveryEntryFor("ordinary", "AGENTS.md", string(hash), string(hash), "unused")
	state := resourceStateWithPermissions(entry, string(hash), realization.PathPermissionsExact, 0o644)
	mutation, err := NewManagedPathRecordMutation(state.Subject(), state.ConsumerTargets(), state.Scope(), state.Destination(), hash, hash, state.ContentKind(), 0o644, &state)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := observe.NewManagedPathEvidence(state.Subject(), state.Destination(), true, hash, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	withOrdinary := func(snapshot durable.Snapshot) durable.Snapshot {
		result, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: append(snapshot.ManagedPaths(), state), ManagedAggregates: snapshot.ManagedAggregates()})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	fixture.journal.before, fixture.journal.after = withOrdinary(fixture.whole.envelope.before), withOrdinary(fixture.whole.envelope.after)
	fixture.capture.ManagedPathMutations, fixture.capture.ManagedPathEvidence = []ManagedPathMutation{mutation}, []observe.ManagedPathEvidence{evidence}
	return fixture
}

func TestNativeMixedWriterPartitionRefusesActualAliasedOrdinaryWriter(t *testing.T) {
	fixture := aliasedMixedWriterFixture(t)
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	writer := &mixedNoWriteWriter{}
	fingerprint, err := fixture.journal.stageMixedDocumentJournal(t.Context(), fixture.paths, fixture.capture, []DocumentComparison{fixture.comparison}, writer, documentStageOptions(fixture.whole.envelope, imageBudget(t)))
	if err == nil || fingerprint != "" || (writer.directories+writer.writes) != 0 {
		t.Fatalf("aliased native and ordinary writers staged a usable result: fingerprint=%q calls=%d err=%v", fingerprint, (writer.directories + writer.writes), err)
	}
	if entries, err := os.ReadDir(temporary); err != nil || len(entries) != 0 {
		t.Fatalf("refused writer partition retained ordinary build artifacts: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(fixture.paths.ManifestRoot, "AGENTS.md"))
	if err != nil || !slices.Equal(got, fixture.comparison.before.Content()) {
		t.Fatalf("partition refusal changed host bytes: %v", err)
	}
}

func TestNativeWriterPartitionRefusesAtEveryRecordBoundary(t *testing.T) {
	fixture := aliasedMixedWriterFixture(t)
	source, _ := fixture.source(t, imageBudget(t))
	fixture.journal.record.Entries = source.record.Entries
	fixture.journal.record.ClaimTransitions = source.record.ClaimTransitions
	fixture.journal.record.ProvisionalAcquires = source.record.ProvisionalAcquires
	fixture.journal.record.RemovalIntents = source.record.RemovalIntents
	if content, err := marshalNativeJournalRecord(t.Context(), fixture.journal.record, fixture.journal.before, fixture.journal.after, fixture.whole.envelope.codec); err == nil || content != nil {
		t.Error("strict record construction admitted overlapping physical writers")
	}
	before, after, err := encodeRecoveryJournalSnapshots(fixture.journal.before, fixture.journal.after, fixture.whole.envelope.codec)
	if err != nil {
		t.Fatal(err)
	}
	forged := fixture.journal.record
	forged.StatefileBefore, forged.StatefileAfter = before, after
	content, err := json.Marshal(forged)
	if err != nil {
		t.Fatal(err)
	}
	if decoded, err := decodeNativeJournalRecord(t.Context(), content, fixture.whole.envelope.codec); err == nil || decoded.record.Version != 0 {
		t.Error("strict decoding admitted overlapping physical writers")
	}
	if prepared, err := fixture.journal.prepareDocuments(t.Context(), []DocumentComparison{fixture.comparison}, fixture.whole.input.limits, fixture.whole.envelope.read.codecs); err == nil || prepared.groups != nil {
		t.Error("prepared comparison boundary returned an overlapping group prefix")
	}
	read := fixture.whole.envelope.read
	reader := &nativeNamespaceReader{RootedReader: read.filesystem}
	read.filesystem, read.budget = reader, imageBudget(t)
	if loaded, err := fixture.journal.loadDocuments(t.Context(), read); err == nil || loaded.groups != nil || reader.imageReads != 0 {
		t.Error("artifact boundary admitted overlap or read images before refusing it")
	}
}

func TestNativeWriterPartitionRejectsMalformedAndCanceledMetadata(t *testing.T) {
	fixture := nativeJournalFixture(t)
	for _, change := range []string{"writer escape", "relative root", "missing global binding", "noncanonical global path", "project global selector", "nil context", "canceled"} {
		t.Run(change, func(t *testing.T) {
			record := fixture.record
			record.DocumentGroups = slices.Clone(record.DocumentGroups)
			ctx := t.Context()
			switch change {
			case "writer escape":
				record.DocumentGroups[0].Writer.RelativePath = "../outside"
			case "relative root":
				record.DocumentGroups[0].Writer.Root.PhysicalRoot = "relative"
			case "missing global binding":
				record.Entries = []recoveryEntry{{Scope: string(target.ScopeGlobal), Path: "@pi-agent/other.json"}}
			case "noncanonical global path":
				record.Entries = []recoveryEntry{{Scope: string(target.ScopeGlobal), Path: "@pi-agent/other.json", GlobalPathBinding: &recoveryGlobalPathBinding{ResolvedPath: "relative"}}}
			case "project global selector":
				record.Entries = []recoveryEntry{{Scope: string(target.ScopeProject), Path: "@pi-agent/other.json"}}
			case "nil context":
				ctx = nil
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if err := record.validateWriterPartition(ctx); err == nil || change == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("invalid/canceled metadata admitted a writer partition: %v", err)
			}
		})
	}
}
