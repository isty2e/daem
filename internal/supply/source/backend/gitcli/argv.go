package gitcli

func initializeBareRepositoryArgs(format gitObjectFormat, explicit bool) []string {
	if explicit {
		return []string{"init", "--bare", "--quiet", "--object-format=" + string(format)}
	}
	return []string{"-c", "init.defaultObjectFormat=sha1", "init", "--bare", "--quiet"}
}

func inspectGitInitHelpArgs() []string {
	return []string{"init", "-h"}
}

func inspectObjectFormatArgs() []string {
	return []string{"rev-parse", "--show-object-format"}
}

func localObjectFormatArgs(path string) []string {
	return []string{"-C", path, "rev-parse", "--show-object-format"}
}

func localObjectFormatConfigArgs(path string) []string {
	return []string{"-C", path, "config", "--local", "--no-includes", "--get", "extensions.objectformat"}
}

func inspectObjectFormatConfigArgs() []string {
	return []string{"config", "--local", "--no-includes", "--get", "extensions.objectformat"}
}

func localGitDirectoryArgs(path string) []string {
	return []string{"-C", path, "rev-parse", "--absolute-git-dir"}
}

func localObjectIDArgs(path string, objectName string) []string {
	return []string{"-C", path, "rev-parse", "--verify", "--end-of-options", objectName}
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
