package mcpcodec

import (
	"bytes"
	"reflect"
	"testing"

	tomlsyntax "github.com/isty2e/daem/internal/encoding/toml"
)

func TestCodexMCPEditsPreserveUnmanagedTemporalValues(t *testing.T) {
	input := []byte(`date=1979-05-27
time=07:32:00.123
local=1979-05-27T07:32:00
offset=1979-05-27T07:32:00+05:30
nested={values=[1979-05-27,07:32:00], child={local=1979-05-27T07:32:00}}
[mcp_servers.managed]
command="npx"
args=[]
[mcp_servers.sibling]
command="node"
schedule=1979-05-27
`)
	project, err := CanonicalCodexProjectMCPServerEntry(validCodexMCPProjection("managed"))
	if err != nil {
		t.Fatal(err)
	}
	global, err := CanonicalCodexGlobalMCPServerEntry(validCodexGlobalMCPProjection("managed"))
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []struct {
		name      string
		canonical []byte
		merge     func([]byte, string, []byte) ([]byte, error)
		remove    func([]byte, string) ([]byte, error)
	}{
		{"project", project, mergeCodexProjectMCPServerCanonicalEntry, removeCodexProjectMCPServerProjection},
		{"global", global, mergeCodexGlobalMCPServerCanonicalEntry, removeCodexGlobalMCPServerProjection},
	} {
		for _, remove := range []bool{false, true} {
			t.Run(operation.name+map[bool]string{false: "/merge", true: "/remove"}[remove], func(t *testing.T) {
				original := bytes.Clone(input)
				before, err := tomlsyntax.Native(t.Context(), input)
				if err != nil {
					t.Fatal(err)
				}
				delete(before["mcp_servers"].(map[string]any), "managed")
				var edited []byte
				if remove {
					edited, err = operation.remove(input, "managed")
				} else {
					edited, err = operation.merge(input, "managed", operation.canonical)
				}
				if err != nil {
					t.Fatal(err)
				}
				after, err := tomlsyntax.Native(t.Context(), edited)
				if err != nil {
					t.Fatal(err)
				}
				_, managed := after["mcp_servers"].(map[string]any)["managed"]
				if managed == remove {
					t.Fatal("managed entry has the wrong lifecycle result", managed, remove)
				}
				delete(after["mcp_servers"].(map[string]any), "managed")
				if !reflect.DeepEqual(before, after) || !bytes.Equal(input, original) {
					t.Fatal("MCP edit changed unmanaged typed values or its input", before, after)
				}
			})
		}
	}
}
