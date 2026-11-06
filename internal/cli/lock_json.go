package cli

import (
	"bytes"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func prepareLockResultJSON(result workflowlock.Result, dryRun bool) ([]byte, error) {
	mode := "write"
	if dryRun {
		mode = "dry-run"
	}
	var buffer bytes.Buffer
	if err := clipresent.PrintJSON(&buffer, clipresent.JSONInput{
		Command: "lock", Mode: mode,
		ManifestPath: result.ManifestPath, LockfilePath: result.LockfilePath,
		PreviousFound: result.PreviousFound, Lockfile: result.Lockfile, Delta: result.Delta,
	}); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
