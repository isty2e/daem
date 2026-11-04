package compose_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/target"
)

func TestCandidateVerificationUsesSemanticMCPSelectionNotBroadRewriteParent(t *testing.T) {
	native := nativePort(t)
	model := nativeSet(t, native, target.ScopeProject, ".codex/config.toml", path(t, "model"), `"fast"`)
	mcp, server := mcpSet(t, "search")
	original := aggregate.ExistingDocument([]byte("# protected\nmodel='before'\n[mcp_servers.foreign]\ncommand='keep'\n"))
	modelBefore := observe(t, native, original, selectSets(t, model))
	serverBefore := observe(t, mcp, original, selectSets(t, server))
	edits := []compose.ProjectionEdit{
		edit(t, native, desiredPlan(t, modelBefore, map[output.KeyPath]*aggregate.ContributionSet{path(t, "model"): &model}), path(t, "model")),
		edit(t, mcp, desiredPlan(t, serverBefore, map[output.KeyPath]*aggregate.ContributionSet{server.Address().KeyPath(): &server}), path(t, "mcp_servers")),
	}
	participants := []compose.Participant{edits[0].Participant(), edits[1].Participant()}
	candidate, err := compose.Render(t.Context(), original, limits, edits)
	if err != nil {
		t.Fatal(err)
	}
	if err := compose.VerifyCandidate(t.Context(), original, candidate.Document(), limits, participants); err != nil {
		t.Fatal(err)
	}
	if err := compose.VerifyCandidate(t.Context(), candidate.Document(), original, limits, participants); err != nil {
		t.Fatalf("selected removal/replacement could not reconstruct its expected image: %v", err)
	}
	change, err := configdocument.Set(path(t, "mcp_servers", "foreign", "command"), value(t, `"stolen"`))
	if err != nil {
		t.Fatal(err)
	}
	tampered, err := configdocument.Edit(t.Context(), nativeParse(t, candidate.Document()), []configdocument.Change{change})
	if err != nil {
		t.Fatal(err)
	}
	for _, changed := range [][]byte{
		tampered.Bytes(),
		bytes.Replace(candidate.Document().Content(), []byte("# protected"), []byte("# changed"), 1),
		append([]byte("outside=true\n"), candidate.Document().Content()...),
	} {
		if bytes.Equal(changed, candidate.Document().Content()) {
			t.Fatal("fixture did not change protected content")
		}
		if err := compose.VerifyCandidate(t.Context(), original, aggregate.ExistingDocument(changed), limits, participants); err == nil {
			t.Fatal("candidate verification admitted a foreign value or comment")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := compose.VerifyCandidate(ctx, original, candidate.Document(), limits, participants); err != context.Canceled {
		t.Fatalf("cancellation was not preserved: %v", err)
	}
	if err := compose.VerifyCandidate(t.Context(), original, candidate.Document(), limits, nil); err == nil {
		t.Fatal("an empty projection set was accepted")
	}
}
