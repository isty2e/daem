package gitcli

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGitArgvShapesKeepDataAfterOptionTerminators(t *testing.T) {
	t.Parallel()

	networkSource, ok := mustGitSource(t, "https://example.com/acme/repo.git", ".", "main").Git()
	if !ok {
		t.Fatal("network source is not git")
	}
	localPath, err := filepath.Abs("repo")
	if err != nil {
		t.Fatalf("Abs returned error: %v", err)
	}
	localSource, ok := mustGitSource(t, localPath, ".", "main").Git()
	if !ok {
		t.Fatal("local source is not git")
	}
	commit := strings.Repeat("a", 40)

	testCases := []struct {
		name string
		got  []string
		want []string
	}{
		{
			name: "network clone",
			got:  cloneArgs(networkSource, "/cache/repo"),
			want: []string{"clone", "--no-checkout", "--filter=blob:none", "--", "https://example.com/acme/repo.git", "/cache/repo"},
		},
		{
			name: "native local clone",
			got:  cloneArgs(localSource, "/cache/repo"),
			want: []string{"clone", "--no-checkout", "--filter=blob:none", "--no-local", "--", localSource.Locator().String(), "/cache/repo"},
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
			want: []string{"ls-tree", "-z", "-d", "--name-only", "--", commit + ":skills"},
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
