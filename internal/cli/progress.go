package cli

import (
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	cliprogress "github.com/isty2e/daem/internal/cli/present/progress"
)

func newLockProgressRenderer(jsonOutput bool, stderr io.Writer, options commandOptions) *cliprogress.LockProgressRenderer {
	if jsonOutput || !options.stderrIsTerminal || stderr == nil {
		return nil
	}

	return cliprogress.NewLockProgressRenderer(cliprogress.LockProgressRendererOptions{Output: stderr}, clipresent.ResourceReference)
}

func newImportProgressRenderer(jsonOutput bool, stderr io.Writer, options commandOptions) *cliprogress.ImportProgressRenderer {
	if jsonOutput || !options.stderrIsTerminal || stderr == nil {
		return nil
	}

	return cliprogress.NewImportProgressRenderer(stderr)
}

func newApplyProgressRenderer(jsonOutput bool, stderr io.Writer, options commandOptions) *cliprogress.ApplyProgressRenderer {
	if jsonOutput || !options.stderrIsTerminal || stderr == nil {
		return nil
	}

	return cliprogress.NewApplyProgressRenderer(cliprogress.ApplyProgressRendererOptions{Output: stderr}, clipresent.ResourceReference)
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
