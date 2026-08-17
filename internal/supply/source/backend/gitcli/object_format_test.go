package gitcli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitObjectFormatFromAdvertisedIDs(t *testing.T) {
	t.Parallel()

	sha1 := strings.Repeat("a", 40)
	sha256 := strings.Repeat("b", 64)

	testCases := []struct {
		name      string
		output    string
		budget    *remoteRefAdvertisementBudget
		want      gitObjectFormat
		wantFound bool
		wantErr   string
	}{
		{
			name:      "empty",
			output:    "",
			wantFound: false,
		},
		{
			name:      "sha1 refs",
			output:    sha1 + "\tHEAD\n" + sha1 + "\trefs/heads/main\n",
			want:      gitObjectFormatSHA1,
			wantFound: true,
		},
		{
			name:      "sha256 refs including peeled tag",
			output:    sha256 + "\tHEAD\n" + strings.Repeat("c", 64) + "\trefs/tags/v1\n" + sha256 + "\trefs/tags/v1^{}\n",
			want:      gitObjectFormatSHA256,
			wantFound: true,
		},
		{
			name:    "mixed widths",
			output:  sha1 + "\trefs/heads/main\n" + sha256 + "\trefs/heads/other\n",
			wantErr: "mixed object-id widths",
		},
		{
			name:    "malformed line",
			output:  "not-a-record\n",
			wantErr: "malformed",
		},
		{
			name:    "unsupported width",
			output:  strings.Repeat("a", 32) + "\trefs/heads/main\n",
			wantErr: "unsupported object id",
		},
		{
			name: "record ceiling",
			output: sha1 + "\trefs/heads/one\n" +
				sha1 + "\trefs/heads/two\n" +
				sha1 + "\trefs/heads/three\n",
			budget: &remoteRefAdvertisementBudget{
				maxBytes:     defaultRemoteRefAdvertisementBytes,
				maxRecords:   2,
				maxLineBytes: defaultRemoteRefAdvertisementLine,
			},
			wantErr: "exceeds 2 records",
		},
		{
			name:   "byte ceiling",
			output: sha1 + "\trefs/heads/main\n",
			budget: &remoteRefAdvertisementBudget{
				maxBytes:     8,
				maxRecords:   defaultRemoteRefAdvertisementRecords,
				maxLineBytes: defaultRemoteRefAdvertisementLine,
			},
			wantErr: "exceeds 8 bytes",
		},
		{
			name:   "overlong record",
			output: sha1 + "\t" + strings.Repeat("x", 64) + "\n",
			budget: &remoteRefAdvertisementBudget{
				maxBytes:     defaultRemoteRefAdvertisementBytes,
				maxRecords:   defaultRemoteRefAdvertisementRecords,
				maxLineBytes: 16,
			},
			wantErr: "exceeds 16 bytes",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			budget := defaultRemoteRefAdvertisementBudget()
			if testCase.budget != nil {
				budget = *testCase.budget
			}
			got, found, err := observeAdvertisedObjectFormat(strings.NewReader(testCase.output), budget)
			if testCase.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
					t.Fatalf("error = %v, want %q", err, testCase.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("observeAdvertisedObjectFormat returned error: %v", err)
			}
			if found != testCase.wantFound || got != testCase.want {
				t.Fatalf("format = %q/%t, want %q/%t", got, found, testCase.want, testCase.wantFound)
			}
		})
	}
}

func TestRepositoryCacheDirectoryNameKeepsSHA1LocatorIdentity(t *testing.T) {
	t.Parallel()

	locator := "https://example.com/acme/skills.git"
	if got, want := repositoryCacheDirectoryName(locator, gitObjectFormatSHA1), cacheKey(locator); got != want {
		t.Fatalf("sha1 cache name = %q, want locator-only %q", got, want)
	}
	if got := repositoryCacheDirectoryName(locator, gitObjectFormatSHA256); got == cacheKey(locator) {
		t.Fatalf("sha256 cache name = %q, want distinct from locator-only identity", got)
	}
}

func TestGitHelpSupportsExplicitObjectFormat(t *testing.T) {
	t.Parallel()

	if !gitHelpSupportsExplicitObjectFormat("usage: git init [--object-format=<format>]") {
		t.Fatal("capable git help was not recognized")
	}
	if gitHelpSupportsExplicitObjectFormat("usage: git init") {
		t.Fatal("legacy git help was treated as capable")
	}
}

func TestGitDirectoryOwnsLocalPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	nested := filepath.Join(repo, "nested")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if !gitDirectoryOwnsLocalPath(repo, repo) {
		t.Fatal("bare git directory was not recognized")
	}
	if !gitDirectoryOwnsLocalPath(filepath.Join(repo, ".git"), repo) {
		t.Fatal("worktree git directory was not recognized")
	}
	if gitDirectoryOwnsLocalPath(filepath.Join(repo, ".git"), nested) {
		t.Fatal("enclosing git directory was treated as the nested locator")
	}
}
