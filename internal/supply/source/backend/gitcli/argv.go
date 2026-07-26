package gitcli

import "github.com/isty2e/daem/internal/supply/source"

func cloneArgs(gitSource source.GitSource, repoPath string) []string {
	args := []string{"clone", "--no-checkout", "--filter=blob:none"}
	if gitSource.Locator().IsNativeLocal() {
		args = append(args, "--no-local")
	}
	return append(args, "--", gitSource.Locator().String(), repoPath)
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
	return []string{"ls-tree", "-z", "-d", "--name-only", "--", objectName}
}

func archiveArgs(commit string, repositoryPath string) []string {
	return []string{"archive", "--format=tar", commit, "--", repositoryPath}
}
