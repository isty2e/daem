package mcpcodec

import (
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestMCPNoEnvServerProjectionHasOnlySharedNoEnvFacts(t *testing.T) {
	projectionType := reflect.TypeOf(MCPNoEnvServerProjection{})
	want := []struct {
		name string
		kind reflect.Kind
	}{
		{"ServerID", reflect.String},
		{"Command", reflect.String},
		{"Args", reflect.Slice},
		{"AdapterContract", reflect.String},
	}
	if projectionType.NumField() != len(want) {
		t.Fatalf("MCPNoEnvServerProjection fields = %d, want %d", projectionType.NumField(), len(want))
	}
	for index, field := range want {
		got := projectionType.Field(index)
		if got.Name != field.name || got.Type.Kind() != field.kind {
			t.Fatalf(
				"MCPNoEnvServerProjection field[%d] = %s/%s, want %s/%s",
				index,
				got.Name,
				got.Type.Kind(),
				field.name,
				field.kind,
			)
		}
	}
}

func TestMCPNoEnvServerProjectionRejectsCrossSurfaceAdapterContracts(t *testing.T) {
	tests := []struct {
		name            string
		adapterContract string
		canonicalize    func(MCPNoEnvServerProjection) ([]byte, error)
	}{
		{
			name:            "OpenCode project",
			adapterContract: aggregate.OpenCodeGlobalMCPLocalEnvV1,
			canonicalize:    CanonicalOpenCodeProjectMCPServerEntry,
		},
		{
			name:            "Codex project",
			adapterContract: aggregate.CodexGlobalMCPStdioEnvVarsV1,
			canonicalize:    CanonicalCodexProjectMCPServerEntry,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.canonicalize(MCPNoEnvServerProjection{
				ServerID:        "context7",
				Command:         "npx",
				Args:            []string{"-y", "@upstash/context7-mcp"},
				AdapterContract: test.adapterContract,
			})
			if code, ok := MCPProjectionReasonCodeOf(err); !ok ||
				code != MCPProjectionReasonStaleAdapterContract {
				t.Fatalf("cross-surface adapter error = %v, code = %q", err, code)
			}
		})
	}
}

func TestMCPNoEnvServerProjectionCanonicalEntriesOwnArgs(t *testing.T) {
	tests := []struct {
		name            string
		adapterContract string
		lower           func(MCPNoEnvServerProjection) ([]string, error)
		want            []string
	}{
		{
			name:            "OpenCode project",
			adapterContract: aggregate.OpenCodeProjectMCPLocalCommandV1,
			lower: func(projection MCPNoEnvServerProjection) ([]string, error) {
				entry, err := canonicalOpenCodeProjectMCPServerEntry(projection)
				return entry.Command, err
			},
			want: []string{"npx", "", "--flag", "--flag", " value "},
		},
		{
			name:            "Codex project",
			adapterContract: aggregate.CodexProjectMCPStdioCommandV1,
			lower: func(projection MCPNoEnvServerProjection) ([]string, error) {
				entry, err := canonicalCodexProjectMCPServerEntry(projection)
				return entry.Args, err
			},
			want: []string{"", "--flag", "--flag", " value "},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			projection := MCPNoEnvServerProjection{
				ServerID:        "context7",
				Command:         "npx",
				Args:            []string{"", "--flag", "--flag", " value "},
				AdapterContract: test.adapterContract,
			}
			lowered, err := test.lower(projection)
			if err != nil {
				t.Fatalf("lower projection: %v", err)
			}

			projection.Args[0] = "caller-mutated"
			if !reflect.DeepEqual(lowered, test.want) {
				t.Fatalf("lowered args after caller mutation = %#v, want %#v", lowered, test.want)
			}
		})
	}
}

func TestMCPNoEnvServerProjectionExtractionOwnsArgs(t *testing.T) {
	type extractFunc func([]byte) ([]MCPNoEnvServerProjection, []MCPProjectionRejection, error)
	tests := []struct {
		name    string
		content []byte
		extract extractFunc
	}{
		{
			name:    "OpenCode project",
			content: []byte(`{"mcp":{"context7":{"type":"local","command":["npx","-y"]}}}`),
			extract: ExtractOpenCodeProjectMCPServerProjections,
		},
		{
			name:    "Codex project",
			content: []byte("[mcp_servers.context7]\ncommand = \"npx\"\nargs = [\"-y\"]\n"),
			extract: ExtractCodexProjectMCPServerProjections,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			first, rejections, err := test.extract(test.content)
			if err != nil {
				t.Fatalf("first extraction: %v", err)
			}
			if len(rejections) != 0 || len(first) != 1 {
				t.Fatalf("first extraction = %#v, rejections = %#v", first, rejections)
			}
			first[0].Args[0] = "caller-mutated"

			second, rejections, err := test.extract(test.content)
			if err != nil {
				t.Fatalf("second extraction: %v", err)
			}
			if len(rejections) != 0 || len(second) != 1 {
				t.Fatalf("second extraction = %#v, rejections = %#v", second, rejections)
			}
			if got := second[0].Args; !reflect.DeepEqual(got, []string{"-y"}) {
				t.Fatalf("second extraction args = %#v, want [-y]", got)
			}
		})
	}
}
