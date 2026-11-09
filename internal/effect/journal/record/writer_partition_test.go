package record

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeWriterPartitionKeepsMissingRootPrefixes(t *testing.T) {
	for _, projectSibling := range []bool{false, true} {
		name := "two missing global roots"
		if projectSibling {
			name = "missing global root with project sibling"
		}
		t.Run(name, func(t *testing.T) {
			fixture := nativeJournalFixture(t)
			base := fixture.record.ManifestRootProvenance
			template := fixture.record.DocumentGroups[0]
			firstAnchor, firstRelative := "", ".pi/agent/settings.json"
			secondAlias, secondRelative := "@codex-home/config.toml", ".codex/config.toml"
			secondTarget, secondScope := target.TargetCodex, target.ScopeGlobal
			if projectSibling {
				firstAnchor, firstRelative = ".pi", "agent/settings.json"
				secondAlias, secondRelative = ".pi/settings.json", ".pi/settings.json"
				secondTarget, secondScope = target.TargetPi, target.ScopeProject
			}
			fixture.record.DocumentGroups = nil
			for _, spec := range []struct {
				target   target.Target
				scope    target.Scope
				alias    string
				anchor   string
				relative string
			}{
				{target.TargetPi, target.ScopeGlobal, "@pi-agent/settings.json", firstAnchor, firstRelative},
				{secondTarget, secondScope, secondAlias, "", secondRelative},
			} {
				group := template
				root := base
				root.PhysicalRoot = filepath.Join(base.PhysicalRoot, filepath.FromSlash(spec.anchor))
				group.Writer = recoveryDocumentWriter{Root: root, RelativePath: spec.relative}
				exact, err := pathauthority.NewExact(filepath.Join(root.PhysicalRoot, filepath.FromSlash(spec.relative)), "exact-v1:")
				if err != nil {
					t.Fatal(err)
				}
				observation := recoveryDocumentObservation{Kind: "exact", Authority: persistedPathAuthority(exact)}
				role := "global-root"
				if spec.scope == target.ScopeProject {
					role = "manifest-root"
				}
				group.CapturedTarget = observation
				group.Aliases = []recoveryDocumentAlias{{
					Address:     recoveryDocumentAliasRef{Target: string(spec.target), Scope: string(spec.scope), Destination: spec.alias},
					LogicalRoot: recoveryDocumentLogicalRoot{Role: role, Provenance: root}, SelectedObservation: observation, ObservedBefore: observation,
				}}
				fixture.record.DocumentGroups = append(fixture.record.DocumentGroups, group)
			}
			if err := fixture.record.validateWriterSpellingPartition(t.Context()); err != nil {
				t.Fatal("disjoint missing-root writers collapsed to their anchor", err)
			}
			first := fixture.record.DocumentGroups[0].Writer
			second := fixture.record.DocumentGroups[1].Writer
			fixture.record.DocumentGroups[1].Writer = first
			if err := fixture.record.validateWriterSpellingPartition(t.Context()); err == nil {
				t.Fatal("actual writer overlap passed between two anchored document groups")
			}
			fixture.record.DocumentGroups[1].Writer = second
			fixture.record.Entries = []recoveryEntry{{
				Scope: string(target.ScopeGlobal), Path: "@pi-agent/ordinary.json",
				GlobalPathBinding: &recoveryGlobalPathBinding{ResolvedPath: filepath.Join(first.Root.PhysicalRoot, filepath.FromSlash(first.RelativePath))},
			}}
			if err := fixture.record.validateWriterSpellingPartition(t.Context()); err == nil {
				t.Fatal("actual writer overlap passed with an anchor-relative writer")
			}
		})
	}
}

func TestNativeWriterPartitionRefusesOverlappingEncodeAndDecode(t *testing.T) {
	journal, material := ordinaryEvidenceFixture(t, "2026-11-04T00:00:00Z")
	journal, err := journal.WithOrdinaryEvidence(material)
	if err != nil {
		t.Fatal(err)
	}
	content, err := journal.Encode(t.Context(), testStateCodec())
	if err != nil {
		t.Fatal("disjoint writer control could not encode", err)
	}
	if _, err := decodeNativeJournalRecord(t.Context(), content, testStateCodec()); err != nil {
		t.Fatal("disjoint writer control could not decode", err)
	}
	journal.record.DocumentGroups = slices.Clone(journal.record.DocumentGroups)
	journal.record.DocumentGroups[0].Writer.Root = journal.record.ManifestRootProvenance
	journal.record.DocumentGroups[0].Writer.RelativePath = journal.record.Entries[0].Path
	if err := journal.record.DocumentGroups[0].validate(journal.record.ManifestRootProvenance); err != nil {
		t.Fatal("overlap invalidated unrelated document metadata", err)
	}
	if err := journal.record.validateWriterSpellingPartition(t.Context()); err == nil {
		t.Fatal("overlapping fixture does not discriminate partition validation")
	}
	if content, err := journal.Encode(t.Context(), testStateCodec()); err == nil || content != nil {
		t.Error("strict record construction admitted overlapping physical writers")
	}
	before, after, err := EncodeSnapshots(journal.before, journal.after, testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	forged := journal.record
	forged.StatefileBefore, forged.StatefileAfter = before, after
	content, err = json.Marshal(forged)
	if err != nil {
		t.Fatal(err)
	}
	if decoded, err := decodeNativeJournalRecord(t.Context(), content, testStateCodec()); err == nil || decoded.record.Version != 0 {
		t.Error("strict decoding admitted overlapping physical writers")
	}
}

func TestNativeWriterPartitionRejectsMalformedAndCanceledMetadata(t *testing.T) {
	fixture := nativeJournalFixture(t)
	for _, change := range []string{"writer escape", "relative root", "missing global binding", "noncanonical global path", "project global selector", "unbound global alias", "nil context", "canceled"} {
		t.Run(change, func(t *testing.T) {
			record := fixture.record
			record.DocumentGroups = slices.Clone(record.DocumentGroups)
			ctx := t.Context()
			var expected error
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
			case "unbound global alias":
				record.DocumentGroups[0].Aliases = slices.Clone(record.DocumentGroups[0].Aliases)
				record.DocumentGroups[0].Aliases[0].Address = recoveryDocumentAliasRef{Target: string(target.TargetPi), Scope: string(target.ScopeGlobal), Destination: "@codex-home/other.json"}
				expected = configuration.ErrDocumentPath
			case "nil context":
				ctx = nil
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				expected = context.Canceled
			}
			if err := record.validateWriterSpellingPartition(ctx); err == nil || expected != nil && !errors.Is(err, expected) {
				t.Fatalf("invalid/canceled metadata admitted a writer partition: %v", err)
			}
		})
	}
}

func TestDocumentWriterPartitionAccumulatorKeepsGroupAndEmptySemantics(t *testing.T) {
	var empty WriterPartition
	if err := empty.Validate(t.Context()); err != nil {
		t.Fatal("empty writer partition was refused", err)
	}
	parent := filepath.Join(t.TempDir(), "parent")
	for _, sameGroup := range []bool{true, false} {
		var partition WriterPartition
		childGroup := 1
		if sameGroup {
			childGroup = 0
		}
		if err := partition.Add(filepath.Join(parent, "child"), childGroup); err != nil {
			t.Fatal(err)
		}
		if err := partition.Add(parent, 0); err != nil {
			t.Fatal(err)
		}
		if err := partition.Validate(t.Context()); (err == nil) != sameGroup {
			t.Fatal("writer grouping changed overlap admission", sameGroup, err)
		}
	}
	var absent *WriterPartition
	if absent.Add(parent, 0) == nil || absent.Validate(t.Context()) == nil || empty.Validate(nil) == nil {
		t.Fatal("missing writer accumulator or context was admitted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := empty.Validate(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("writer accumulator discarded cancellation", err)
	}
}

func TestDocumentWriterPartitionCopiesRetainEveryCollectedPath(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "parent")
	var original WriterPartition
	if err := original.Add(parent, 0); err != nil {
		t.Fatal(err)
	}
	copied := original
	if err := original.Add(filepath.Join(parent, "child"), 1); err != nil {
		t.Fatal(err)
	}
	if err := copied.Add(parent+"-sibling", 2); err != nil {
		t.Fatal(err)
	}
	if original.Validate(t.Context()) == nil || copied.Validate(t.Context()) == nil {
		t.Fatal("a copied writer collection lost an observed cross-group conflict")
	}
}

func TestNativeWriterPartitionPathOrdering(t *testing.T) {
	for _, test := range []struct {
		name    string
		paths   []documentWriterPath
		refuses bool
	}{
		{name: "one group aliases", paths: []documentWriterPath{{components: []string{"a"}, group: 0}, {components: []string{"a", "b"}, group: 0}, {components: []string{"a"}, group: 0}}},
		{name: "ordinary projection siblings", paths: []documentWriterPath{{components: []string{"a"}, group: -1}, {components: []string{"a"}, group: -1}}},
		{name: "distinct case-sensitive equality keys", paths: []documentWriterPath{{components: []string{"settings.json"}, group: 0}, {components: []string{"SETTINGS.JSON"}, group: -1}}},
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

func TestNativeWriterPartitionDoesNotInventGlobalAliasRoutes(t *testing.T) {
	for _, writer := range []string{"mcp.json", "mcp_extra.json"} {
		t.Run(writer, func(t *testing.T) {
			fixture := nativeJournalFixture(t)
			group := &fixture.record.DocumentGroups[0]
			group.Writer.RelativePath = "physical/config.json"
			alias := &group.Aliases[0]
			alias.Address = recoveryDocumentAliasRef{Target: string(target.TargetPi), Scope: string(target.ScopeGlobal), Destination: "~/.pi/agent/mcp.json"}
			fixture.record.Entries = []recoveryEntry{{
				Scope: string(target.ScopeGlobal), Path: alias.Address.Destination,
				GlobalPathBinding: &recoveryGlobalPathBinding{ResolvedPath: filepath.Join(alias.LogicalRoot.Provenance.PhysicalRoot, writer)},
			}}
			if err := fixture.record.validateWriterSpellingPartition(t.Context()); err != nil {
				t.Fatalf("global alias route inferred without its selected-root prefix: %v", err)
			}
		})
	}
}

func TestNativeWriterPartitionProtectsSelectedRoutesAndIndependentRoots(t *testing.T) {
	fixture := nativeJournalFixture(t)
	group := &fixture.record.DocumentGroups[0]
	group.Writer.Root = fixture.record.ManifestRootProvenance
	group.Writer.RelativePath = "physical/config.json"
	alias := group.Aliases[0]
	alias.Address = recoveryDocumentAliasRef{Target: string(target.TargetPi), Scope: string(target.ScopeProject), Destination: ".pi/custom.json"}
	alias.LogicalRoot = recoveryDocumentLogicalRoot{Role: "manifest-root", Provenance: fixture.record.ManifestRootProvenance}
	group.Aliases[0] = alias
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
			record.Entries = []recoveryEntry{{Scope: string(target.ScopeGlobal), Path: "@pi-agent/ordinary.json", GlobalPathBinding: &recoveryGlobalPathBinding{ResolvedPath: test.writer}}}
			if err := record.validateWriterSpellingPartition(t.Context()); (err != nil) != test.refuses {
				t.Fatalf("rooted writer/selected route partition: %v", err)
			}
		})
	}
}
