package mcp_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/delegate"
	mcpdelegate "github.com/isty2e/daem/internal/realization/delegate/mcp"
)

func TestMCPDelegatePlanEdgeHuntRoundOneNPM(t *testing.T) {
	// Exploit E1: scoped npm packages without selector are floating, not invalid.
	plan := mustMCPDelegatePlan(t, validDelegateMCPServer(t, "npx", []string{"@scope/server"}, nil))
	assertPackageRef(t, plan, delegate.EcosystemNPM, "@scope/server", "", delegate.PinFloating)

	// Exploit E2: --package=... is an explicit package selector, not an option to ignore.
	plan = mustMCPDelegatePlan(t, validDelegateMCPServer(t, "npx", []string{"--package=server@1.0.0", "server"}, nil))
	assertPackageRef(t, plan, delegate.EcosystemNPM, "server", "1.0.0", delegate.PinPinned)

	// Exploit E3: --package value beats the later command argv.
	plan = mustMCPDelegatePlan(t, validDelegateMCPServer(t, "npx", []string{"--package", "server@2.0.0", "server-bin"}, nil))
	assertPackageRef(t, plan, delegate.EcosystemNPM, "server", "2.0.0", delegate.PinPinned)

	// Exploit E4: -- stops option parsing without becoming package identity itself.
	plan = mustMCPDelegatePlan(t, validDelegateMCPServer(t, "npx", []string{"--yes", "--", "server@3.0.0"}, nil))
	assertPackageRef(t, plan, delegate.EcosystemNPM, "server", "3.0.0", delegate.PinPinned)

	// Explore X1: an npm spec ending in @ remains executable but cannot produce
	// canonical package facts.
	plan = mustMCPDelegatePlan(t, validDelegateMCPServer(t, "npx", []string{"server@"}, nil))
	if plan.PinPolicy() != delegate.PinFloating || len(plan.PackageRefs()) != 0 {
		t.Fatalf("opaque npm plan = pin %q packages %#v, want floating with no canonical package", plan.PinPolicy(), plan.PackageRefs())
	}

	// Explore X2: @latest is explicit but floating, not an exact pin claim.
	plan = mustMCPDelegatePlan(t, validDelegateMCPServer(t, "npx", []string{"server@latest"}, nil))
	assertPackageRef(t, plan, delegate.EcosystemNPM, "server", "latest", delegate.PinFloating)

	// Explore X3: an empty argv element remains exact argv but cannot satisfy
	// canonical package identity.
	plan = mustMCPDelegatePlan(t, validDelegateMCPServer(t, "npx", []string{"-y", ""}, nil))
	if plan.PinPolicy() != delegate.PinFloating || len(plan.PackageRefs()) != 0 {
		t.Fatalf("empty npm package plan = pin %q packages %#v, want floating with no canonical package", plan.PinPolicy(), plan.PackageRefs())
	}
}

func TestMCPDelegatePlanEdgeHuntRoundTwoDockerAndUVX(t *testing.T) {
	// Exploit E1: uvx --from=... owns the package identity even when command follows.
	plan := mustMCPDelegatePlan(t, validDelegateMCPServer(t, "uvx", []string{"--from=mcp-server==0.4.0", "mcp-server"}, nil))
	assertPackageRef(t, plan, delegate.EcosystemPython, "mcp-server", "0.4.0", delegate.PinPinned)

	// Exploit E2: Docker digest selectors are package selectors, not part of the image name.
	digest := "sha256:" + strings.Repeat("a", 64)
	plan = mustMCPDelegatePlan(t, validDelegateMCPServer(t, "docker", []string{"run", "ghcr.io/acme/server@" + digest}, nil))
	assertPackageRef(t, plan, delegate.EcosystemContainer, "ghcr.io/acme/server", digest, delegate.PinPinned)

	// Exploit E3: Docker options with values must not be mistaken for image identity.
	plan = mustMCPDelegatePlan(t, validDelegateMCPServer(t, "docker", []string{"run", "--name", "daemon", "ghcr.io/acme/server:1.0.0"}, nil))
	assertPackageRef(t, plan, delegate.EcosystemContainer, "ghcr.io/acme/server", "1.0.0", delegate.PinFloating)

	// Exploit E4: registry port and image tag are separate colons.
	plan = mustMCPDelegatePlan(t, validDelegateMCPServer(t, "docker", []string{"run", "localhost:5000/acme/server:1.0.0"}, nil))
	assertPackageRef(t, plan, delegate.EcosystemContainer, "localhost:5000/acme/server", "1.0.0", delegate.PinFloating)

	// Explore X1: a trailing tag delimiter remains exact argv but cannot produce
	// canonical image facts.
	plan = mustMCPDelegatePlan(t, validDelegateMCPServer(t, "docker", []string{"run", "ghcr.io/acme/server:"}, nil))
	if plan.PinPolicy() != delegate.PinFloating || len(plan.PackageRefs()) != 0 {
		t.Fatalf("opaque docker plan = pin %q packages %#v, want floating with no canonical package", plan.PinPolicy(), plan.PackageRefs())
	}

	// Explore X2: Docker command without an image is a missing package, not plain command fallback.
	assertMCPDelegateReason(t, mcpdelegate.MCPDelegatePlanReasonMissingPackage, validDelegateMCPServer(t, "docker", []string{"run", "--rm"}, nil))
}

func TestMCPDelegatePlanEdgeHuntRoundThreeEnvAndPlain(t *testing.T) {
	// Exploit E1: delegate env identity uses host from_env names, not projection keys.
	server := validDelegateMCPServer(t, "node", []string{"server.js"}, map[string]string{
		"API_TOKEN": "HOST_TOKEN", "OTHER_TOKEN": "HOST_TOKEN",
	})
	plan := mustMCPDelegatePlan(t, server)
	if !slices.Equal(plan.Env().SourceNames(), []string{"HOST_TOKEN"}) {
		t.Fatalf("delegate env sources = %#v, want deduped host env name only", plan.Env().SourceNames())
	}

	// Exploit E2: spaces inside argv entries are literal argv identity for plain commands.
	plan = mustMCPDelegatePlan(t, validDelegateMCPServer(t, "python3", []string{"-m", "module name"}, nil))
	if plan.Runner().Kind() != delegate.RunnerPlain || plan.Command().Args()[1] != "module name" {
		t.Fatalf("plain argv not preserved: runner=%q args=%#v", plan.Runner().Kind(), plan.Command().Args())
	}

	// Explore X1: unknown portable commands remain plain ambient executable delegates.
	plan = mustMCPDelegatePlan(t, validDelegateMCPServer(t, "python3", []string{"server.py"}, nil))
	if plan.Runner().Kind() != delegate.RunnerPlain || plan.PinPolicy() != delegate.PinNotApplicable {
		t.Fatalf("unknown command lowered to runner=%q pin=%q, want plain/not_applicable", plan.Runner().Kind(), plan.PinPolicy())
	}

	// Explore X2: docker latest tag is floating because daem cannot claim exact outcome identity.
	plan = mustMCPDelegatePlan(t, validDelegateMCPServer(t, "docker", []string{"run", "ghcr.io/acme/server:latest"}, nil))
	assertPackageRef(t, plan, delegate.EcosystemContainer, "ghcr.io/acme/server", "latest", delegate.PinFloating)

	// Explore X3: plain delegates preserve an empty argv element without shell interpretation.
	plan = mustMCPDelegatePlan(t, validDelegateMCPServer(t, "node", []string{"--label", ""}, nil))
	if args := plan.Command().Args(); len(args) != 2 || args[1] != "" {
		t.Fatalf("plain delegate args = %#v, want empty argument preserved", args)
	}
}
