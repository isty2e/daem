package host

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/subprocess"
)

// ObservePiVersion isolates Pi's bootstrap settings access while observing only its bounded version command.
func ObservePiVersion(ctx context.Context) (profile.PiMCPVersion, error) {
	if ctx == nil {
		return profile.PiMCPVersion{}, fmt.Errorf("Pi version observation requires a context")
	}
	if err := ctx.Err(); err != nil {
		return profile.PiMCPVersion{}, err
	}
	command, err := exec.LookPath("pi")
	if err != nil {
		return profile.PiMCPVersion{}, nil
	}
	command, err = filepath.Abs(command)
	if err != nil {
		return profile.PiMCPVersion{}, nil
	}
	root, err := os.MkdirTemp("", "daem-pi-version-")
	if err != nil {
		return profile.PiMCPVersion{}, nil
	}
	defer os.RemoveAll(root)

	result := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Timeout: 3 * time.Second, OutputLimit: 128,
		LookupEnv: func(name string) (string, bool) {
			if name == "PI_CODING_AGENT_DIR" {
				return root, true
			}
			return os.LookupEnv(name)
		},
	}).Execute(ctx, subprocess.CommandAttemptRequest{
		Command: command, Args: []string{"--version"}, WorkDir: root,
		EnvRefs: []subprocess.CommandEnvRef{{Name: "PI_CODING_AGENT_DIR", SourceName: "PI_CODING_AGENT_DIR"}},
	})
	if err := ctx.Err(); err != nil {
		return profile.PiMCPVersion{}, err
	}
	if !result.Succeeded() || result.StdoutTruncated() || !result.Started() {
		return profile.PiMCPVersion{}, nil
	}
	return profile.ObservePiMCPVersion(result.Stdout()), nil
}
