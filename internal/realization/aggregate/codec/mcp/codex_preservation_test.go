package mcpcodec

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/encoding/tomlstrict"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func codexNativeDocument(t *testing.T, content []byte) configdocument.Document {
	t.Helper()
	document, err := configdocument.Parse(context.Background(), content, configdocument.TOML, configdocument.Limits{
		MaximumBytes: maximumDocumentBytes, MaximumDepth: tomlstrict.MaximumDepth,
	})
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func assertCodexPreservation(t *testing.T, before, after []byte, paths ...[]string) {
	t.Helper()
	original := codexNativeDocument(t, before)
	updated := codexNativeDocument(t, after)
	wantComments, gotComments := original.Comments(), updated.Comments()
	slices.Sort(wantComments)
	slices.Sort(gotComments)
	if !slices.Equal(wantComments, gotComments) {
		t.Fatalf("Codex edit changed actual comments: want %q, got %q", wantComments, gotComments)
	}
	for _, segments := range paths {
		key, err := output.NewKeyPath(segments...)
		if err != nil {
			t.Fatal(err)
		}
		want, wanted, err := original.Lookup(key)
		if err != nil {
			t.Fatal(err)
		}
		got, present, err := updated.Lookup(key)
		if err != nil || present != wanted || wanted && !got.Equal(want) {
			t.Fatalf("unmanaged value %q changed: %v", segments, err)
		}
	}
}

func TestCodexMCPMergePreservesCommentsAndForeignValues(t *testing.T) {
	inputs := []string{
		`# document comment
title = "# string marker is not a comment"
[profiles.dev] # profile comment
model = "small"
[mcp_servers.foreign] # foreign comment
command = "foreign"
args = []
date = 1979-05-27
[mcp_servers.demo] # managed comment
command = "old"
args = ["old"] # argument comment
# trailing comment
`,
		`# inline document
title = "keep"
profiles = { dev = { model = "small" } }
mcp_servers = {
  # managed inline comment
  demo = { command = "old", args = ["old"], },
  foreign = {
    command = "foreign", args = [], date = 1979-05-27,
  }, # foreign inline comment
}
`,
	}
	canonical, err := encodeCodexProjectMCPServerEntry(CodexProjectMCPServerEntry{
		Command: "new", Args: []string{"--new"},
	}, "demo")
	if err != nil {
		t.Fatal(err)
	}
	for name, merge := range map[string]func([]byte, string, []byte) ([]byte, error){
		"project": mergeCodexProjectMCPServerCanonicalEntry,
		"global":  mergeCodexGlobalMCPServerCanonicalEntry,
	} {
		t.Run(name, func(t *testing.T) {
			for _, input := range inputs {
				updated, err := merge([]byte(input), "demo", canonical)
				if err != nil {
					t.Fatal(err)
				}
				assertCodexPreservation(t, []byte(input), updated,
					[]string{"title"}, []string{"profiles"}, []string{"mcp_servers", "foreign"})
				entry, present, err := ExtractCodexProjectMCPServerProjection(updated, "demo")
				if err != nil || !present || entry.Command != "new" || !slices.Equal(entry.Args, []string{"--new"}) {
					t.Fatalf("selected Codex value did not change: %#v, %v", entry, err)
				}
			}
		})
	}
}

func TestCodexMCPRemovalPreservesCommentsOnlyDocument(t *testing.T) {
	input := []byte(`# document comment
[mcp_servers.demo] # managed comment
command = "old"
args = []
`)
	for _, retainParent := range []bool{false, true} {
		updated, keepFile, err := restoreRemoveCodexProjectMCPServerProjection(input, "demo", retainParent)
		if err != nil || !keepFile {
			t.Fatalf("commented document was removed: keep=%v, %v", keepFile, err)
		}
		assertCodexPreservation(t, input, updated)
		key, err := output.NewKeyPath("mcp_servers")
		if err != nil {
			t.Fatal(err)
		}
		parent, present, err := codexNativeDocument(t, updated).Lookup(key)
		if err != nil || present != retainParent || present && !parent.IsObject() {
			t.Fatalf("parent retention = %v, want %v: %v", present, retainParent, err)
		}
	}
}

func TestCodexMCPEncodingHonorsCancelledOperationContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	config, err := decodeCodexProjectMCPConfigContext(ctx, []byte(`title = "keep"`))
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	content, err := config.encode()
	if !errors.Is(err, context.Canceled) || content != nil {
		t.Fatalf("cancelled encoding produced content: %q, %v", content, err)
	}
}

func assertCodexMissingValue(t *testing.T, content []byte, segments ...string) {
	t.Helper()
	path, err := output.NewKeyPath(segments...)
	if err != nil {
		t.Fatal(err)
	}
	_, present, err := codexNativeDocument(t, content).Lookup(path)
	if err != nil || present {
		t.Fatalf("removed native value %q remains: %v", segments, err)
	}
}

func TestCodexMCPMergeBoundsTouchedRootExpansion(t *testing.T) {
	host := []byte("[mcp_servers.foreign]\nnote = '''" + strings.Repeat("\n", 2_200_000) + "'''\n")
	if int64(len(host)) >= maximumDocumentBytes {
		t.Fatalf("host fixture bytes = %d, want below document limit", len(host))
	}
	canonical := mustCanonicalCodexProjectMCPServerEntry(t, validCodexMCPProjection("context7"))
	merged, err := mergeCodexProjectMCPServerCanonicalEntry(host, "context7", canonical)
	assertMCPProjectionReason(t, err, MCPProjectionReasonCanonicalInvalid)
	if merged != nil {
		t.Fatal("oversized native edit returned a writable candidate")
	}
}

func TestCodexMCPFoldAndRestorePreserveNativeValues(t *testing.T) {
	existing := []byte(`# document
title = "keep"
mcp_servers."replace-me".command = "npx" # replacement
mcp_servers."remove-me".command = "npx" # removal
[mcp_servers.foreign] # unmanaged
command = "manual"
values = [nan, +inf, -inf, -0.0, 1979-05-27, 07:32:00]
`)
	for _, tc := range mcpProjectionMutationCases() {
		if tc.placement != aggregate.MCPPlacementCodexProject && tc.placement != aggregate.MCPPlacementCodexGlobal {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			operations, ok := mcpPlacementOperationsForID(tc.placement)
			if !ok {
				t.Fatal("Codex placement operations missing")
			}
			mutations := []MCPProjectionMutation{
				mustMCPProjectionUpsert(t, "replace-me", mustMutationCanonical(t, tc, "replace-me", "node")),
				mustMCPProjectionRemoval(t, "remove-me"),
				mustMCPProjectionInsert(t, "created", mustMutationCanonical(t, tc, "created", "uvx")),
			}
			folded, err := operations.FoldMutations(existing, mutations)
			if err != nil {
				t.Fatal(err)
			}
			if err := operations.VerifyMutations(folded, mutations); err != nil {
				t.Fatal(err)
			}
			assertCodexPreservation(t, existing, folded, []string{"title"}, []string{"mcp_servers", "foreign"})
			restored, keepFile, err := operations.restoreRemove(folded, "created", false)
			if err != nil || !keepFile {
				t.Fatalf("restore removed foreign document: keep=%v, %v", keepFile, err)
			}
			assertCodexMissingValue(t, restored, "mcp_servers", "created")
			assertCodexPreservation(t, folded, restored, []string{"title"}, []string{"mcp_servers", "foreign"}, []string{"mcp_servers", "replace-me"})
		})
	}
}
