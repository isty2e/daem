package cli

import (
	"bytes"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func prepareLockResultJSON(result workflowlock.Result, dryRun bool) ([]byte, error) {
	mode := "write"
	if dryRun {
		mode = "dry-run"
	}
	var buffer bytes.Buffer
	if err := printLockComparisonJSON(&buffer, "lock", mode, result); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func printLockComparisonJSON(output io.Writer, command, mode string, result workflowlock.Result) error {
	return clipresent.PrintJSON(output, clipresent.JSONInput{
		Command:        command,
		Mode:           mode,
		ManifestPath:   result.ManifestPath,
		LockfilePath:   result.LockfilePath,
		PreviousStatus: string(result.PreviousStatus()),
		Lockfile:       result.Lockfile,
		Delta:          result.Comparison(),
	})
}
