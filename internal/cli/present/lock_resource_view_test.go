package clipresent_test

import (
	"bytes"
	"strings"
	"testing"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
)

func TestLockResourceViewKeepsFixedDistinguishingFieldsAndCanonicalJSON(t *testing.T) {
	claude := testMCPSubjectRecord(t, "context7")
	canonical, err := mcpcodec.CanonicalCodexProjectMCPServerEntry(mcpcodec.MCPNoEnvServerProjection{
		ServerID: "context7", Command: "npx", AdapterContract: aggregate.CodexProjectMCPStdioCommandV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	codex := snapshottest.MCPProjection(t, snapshottest.MCPProjectionInput{
		PlacementID: aggregate.MCPPlacementCodexProject, ServerID: "context7",
		LauncherCommand: "npx", CanonicalProjection: string(canonical),
	})
	contracts := append(testInstructionsContracts(t, "global", "AGENTS.md", "instructions"), claude, codex)
	file := snapshottest.File(t, contracts...)
	delta := lock.BuildDelta(lock.File{Version: lock.CurrentVersion}, file)
	encode := func() []byte {
		t.Helper()
		var out bytes.Buffer
		if err := clipresent.PrintJSON(&out, clipresent.JSONInput{Command: "lock", Mode: "dry-run", Lockfile: file, Delta: delta}); err != nil {
			t.Fatal(err)
		}
		return bytes.Clone(out.Bytes())
	}
	jsonBefore := encode()
	for _, verbose := range []bool{false, true} {
		options := clipresent.HumanOptions{Verbose: verbose}
		var output bytes.Buffer
		clipresent.PrintDeltaSummaryWithOptions(&output, delta, options)
		var rows []string
		for line := range strings.SplitSeq(output.String(), "\n") {
			if strings.HasPrefix(line, "  - ") {
				rows = append(rows, line)
			}
		}
		if len(rows) != len(contracts) {
			t.Fatalf("verbose=%t dropped or merged lock entries:\n%s", verbose, output.String())
		}
		seen := make(map[string]bool)
		for _, row := range rows {
			if seen[row] {
				t.Fatalf("distinct subjects rendered identically: %s", row)
			}
			seen[row] = true
		}
		if !strings.Contains(rows[0], `resource="instruction/global"`) || strings.Contains(rows[0], "target=") ||
			!strings.Contains(rows[1], `resource="instruction/global"`) || !strings.Contains(rows[1], "target=codex scope=project placement=") ||
			!strings.Contains(rows[2], `resource="mcp-server/context7"`) || !strings.Contains(rows[2], "target=codex scope=project placement=") ||
			!strings.Contains(rows[3], `resource="mcp-server/context7"`) || !strings.Contains(rows[3], "target=claude-code scope=project placement=") {
			t.Fatalf("verbose=%t wrong logical/structural fields or display order:\n%s", verbose, output.String())
		}
		for _, contract := range []lock.LockedSubjectContract{claude, codex} {
			solo := snapshottest.File(t, contract)
			var single bytes.Buffer
			clipresent.PrintDeltaSummaryWithOptions(&single, lock.BuildDelta(lock.File{Version: lock.CurrentVersion}, solo), options)
			var singleRow string
			for line := range strings.SplitSeq(single.String(), "\n") {
				if strings.HasPrefix(line, "  - ") {
					singleRow = line
				}
			}
			if !seen[singleRow] {
				t.Fatalf("fields depend on sibling entries: %s\n%s", singleRow, output.String())
			}
			if verbose && !strings.Contains(singleRow, `subject="`+contract.SubjectID().String()+`"`) {
				t.Fatalf("verbose lost canonical lock correlation: %s", singleRow)
			}
		}
		instructionsOnly := snapshottest.File(t, contracts[:2]...)
		var instructionsOutput bytes.Buffer
		clipresent.PrintDeltaSummaryWithOptions(&instructionsOutput, lock.BuildDelta(lock.File{Version: lock.CurrentVersion}, instructionsOnly), options)
		if !strings.Contains(instructionsOutput.String(), rows[0]+"\n") || !strings.Contains(instructionsOutput.String(), rows[1]+"\n") {
			t.Fatalf("instruction fields depend on unrelated MCP entries:\n%s", instructionsOutput.String())
		}
	}
	if !bytes.Equal(jsonBefore, encode()) {
		t.Fatal("human lock rendering changed canonical JSON")
	}
}

func TestPrivateLockResourcesKeepTheSameDefaultAndVerboseOrder(t *testing.T) {
	makeFile := func(privateSource, publicSource string) lock.File {
		t.Helper()
		return presentOrderFile(t, []desiredextension.Extension{
			presentOrderExtension(t, "alpha-local", privateSource),
			presentOrderExtension(t, "zeta-public", publicSource),
		})
	}
	empty := lock.File{Version: lock.CurrentVersion}
	before := makeFile("./pkgs/old-private-carrier", "npm:old-public-package")
	after := makeFile("./pkgs/current-private-carrier", "npm:current-public-package")
	for _, test := range []struct {
		bucket string
		delta  lock.Delta
	}{
		{"added", lock.BuildDelta(empty, after)},
		{"changed", lock.BuildDelta(before, after)},
		{"removed", lock.BuildDelta(before, empty)},
		{"unchanged", lock.BuildDelta(after, after)},
	} {
		t.Run(test.bucket, func(t *testing.T) {
			for _, verbose := range []bool{false, true} {
				var output bytes.Buffer
				clipresent.PrintDeltaSummaryWithOptions(&output, test.delta, clipresent.HumanOptions{Verbose: verbose})
				if test.bucket == "unchanged" && !verbose {
					if strings.Contains(output.String(), "resource=") {
						t.Fatalf("default unchanged visibility changed:\n%s", output.String())
					}
					continue
				}
				local := strings.Index(output.String(), `resource="extension/alpha-local"`)
				public := strings.Index(output.String(), `resource="extension/zeta-public"`)
				if local < 0 || public <= local {
					t.Fatalf("%s verbose=%t lost a declaration or changed resource order:\n%s", test.bucket, verbose, output.String())
				}
				if !verbose && strings.Contains(output.String(), "private-carrier") {
					t.Fatalf("default lock output disclosed private source:\n%s", output.String())
				}
			}
		})
	}
}

func TestLockChangeBucketsUseTheSameHumanOrder(t *testing.T) {
	makeFile := func(command string) lock.File {
		t.Helper()
		claude, err := mcpcodec.CanonicalClaudeProjectMCPServerEntry(mcpcodec.ClaudeProjectMCPServerProjection{
			ServerID: "ctx", Command: command, AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
		})
		if err != nil {
			t.Fatal(err)
		}
		codex, err := mcpcodec.CanonicalCodexProjectMCPServerEntry(mcpcodec.MCPNoEnvServerProjection{
			ServerID: "ctx", Command: command, AdapterContract: aggregate.CodexProjectMCPStdioCommandV1,
		})
		if err != nil {
			t.Fatal(err)
		}
		return snapshottest.File(
			t,
			snapshottest.MCPProjection(t, snapshottest.MCPProjectionInput{
				PlacementID: aggregate.MCPPlacementClaudeProject, ServerID: "ctx", LauncherCommand: command, CanonicalProjection: string(claude),
			}),
			snapshottest.MCPProjection(t, snapshottest.MCPProjectionInput{
				PlacementID: aggregate.MCPPlacementCodexProject, ServerID: "ctx", LauncherCommand: command, CanonicalProjection: string(codex),
			}),
		)
	}
	empty := lock.File{Version: lock.CurrentVersion}
	before, after := makeFile("before"), makeFile("after")
	for _, test := range []struct {
		bucket string
		delta  lock.Delta
	}{
		{"added", lock.BuildDelta(empty, after)},
		{"changed", lock.BuildDelta(before, after)},
		{"removed", lock.BuildDelta(before, empty)},
		{"unchanged", lock.BuildDelta(after, after)},
	} {
		t.Run(test.bucket, func(t *testing.T) {
			var output bytes.Buffer
			clipresent.PrintDeltaSummaryWithOptions(&output, test.delta, clipresent.HumanOptions{Verbose: true})
			if !strings.Contains(output.String(), test.bucket+" lock entries:\n") {
				t.Fatalf("wrong change bucket:\n%s", output.String())
			}
			codex := strings.Index(output.String(), `resource="mcp-server/ctx" subject="projection/codex.project.mcp-server/ctx"`)
			claude := strings.Index(output.String(), `resource="mcp-server/ctx" subject="projection/claude-code.project.mcp-server/ctx"`)
			if codex < 0 || claude <= codex {
				t.Fatalf("wrong target order in %s bucket:\n%s", test.bucket, output.String())
			}
		})
	}
}
