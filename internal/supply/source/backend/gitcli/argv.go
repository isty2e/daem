package gitcli

func initializeBareRepositoryArgs() []string {
	return []string{"init", "--bare", "--quiet"}
}

func addOriginArgs(locator string) []string {
	return []string{"remote", "add", "origin", locator}
}

func inspectBareRepositoryArgs() []string {
	return []string{"rev-parse", "--is-bare-repository"}
}

func inspectOriginArgs() []string {
	return []string{"config", "--local", "--no-includes", "--get-all", "remote.origin.url"}
}

func inspectLocalConfigNamesArgs() []string {
	return []string{"config", "--local", "--no-includes", "--name-only", "--get-regexp", ".*"}
}

func inspectOriginFetchArgs() []string {
	return []string{"config", "--local", "--no-includes", "--get-all", "remote.origin.fetch"}
}

func inspectEffectiveOriginArgs() []string {
	return []string{"remote", "get-url", "origin"}
}

func refreshArgs() []string {
	return []string{"fetch", "--tags", "--force", "--prune", "--prune-tags", "--", "origin"}
}

func fetchCommitArgs(objectID string) []string {
	return []string{"fetch", "--force", "--", "origin", objectID}
}

func verifyObjectArgs(objectName string) []string {
	return []string{"rev-parse", "--verify", "--end-of-options", objectName}
}

func inspectObjectArgs(objectName string) []string {
	return []string{"cat-file", "-t", "--", objectName}
}

func listTreeArgs(objectName string) []string {
	return []string{"ls-tree", "-z", "--", objectName}
}

func archiveArgs(commit string, repositoryPath string) []string {
	return []string{"archive", "--format=tar", commit, "--", repositoryPath}
}
