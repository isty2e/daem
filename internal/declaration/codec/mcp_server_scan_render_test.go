package codec

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
)

func TestMCPServerScanAndRenderMCPServerBlock(t *testing.T) {
	original := []byte(`version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
env = { API_TOKEN = { from_env = "CONTEXT7_API_TOKEN" } }

[[hook]]
name = "later"
`)
	blocks, err := ScanMCPServerBlocks(original)
	if err != nil {
		t.Fatalf("ScanMCPServerBlocks returned error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("len(blocks) = %d, want 1", len(blocks))
	}
	server := blocks[0].Server
	if server.Name != "context7" ||
		server.Command != declaration.NewMCPAmbientCommand("npx") ||
		len(server.Args) != 2 {
		t.Fatalf("server = %#v, want context7 npx argv", server)
	}
	if server.Env["API_TOKEN"].FromEnv != "CONTEXT7_API_TOKEN" {
		t.Fatalf("env = %#v, want structured from_env", server.Env)
	}
	if strings.Contains(string(original[blocks[0].Start:blocks[0].End]), "[[hook]]") {
		t.Fatalf("scanned range included following hook block")
	}

	rendered := RenderMCPServerBlock(MCPServer{
		Name:      "context7",
		Targets:   []string{"claude-code"},
		Scope:     "project",
		Transport: "stdio",
		Command:   declaration.NewMCPAmbientCommand("npx"),
		Args:      []string{"-y", "@upstash/context7-mcp"},
		Env: map[string]MCPEnvReference{
			"ZZZ":       {FromEnv: "ZZZ_ENV"},
			"API_TOKEN": {FromEnv: "CONTEXT7_API_TOKEN"},
		},
	})
	for _, want := range []string{
		"[[mcp_server]]",
		`name = "context7"`,
		`targets = ["claude-code"]`,
		`scope = "project"`,
		`transport = "stdio"`,
		`command = "npx"`,
		`args = ["-y", "@upstash/context7-mcp"]`,
		`env = { API_TOKEN = { from_env = "CONTEXT7_API_TOKEN" }, ZZZ = { from_env = "ZZZ_ENV" } }`,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered = %q, want %q", rendered, want)
		}
	}
}

func TestMCPServerScanBlocksKeepsNestedEnvTablesInMCPServerBlock(t *testing.T) {
	original := []byte(`[[mcp_server]] # user-authored comment
name = "context7"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "npx"

[mcp_server.env.API_TOKEN]
from_env = "CONTEXT7_API_TOKEN"

[[hook]]
name = "later"
`)

	blocks, err := ScanMCPServerBlocks(original)
	if err != nil {
		t.Fatalf("ScanMCPServerBlocks returned error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("len(blocks) = %d, want 1", len(blocks))
	}
	if blocks[0].Server.Env["API_TOKEN"].FromEnv != "CONTEXT7_API_TOKEN" {
		t.Fatalf("env = %#v, want nested structured from_env", blocks[0].Server.Env)
	}
	block := string(original[blocks[0].Start:blocks[0].End])
	if !strings.Contains(block, "[mcp_server.env.API_TOKEN]") {
		t.Fatalf("block = %q, want nested env table included", block)
	}
	if strings.Contains(block, "[[hook]]") {
		t.Fatalf("block = %q, want following hook excluded", block)
	}
}

func TestMCPServerScanBlocksKeepsQuotedNestedEnvAndFindsNextMCPServer(t *testing.T) {
	original := []byte("[[mcp_server]]\r\n" +
		"name = \"context7\"\r\n" +
		"targets = [\"claude-code\"]\r\n" +
		"scope = \"project\"\r\n" +
		"transport = \"stdio\"\r\n" +
		"command = \"npx\"\r\n" +
		"\r\n" +
		"[mcp_server.env.\"API TOKEN\"]\r\n" +
		"from_env = \"CONTEXT7_API_TOKEN\"\r\n" +
		"\r\n" +
		"[[mcp_server]]\r\n" +
		"name = \"playwright\"\r\n" +
		"targets = [\"claude-code\"]\r\n" +
		"scope = \"project\"\r\n" +
		"transport = \"stdio\"\r\n" +
		"command = \"npx\"")

	blocks, err := ScanMCPServerBlocks(original)
	if err != nil {
		t.Fatalf("ScanMCPServerBlocks returned error: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("len(blocks) = %d, want 2", len(blocks))
	}
	if blocks[0].Server.Env["API TOKEN"].FromEnv != "CONTEXT7_API_TOKEN" {
		t.Fatalf("env = %#v, want quoted nested env key", blocks[0].Server.Env)
	}
	if blocks[1].Server.Name != "playwright" {
		t.Fatalf("second server = %#v, want playwright", blocks[1].Server)
	}
}

func TestMCPServerScanBlocksRejectsFutureCommandObject(t *testing.T) {
	original := []byte(`version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = { local_parameter = "context7_runner" }
args = ["--stdio"]
`)

	_, err := ScanMCPServerBlocks(original)
	if err == nil {
		t.Fatal("ScanMCPServerBlocks returned nil error")
	}
	if !strings.Contains(err.Error(), "command") {
		t.Fatalf("error = %q, want command object rejection", err)
	}
}

func TestMCPServerScanAndRenderPreservesAbsoluteCommandPathForm(t *testing.T) {
	absolutePath := filepath.Join(t.TempDir(), "bin with spaces", "codegraph")
	content := []byte(`[[mcp_server]]
name = "codegraph"
targets = ["antigravity-cli"]
scope = "global"
transport = "stdio"
command = { path = ` + strconv.Quote(absolutePath) + ` }
args = ["serve", "--mcp"]
`)

	blocks, err := ScanMCPServerBlocks(content)
	if err != nil {
		t.Fatalf("ScanMCPServerBlocks returned error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("len(blocks) = %d, want 1", len(blocks))
	}
	command := blocks[0].Server.Command
	if command.Kind() != declaration.MCPCommandKindAbsolutePath ||
		command.Value() != absolutePath {
		t.Fatalf("command = (%d, %q), want exact absolute path", command.Kind(), command.Value())
	}
	rendered := RenderMCPServerBlock(blocks[0].Server)
	want := `command = { path = ` + strconv.Quote(absolutePath) + ` }`
	if !strings.Contains(rendered, want) {
		t.Fatalf("rendered = %q, want %q", rendered, want)
	}
	if strings.Contains(rendered, `command = "`+absolutePath+`"`) {
		t.Fatalf("rendered = %q, absolute path collapsed into portable string form", rendered)
	}
}

func TestMCPServerEnvModelsRemainDistinctAcrossStrictAndPartialDecode(t *testing.T) {
	content := []byte(`version = 1
targets = ["codex"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "npx"
env = { TOKEN = { from_env = "TOKEN", future = "preserve" } }
`)

	if _, err := declaration.DecodeManifest(content); err == nil ||
		!strings.Contains(err.Error(), `unknown env reference key "future"`) {
		t.Fatalf("DecodeManifest error = %v, want strict env reference rejection", err)
	}
	blocks, err := ScanMCPServerBlocks(content)
	if err != nil {
		t.Fatalf("ScanMCPServerBlocks returned error: %v", err)
	}
	if len(blocks) != 1 || blocks[0].Server.Env["TOKEN"].FromEnv != "TOKEN" {
		t.Fatalf("blocks = %#v, want partial MCP env projection", blocks)
	}
}

func TestMCPServerSameProjectionPayloadExcludesRelationIdentity(t *testing.T) {
	left := MCPServer{
		Name: "one", Targets: []string{"codex"}, Scope: "project", Transport: "stdio",
		Command: declaration.NewMCPAmbientCommand("npx"),
		Args:    []string{"-y", "server"}, Env: map[string]MCPEnvReference{"TOKEN": {FromEnv: "TOKEN"}},
	}
	right := left
	right.Name = "two"
	right.Targets = []string{"claude-code"}
	right.Scope = "global"
	if !SameMCPServerProjectionPayload(left, right) {
		t.Fatal("relation identity incorrectly participated in projection payload equality")
	}
	right.Args = []string{"server"}
	if SameMCPServerProjectionPayload(left, right) {
		t.Fatal("different transport arguments compared equal")
	}
}
