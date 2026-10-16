package clipresent

// HumanOptions selects evidence depth and command context for human output.
type HumanOptions struct {
	Verbose bool

	// ManifestRequest preserves the original selector, including implicit empty.
	// Nil deliberately uses the resolved target as the explicit fallback.
	ManifestRequest *string

	// ImplicitManifestPath is the CLI-observed default for the suggested command.
	// An empty value retains the request rather than admitting elision.
	ImplicitManifestPath string
}

// MCPProbeHumanOptions selects probe evidence depth and whether a disclosed
// dry-run is immediately followed by interactive confirmation.
type MCPProbeHumanOptions struct {
	Verbose              bool
	AwaitingConfirmation bool
}
