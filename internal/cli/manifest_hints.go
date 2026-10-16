package cli

import (
	clipresent "github.com/isty2e/daem/internal/cli/present"
	workflowhelp "github.com/isty2e/daem/internal/workflow/help"
)

func manifestHintOptions(verbose bool) clipresent.HumanOptions {
	return clipresent.HumanOptions{
		Verbose:              verbose,
		ImplicitManifestPath: workflowhelp.CWDManifestHintPath(),
	}
}

func creationHintOptions() clipresent.HumanOptions {
	return clipresent.HumanOptions{ImplicitManifestPath: workflowhelp.CreationManifestHintPath()}
}
