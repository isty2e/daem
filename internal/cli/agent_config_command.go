package cli

import (
	"flag"
	"slices"
)

const (
	agentConfigSubject = "agent-config"
	// The JSON command identifier also uses the legacy subject.
	legacyAgentConfigSubject  = "host-config"
	agentConfigPathFlag       = "config-path"
	legacyAgentConfigPathFlag = "host-path"
)

func canonicalAgentConfigSubject(args []string) []string {
	if len(args) < 2 || args[1] != legacyAgentConfigSubject {
		return args
	}
	switch args[0] {
	case "add", "remove", "import", "unmanage", "replace":
		args = slices.Clone(args)
		args[1] = agentConfigSubject
	}
	return args
}

func registerAgentConfigPathFlag(flags *flag.FlagSet, path *string) {
	flags.StringVar(path, agentConfigPathFlag, "", "path relative to the selected agent's configuration root")
	flags.StringVar(path, legacyAgentConfigPathFlag, "", "alias for --config-path")
}
