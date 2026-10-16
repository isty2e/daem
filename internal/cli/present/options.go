package clipresent

// HumanOptions selects evidence depth and command context for human output.
type HumanOptions struct {
	Verbose bool

	// ImplicitManifestPath is the CLI-observed default for the suggested command.
	// An empty value retains an explicit manifest argument.
	ImplicitManifestPath string
}

// MCPProbeHumanOptions selects probe evidence depth and whether a disclosed
// dry-run is immediately followed by interactive confirmation.
type MCPProbeHumanOptions struct {
	Verbose              bool
	AwaitingConfirmation bool
}
