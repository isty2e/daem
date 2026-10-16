package cli

import (
	clipresent "github.com/isty2e/daem/internal/cli/present"
	workflowhelp "github.com/isty2e/daem/internal/workflow/help"
)

func manifestHintOptions(requestedManifestPath string, verbose bool) clipresent.HumanOptions {
	return clipresent.HumanOptions{
		Verbose:              verbose,
		ManifestRequest:      &requestedManifestPath,
		ImplicitManifestPath: workflowhelp.ImplicitManifestHintPath(requestedManifestPath),
	}
}

func creationHintOptions() clipresent.HumanOptions {
	return clipresent.HumanOptions{ImplicitManifestPath: workflowhelp.CreationManifestHintPath()}
}
