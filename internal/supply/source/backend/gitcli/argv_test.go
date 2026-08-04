package gitcli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGitArgvShapesKeepDataAfterOptionTerminators(t *testing.T) {
	t.Parallel()

	localPath, err := filepath.Abs("repo")
	if err != nil {
		t.Fatalf("Abs returned error: %v", err)
	}
	commit := strings.Repeat("a", 40)

	testCases := []struct {
		name string
		got  []string
		want []string
	}{
		{
			name: "bare repository initialization",
			got:  initializeBareRepositoryArgs(),
			want: []string{"init", "--bare", "--quiet"},
		},
		{
			name: "origin declaration",
			got:  addOriginArgs(localPath),
			want: []string{"remote", "add", "origin", localPath},
		},
		{
			name: "bare repository inspection",
			got:  inspectBareRepositoryArgs(),
			want: []string{"rev-parse", "--is-bare-repository"},
		},
		{
			name: "origin inspection",
			got:  inspectOriginArgs(),
			want: []string{"config", "--local", "--no-includes", "--get-all", "remote.origin.url"},
		},
		{
			name: "local config name inspection",
			got:  inspectLocalConfigNamesArgs(),
			want: []string{"config", "--local", "--no-includes", "--name-only", "--get-regexp", ".*"},
		},
		{
			name: "origin fetch inspection",
			got:  inspectOriginFetchArgs(),
			want: []string{"config", "--local", "--no-includes", "--get-all", "remote.origin.fetch"},
		},
		{
			name: "effective origin inspection",
			got:  inspectEffectiveOriginArgs(),
			want: []string{"remote", "get-url", "origin"},
		},
		{
			name: "advertised ref refresh",
			got:  refreshArgs(),
			want: []string{"fetch", "--tags", "--force", "--prune", "--prune-tags", "--", "origin"},
		},
		{
			name: "full commit fetch",
			got:  fetchCommitArgs(commit),
			want: []string{"fetch", "--force", "--", "origin", commit},
		},
		{
			name: "object verification",
			got:  verifyObjectArgs("refs/tags/v1^{commit}"),
			want: []string{"rev-parse", "--verify", "--end-of-options", "refs/tags/v1^{commit}"},
		},
		{
			name: "object inspection",
			got:  inspectObjectArgs(commit + ":skills/review"),
			want: []string{"cat-file", "-t", "--", commit + ":skills/review"},
		},
		{
			name: "tree listing",
			got:  listTreeArgs(commit + ":skills"),
			want: []string{"ls-tree", "-z", "--", commit + ":skills"},
		},
		{
			name: "tree archive",
			got:  archiveArgs(commit, "skills/review"),
			want: []string{"archive", "--format=tar", commit, "--", "skills/review"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if !reflect.DeepEqual(testCase.got, testCase.want) {
				t.Fatalf("argv = %#v, want %#v", testCase.got, testCase.want)
			}
		})
	}
}

func TestRepositoryGitCommandArgsFixRepositoryAndDisableCachePolicy(t *testing.T) {
	t.Parallel()

	got := repositoryGitCommandArgs([]string{"fetch", "--", "origin"})
	want := []string{
		"--no-replace-objects",
		"--git-dir=.",
		"-c",
		"core.hooksPath=" + os.DevNull,
		"fetch",
		"--",
		"origin",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("repositoryGitCommandArgs() = %#v, want %#v", got, want)
	}
}

func TestRepositoryGitCommandEnvironmentDropsRepositorySelectors(t *testing.T) {
	t.Parallel()

	got := repositoryGitCommandEnvironment([]string{
		"HOME=/home/operator",
		"GIT_DIR=/attacker",
		"GIT_COMMON_DIR=/attacker",
		"GIT_OBJECT_DIRECTORY=/attacker/objects",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES=/attacker/alternate",
		"GIT_IMPLICIT_WORK_TREE=1",
		"GIT_WORK_TREE=/attacker",
		"GIT_SSH_COMMAND=ssh -i /operator/key",
	})
	want := []string{
		"HOME=/home/operator",
		"GIT_SSH_COMMAND=ssh -i /operator/key",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("repositoryGitCommandEnvironment() = %#v, want %#v", got, want)
	}
}
