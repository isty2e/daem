package cli

import (
	"io"

	cliprogress "github.com/isty2e/daem/internal/cli/present/progress"
)

func newLockProgressRenderer(jsonOutput bool, stderr io.Writer, options commandOptions) *cliprogress.LockProgressRenderer {
	if jsonOutput || !options.stderrIsTerminal || stderr == nil {
		return nil
	}

	return cliprogress.NewLockProgressRenderer(cliprogress.LockProgressRendererOptions{Output: stderr})
}

func newApplyProgressRenderer(jsonOutput bool, stderr io.Writer, options commandOptions) *cliprogress.ApplyProgressRenderer {
	if jsonOutput || !options.stderrIsTerminal || stderr == nil {
		return nil
	}

	return cliprogress.NewApplyProgressRenderer(cliprogress.ApplyProgressRendererOptions{Output: stderr})
}

func newRefreshProgressRenderer(
	jsonOutput bool,
	stderr io.Writer,
	options commandOptions,
) *cliprogress.RefreshProgressRenderer {
	if jsonOutput || !options.stderrIsTerminal || stderr == nil {
		return nil
	}

	return cliprogress.NewRefreshProgressRenderer(stderr)
}
