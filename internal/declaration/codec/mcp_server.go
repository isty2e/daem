package codec

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/isty2e/daem/internal/declaration"
)

type MCPServer struct {
	Name      string                     `toml:"name"`
	Targets   []string                   `toml:"targets"`
	Scope     string                     `toml:"scope"`
	Transport string                     `toml:"transport"`
	Command   string                     `toml:"command"`
	Args      []string                   `toml:"args"`
	Env       map[string]MCPEnvReference `toml:"env"`
}

type MCPEnvReference struct {
	FromEnv string `toml:"from_env"`
}

// SameMCPServerProjectionPayload reports whether two rows have the same transport
// payload independently of name, target, and scope identity.
func SameMCPServerProjectionPayload(left MCPServer, right MCPServer) bool {
	return left.Transport == right.Transport &&
		left.Command == right.Command &&
		slices.Equal(left.Args, right.Args) &&
		maps.Equal(left.Env, right.Env)
}

type MCPServerBlock struct {
	Start  int
	End    int
	Server MCPServer
}

func ScanMCPServerBlocks(content []byte) ([]MCPServerBlock, error) {
	ranges := declaration.ScanDocumentRanges(
		content,
		func(trimmed string) bool { return declaration.StartsArrayTableRoot(trimmed, "mcp_server") },
		startsNewMCPServerTable,
	)
	blocks := make([]MCPServerBlock, 0, len(ranges))
	for _, targetRange := range ranges {
		block, err := parseMCPServerBlock(content, targetRange.Start, targetRange.End)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func startsNewMCPServerTable(trimmedLine string) bool {
	return declaration.StartsTableOutsideRoot(trimmedLine, "mcp_server")
}

func parseMCPServerBlock(content []byte, start int, end int) (MCPServerBlock, error) {
	var decoded struct {
		Servers []MCPServer `toml:"mcp_server"`
	}
	if _, err := toml.Decode(string(content[start:end]), &decoded); err != nil {
		return MCPServerBlock{}, fmt.Errorf("parse existing mcp_server block: %w", err)
	}
	if len(decoded.Servers) != 1 {
		return MCPServerBlock{}, fmt.Errorf("parse existing mcp_server block: expected one server")
	}
	return MCPServerBlock{
		Start:  start,
		End:    end,
		Server: decoded.Servers[0],
	}, nil
}

func RenderMCPServerBlock(server MCPServer) string {
	var builder strings.Builder
	builder.WriteString("[[mcp_server]]\n")
	builder.WriteString("name = ")
	builder.WriteString(strconv.Quote(server.Name))
	builder.WriteByte('\n')
	if len(server.Targets) != 0 {
		builder.WriteString("targets = ")
		builder.WriteString(renderMCPServerStringArray(server.Targets))
		builder.WriteByte('\n')
	}
	if server.Scope != "" {
		builder.WriteString("scope = ")
		builder.WriteString(strconv.Quote(server.Scope))
		builder.WriteByte('\n')
	}
	builder.WriteString("transport = ")
	builder.WriteString(strconv.Quote(server.Transport))
	builder.WriteByte('\n')
	builder.WriteString("command = ")
	builder.WriteString(strconv.Quote(server.Command))
	builder.WriteByte('\n')
	if len(server.Args) != 0 {
		builder.WriteString("args = ")
		builder.WriteString(renderMCPServerStringArray(server.Args))
		builder.WriteByte('\n')
	}
	if len(server.Env) != 0 {
		builder.WriteString("env = ")
		builder.WriteString(renderMCPEnvReferences(server.Env))
		builder.WriteByte('\n')
	}
	return builder.String()
}

func renderMCPServerStringArray(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, strconv.Quote(value))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func renderMCPEnvReferences(env map[string]MCPEnvReference) string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, key+" = { from_env = "+strconv.Quote(env[key].FromEnv)+" }")
	}
	return "{ " + strings.Join(values, ", ") + " }"
}
