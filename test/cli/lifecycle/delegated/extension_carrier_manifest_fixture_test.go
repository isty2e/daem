package cli_test

func futureExtensionManifest(carrier string) string {
	return `
version = 1
targets = ["claude-code"]

[[extension]]
id = "alpha"
carrier = "` + carrier + `"
source = { marketplace = "alpha@market" }
`
}

func codexProjectExtensionManifest() string {
	return `
version = 1
targets = ["codex"]

[[extension]]
id = "documents"
carrier = "codex-plugin"
scope = "project"
source = { marketplace = "documents@openai-primary-runtime" }
`
}

func codexDefaultGlobalExtensionManifest() string {
	return `
version = 1
targets = ["codex"]

[defaults]
scope = "global"

[[extension]]
id = "documents"
carrier = "codex-plugin"
source = { marketplace = "documents@openai-primary-runtime" }
`
}

func claudeExtensionManifest() string {
	return `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7-managed"
carrier = "claude-code-plugin"
source = { marketplace = "context7@market" }
`
}

func claudeGlobalExtensionManifest() string {
	return `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7-global"
carrier = "claude-code-plugin"
scope = "global"
source = { marketplace = "context7@market" }
`
}

func claudeProjectAndGlobalSamePluginManifest() string {
	return `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7-managed"
carrier = "claude-code-plugin"
scope = "project"
source = { marketplace = "context7@market" }

[[extension]]
id = "context7-global"
carrier = "claude-code-plugin"
scope = "global"
source = { marketplace = "context7@market" }
`
}
