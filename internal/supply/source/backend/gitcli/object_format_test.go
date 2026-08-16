package gitcli

import (
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
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, found, err := gitObjectFormatFromAdvertisedIDs(testCase.output)
			if testCase.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
					t.Fatalf("error = %v, want %q", err, testCase.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("gitObjectFormatFromAdvertisedIDs returned error: %v", err)
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
