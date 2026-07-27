package mcpcodec

import (
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
)

func TestMCPRuntimeProbeLaunchDecoderLowersCanonicalEntriesDefensively(t *testing.T) {
	claudeProjection := validMCPProjection("context7")
	claudeProjection.Command = "node"
	claudeProjection.Args = []string{"server.js", "--stdio"}
	claudeProjection.Env = map[string]string{"API_TOKEN": "${HOST_TOKEN}"}
	claudeCanonical := mustCanonicalClaudeProjectMCPServerEntry(t, claudeProjection)

	command, args, env, err := DecodeRuntimeProbeLaunch(
		aggregate.MCPPlacementClaudeProject,
		string(claudeCanonical),
	)
	if err != nil {
		t.Fatalf("Claude DecodeRuntimeProbeLaunch returned error: %v", err)
	}
	if command != "node" ||
		!slices.Equal(args, []string{"server.js", "--stdio"}) ||
		env["API_TOKEN"] != "HOST_TOKEN" {
		t.Fatalf("Claude runtime launch = %q %#v %#v", command, args, env)
	}
	args[0] = "mutated"
	env["API_TOKEN"] = "MUTATED"
	_, secondArgs, secondEnv, err := DecodeRuntimeProbeLaunch(
		aggregate.MCPPlacementClaudeProject,
		string(claudeCanonical),
	)
	if err != nil {
		t.Fatalf("second Claude DecodeRuntimeProbeLaunch returned error: %v", err)
	}
	if secondArgs[0] != "server.js" || secondEnv["API_TOKEN"] != "HOST_TOKEN" {
		t.Fatalf("runtime launch aliased caller mutations: %#v %#v", secondArgs, secondEnv)
	}

	openCodeProjection := validOpenCodeMCPProjection("context7")
	openCodeProjection.Command = "node"
	openCodeProjection.Args = []string{"server.js"}
	openCodeCanonical, err := CanonicalOpenCodeProjectMCPServerEntry(openCodeProjection)
	if err != nil {
		t.Fatalf("CanonicalOpenCodeProjectMCPServerEntry returned error: %v", err)
	}
	command, args, env, err = DecodeRuntimeProbeLaunch(
		aggregate.MCPPlacementOpenCodeProject,
		string(openCodeCanonical),
	)
	if err != nil {
		t.Fatalf("OpenCode DecodeRuntimeProbeLaunch returned error: %v", err)
	}
	if command != "node" || !slices.Equal(args, []string{"server.js"}) || len(env) != 0 {
		t.Fatalf("OpenCode runtime launch = %q %#v %#v", command, args, env)
	}

	if _, _, _, err := DecodeRuntimeProbeLaunch(
		aggregate.MCPPlacementCodexProject,
		`{"command":"node"}`,
	); err == nil ||
		!strings.Contains(err.Error(), "does not support runtime probes") {
		t.Fatalf("unsupported DecodeRuntimeProbeLaunch error = %v", err)
	}
}

func TestMCPRuntimeProbeLaunchDecoderRejectsMalformedOrSecretBearingCanonicalEntries(t *testing.T) {
	if _, _, _, err := DecodeRuntimeProbeLaunch(
		aggregate.MCPPlacementClaudeProject,
		`{"type":"stdio","command":"node","args":[],"env":{"TOKEN":"SECRET"}}`,
	); err == nil {
		t.Fatal("Claude DecodeRuntimeProbeLaunch accepted a literal secret")
	}

	if _, _, _, err := DecodeRuntimeProbeLaunch(
		aggregate.MCPPlacementOpenCodeProject,
		`{"type":"local","command":[]}`,
	); err == nil {
		t.Fatal("OpenCode DecodeRuntimeProbeLaunch accepted an empty command vector")
	}
}

func TestRuntimeProbeLaunchDecoderCatalogMatchesProfileCapabilities(t *testing.T) {
	if err := validateRuntimeProbeLaunchDecoderCatalog(
		runtimeProbeLaunchDecoderCatalog,
		profile.MCPRuntimeProbeCapabilities(),
	); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeProbeLaunchDecoderCatalogRejectsPolicyAndSyntaxDrift(t *testing.T) {
	decoders := append([]runtimeProbeLaunchDecoder(nil), runtimeProbeLaunchDecoderCatalog...)
	capabilities := profile.MCPRuntimeProbeCapabilities()
	tests := []struct {
		name         string
		decoders     []runtimeProbeLaunchDecoder
		capabilities []profile.MCPRuntimeProbeCapability
		want         string
	}{
		{
			name:         "capability without decoder",
			decoders:     decoders[1:],
			capabilities: capabilities,
			want:         "has no launch decoder",
		},
		{
			name:         "decoder without capability",
			decoders:     decoders,
			capabilities: capabilities[1:],
			want:         "has no profile capability",
		},
		{
			name:         "duplicate decoder",
			decoders:     append(append([]runtimeProbeLaunchDecoder(nil), decoders...), decoders[0]),
			capabilities: capabilities,
			want:         "share placement",
		},
		{
			name: "nil decoder",
			decoders: []runtimeProbeLaunchDecoder{{
				placementID: aggregate.MCPPlacementClaudeProject,
			}},
			capabilities: capabilities[:1],
			want:         "is nil",
		},
		{
			name: "unknown decoder placement",
			decoders: []runtimeProbeLaunchDecoder{{
				placementID: "unknown-placement",
				decode:      claudeProjectMCPRuntimeProbeLaunch,
			}},
			capabilities: nil,
			want:         "is not implemented",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateRuntimeProbeLaunchDecoderCatalog(test.decoders, test.capabilities)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}
