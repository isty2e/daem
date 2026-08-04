package delegate

import (
	"slices"
	"testing"
)

func TestDelegatePlanDerivesCompleteRunnerPackageSet(t *testing.T) {
	tests := []struct {
		name       string
		runner     RunnerKind
		args       []string
		wantPolicy PinPolicy
		wantRefs   []packageInput
	}{
		{
			name:       "npx repeated package options",
			runner:     RunnerNPX,
			args:       []string{"--package=server@1.2.3", "--package", "helper@latest", "server"},
			wantPolicy: PinFloating,
			wantRefs: []packageInput{
				{ecosystem: EcosystemNPM, name: "helper", selector: "latest"},
				{ecosystem: EcosystemNPM, name: "server", selector: "1.2.3"},
			},
		},
		{
			name:       "npx options after positional belong to child command",
			runner:     RunnerNPX,
			args:       []string{"server@1.2.3", "--package=helper@latest"},
			wantPolicy: PinPinned,
			wantRefs: []packageInput{
				{ecosystem: EcosystemNPM, name: "server", selector: "1.2.3"},
			},
		},
		{
			name:       "npx call without explicit packages is opaque",
			runner:     RunnerNPX,
			args:       []string{"--call", "ambient-command --flag"},
			wantPolicy: PinFloating,
		},
		{
			name:       "npx call keeps explicit packages but remains opaque",
			runner:     RunnerNPX,
			args:       []string{"--package=server@1.2.3", "--call=server"},
			wantPolicy: PinFloating,
			wantRefs: []packageInput{
				{ecosystem: EcosystemNPM, name: "server", selector: "1.2.3"},
			},
		},
		{
			name:       "npx malformed selector remains opaque",
			runner:     RunnerNPX,
			args:       []string{"server@"},
			wantPolicy: PinFloating,
		},
		{
			name:       "uvx from and additional package",
			runner:     RunnerUVX,
			args:       []string{"--from", "server==1.2.3", "--with", "helper==2.0", "server"},
			wantPolicy: PinPinned,
			wantRefs: []packageInput{
				{ecosystem: EcosystemPython, name: "helper", selector: "2.0"},
				{ecosystem: EcosystemPython, name: "server", selector: "1.2.3"},
			},
		},
		{
			name:       "uvx extras requirement is opaque",
			runner:     RunnerUVX,
			args:       []string{"--from", "mypy[faster-cache,reports]==1.13.0", "mypy"},
			wantPolicy: PinFloating,
		},
		{
			name:       "uvx git requirement is opaque",
			runner:     RunnerUVX,
			args:       []string{"--from", "git+https://github.com/httpie/cli", "http"},
			wantPolicy: PinFloating,
		},
		{
			name:       "uvx opaque additive requirement preserves known primary",
			runner:     RunnerUVX,
			args:       []string{"--from", "server==1.2.3", "--with", "git+https://github.com/acme/helper", "server"},
			wantPolicy: PinFloating,
			wantRefs: []packageInput{
				{ecosystem: EcosystemPython, name: "server", selector: "1.2.3"},
			},
		},
		{
			name:       "uvx requirements file is opaque",
			runner:     RunnerUVX,
			args:       []string{"--with-requirements", "requirements.txt", "server@1.2.3"},
			wantPolicy: PinFloating,
			wantRefs: []packageInput{
				{ecosystem: EcosystemPython, name: "server", selector: "1.2.3"},
			},
		},
		{
			name:       "uvx option value is not a package",
			runner:     RunnerUVX,
			args:       []string{"--python", "3.13", "server@1.2.3"},
			wantPolicy: PinPinned,
			wantRefs: []packageInput{
				{ecosystem: EcosystemPython, name: "server", selector: "1.2.3"},
			},
		},
		{
			name:       "unknown uvx option degrades assurance",
			runner:     RunnerUVX,
			args:       []string{"--future-option", "value", "server@1.2.3"},
			wantPolicy: PinFloating,
		},
		{
			name:       "docker known option value is not image",
			runner:     RunnerDocker,
			args:       []string{"run", "--pull", "always", "ghcr.io/acme/server@sha256:" + testSHA256},
			wantPolicy: PinPinned,
			wantRefs: []packageInput{
				{ecosystem: EcosystemContainer, name: "ghcr.io/acme/server", selector: "sha256:" + testSHA256},
			},
		},
		{
			name:       "docker container run",
			runner:     RunnerDocker,
			args:       []string{"container", "run", "ghcr.io/acme/server@sha256:" + testSHA256},
			wantPolicy: PinPinned,
			wantRefs: []packageInput{
				{ecosystem: EcosystemContainer, name: "ghcr.io/acme/server", selector: "sha256:" + testSHA256},
			},
		},
		{
			name:   "docker bare boolean flag does not consume image",
			runner: RunnerDocker,
			args: []string{
				"run", "--sig-proxy", "ghcr.io/acme/server:latest",
				"helper@sha256:" + testSHA256,
			},
			wantPolicy: PinFloating,
			wantRefs: []packageInput{
				{ecosystem: EcosystemContainer, name: "ghcr.io/acme/server", selector: "latest"},
			},
		},
		{
			name:       "docker assigned boolean flag preserves image",
			runner:     RunnerDocker,
			args:       []string{"run", "--sig-proxy=false", "ghcr.io/acme/server@sha256:" + testSHA256},
			wantPolicy: PinPinned,
			wantRefs: []packageInput{
				{ecosystem: EcosystemContainer, name: "ghcr.io/acme/server", selector: "sha256:" + testSHA256},
			},
		},
		{
			name:       "docker attached empty option stays delegated",
			runner:     RunnerDocker,
			args:       []string{"run", "--entrypoint=", "ghcr.io/acme/server@sha256:" + testSHA256},
			wantPolicy: PinPinned,
			wantRefs: []packageInput{
				{ecosystem: EcosystemContainer, name: "ghcr.io/acme/server", selector: "sha256:" + testSHA256},
			},
		},
		{
			name:       "docker global context option",
			runner:     RunnerDocker,
			args:       []string{"--context", "remote", "run", "ghcr.io/acme/server@sha256:" + testSHA256},
			wantPolicy: PinPinned,
			wantRefs: []packageInput{
				{ecosystem: EcosystemContainer, name: "ghcr.io/acme/server", selector: "sha256:" + testSHA256},
			},
		},
		{
			name:       "unknown docker global option degrades assurance",
			runner:     RunnerDocker,
			args:       []string{"--future-global", "value", "run", "ghcr.io/acme/server@sha256:" + testSHA256},
			wantPolicy: PinFloating,
		},
		{
			name:       "docker unknown option degrades assurance",
			runner:     RunnerDocker,
			args:       []string{"run", "--future-option", "helper@sha256:" + testSHA256, "ghcr.io/acme/server:latest"},
			wantPolicy: PinFloating,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			commandName := string(test.runner)
			plan, err := NewDelegatePlan(DelegatePlanSpec{
				Runner:  mustRunner(t, test.runner),
				Command: mustCommand(t, commandName, test.args),
			})
			if err != nil {
				t.Fatalf("NewDelegatePlan returned error: %v", err)
			}
			if plan.PinPolicy() != test.wantPolicy {
				t.Fatalf("PinPolicy() = %q, want %q", plan.PinPolicy(), test.wantPolicy)
			}
			if got := plan.Command().Args(); !slices.Equal(got, test.args) {
				t.Fatalf("Command().Args() = %#v, want exact argv %#v", got, test.args)
			}
			refs := plan.PackageRefs()
			if len(refs) != len(test.wantRefs) {
				t.Fatalf("PackageRefs() = %#v, want %#v", refs, test.wantRefs)
			}
			for index, want := range test.wantRefs {
				if refs[index].Ecosystem() != want.ecosystem || refs[index].Name() != want.name || refs[index].Selector() != want.selector {
					t.Fatalf("PackageRefs()[%d] = %#v, want %#v", index, refs[index], want)
				}
			}
		})
	}
}

const testSHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestDelegatePlanRejectsMalformedRunnerArguments(t *testing.T) {
	tests := []struct {
		name   string
		runner RunnerKind
		args   []string
	}{
		{name: "npx package", runner: RunnerNPX, args: []string{"--package"}},
		{name: "npx empty call", runner: RunnerNPX, args: []string{"--call="}},
		{name: "uvx from", runner: RunnerUVX, args: []string{"--from"}},
		{name: "uvx from without command", runner: RunnerUVX, args: []string{"--from", "server==1.2.3"}},
		{name: "uvx with", runner: RunnerUVX, args: []string{"--with"}},
		{name: "uvx python", runner: RunnerUVX, args: []string{"--python"}},
		{name: "uvx command", runner: RunnerUVX, args: []string{"--with", "helper==1.2.3"}},
		{name: "docker subcommand", runner: RunnerDocker, args: []string{"pull", "ghcr.io/acme/server:latest"}},
		{name: "docker global option", runner: RunnerDocker, args: []string{"--context"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewDelegatePlan(DelegatePlanSpec{
				Runner:  mustRunner(t, test.runner),
				Command: mustCommand(t, string(test.runner), test.args),
			})
			if err == nil {
				t.Fatal("NewDelegatePlan accepted malformed runner arguments")
			}
		})
	}
}
