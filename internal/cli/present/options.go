package clipresent

// HumanOptions selects evidence depth for human-readable command output.
type HumanOptions struct {
	Verbose bool
}

// MCPProbeHumanOptions selects probe evidence depth and whether a disclosed
// dry-run is immediately followed by interactive confirmation.
type MCPProbeHumanOptions struct {
	Verbose              bool
	AwaitingConfirmation bool
}
