package gitcli

func initializeBareRepositoryArgs(format gitObjectFormat, explicit bool) []string {
	args := []string{"init", "--bare", "--quiet"}
	if explicit {
		args = append(args, "--object-format="+string(format))
	}
	return args
}

func initializeBareDirectoryArgs(path string) []string {
	return []string{"init", "--bare", "--quiet", "--", path}
}

func inspectObjectFormatArgs() []string {
	return []string{"rev-parse", "--show-object-format"}
}

func localObjectFormatArgs(path string) []string {
	return []string{"-C", path, "rev-parse", "--show-object-format"}
}

func localHEADObjectIDArgs(path string) []string {
	return []string{"-C", path, "rev-parse", "--verify", "--end-of-options", "HEAD"}
}

func gitCommandInDirectoryArgs(directory string, args []string) []string {
	commandArgs := make([]string, 0, len(args)+2)
	commandArgs = append(commandArgs, "-C", directory)
	return append(commandArgs, args...)
}

func lsRemoteRefsArgs(locator string) []string {
	return []string{"ls-remote", "--refs", "--", locator}
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
