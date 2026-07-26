package source

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewGitSourceCanonicalizesAcceptedForms(t *testing.T) {
	t.Parallel()

	localRepo, err := filepath.Abs("repo with space")
	if err != nil {
		t.Fatalf("Abs returned error: %v", err)
	}
	fileLocator := (&url.URL{Scheme: "file", Path: filepath.ToSlash(localRepo)}).String()
	testCases := []struct {
		name          string
		locator       string
		path          string
		ref           string
		wantLocator   string
		wantPath      string
		wantRef       string
		wantCanonical string
		wantLocal     bool
	}{
		{
			name:          "https branch name",
			locator:       "https://github.com/acme/skills.git",
			path:          "skills/review",
			ref:           "main",
			wantLocator:   "https://github.com/acme/skills.git",
			wantPath:      "skills/review",
			wantRef:       "main",
			wantCanonical: "name:main",
		},
		{
			name:          "ssh username and qualified tag",
			locator:       "ssh://git@example.com/acme/skills.git",
			path:          ".",
			ref:           "refs/tags/v1.2.0",
			wantLocator:   "ssh://git@example.com/acme/skills.git",
			wantPath:      ".",
			wantRef:       "refs/tags/v1.2.0",
			wantCanonical: "tag:v1.2.0",
		},
		{
			name:          "scp like and qualified branch",
			locator:       "git@example.com:acme/skills.git",
			path:          "skills/review",
			ref:           "refs/heads/release/v2",
			wantLocator:   "git@example.com:acme/skills.git",
			wantPath:      "skills/review",
			wantRef:       "refs/heads/release/v2",
			wantCanonical: "branch:release/v2",
		},
		{
			name:          "file url and sha1",
			locator:       fileLocator,
			path:          "skill with space",
			ref:           strings.Repeat("A", 40),
			wantLocator:   fileLocator,
			wantPath:      "skill with space",
			wantRef:       strings.Repeat("a", 40),
			wantCanonical: "commit:" + strings.Repeat("a", 40),
		},
		{
			name:          "native absolute and sha256",
			locator:       localRepo + string(filepath.Separator) + ".",
			path:          ".",
			ref:           strings.Repeat("b", 64),
			wantLocator:   filepath.Clean(localRepo),
			wantPath:      ".",
			wantRef:       strings.Repeat("b", 64),
			wantCanonical: "commit:" + strings.Repeat("b", 64),
			wantLocal:     true,
		},
		{
			name:          "unicode symbolic name",
			locator:       "https://example.com/acme/skills.git",
			path:          "skills/리뷰",
			ref:           "feature/한글",
			wantLocator:   "https://example.com/acme/skills.git",
			wantPath:      "skills/리뷰",
			wantRef:       "feature/한글",
			wantCanonical: "name:feature/한글",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			sourceSpec, err := NewGitSource(testCase.locator, testCase.path, testCase.ref)
			if err != nil {
				t.Fatalf("NewGitSource returned error: %v", err)
			}

			gitSource, ok := sourceSpec.Git()
			if !ok {
				t.Fatal("Git returned false")
			}
			if gitSource.Locator().String() != testCase.wantLocator {
				t.Fatalf("locator = %q, want %q", gitSource.Locator(), testCase.wantLocator)
			}
			if gitSource.RepositoryPath().String() != testCase.wantPath {
				t.Fatalf("path = %q, want %q", gitSource.RepositoryPath(), testCase.wantPath)
			}
			if gitSource.Ref().String() != testCase.wantRef {
				t.Fatalf("ref = %q, want %q", gitSource.Ref(), testCase.wantRef)
			}
			if gitSource.Ref().Canonical() != testCase.wantCanonical {
				t.Fatalf("canonical ref = %q, want %q", gitSource.Ref().Canonical(), testCase.wantCanonical)
			}
			if gitSource.Locator().IsNativeLocal() != testCase.wantLocal {
				t.Fatalf("native local = %t, want %t", gitSource.Locator().IsNativeLocal(), testCase.wantLocal)
			}
			localPath, hasLocalPath := gitSource.Locator().LocalPath()
			if testCase.wantLocal && (!hasLocalPath || localPath != filepath.Clean(testCase.locator)) {
				t.Fatalf("local path = %q/%t, want %q", localPath, hasLocalPath, filepath.Clean(testCase.locator))
			}
		})
	}
}

func TestNewGitSourceRejectsInvalidBoundaryValuesWithoutEchoingThem(t *testing.T) {
	t.Parallel()

	secret := "synthetic-secret-value"
	testCases := []struct {
		name      string
		locator   string
		path      string
		ref       string
		wantClass string
	}{
		{name: "empty locator", locator: "", path: ".", ref: "main", wantClass: "locator is required"},
		{name: "trimmed locator", locator: " https://example.com/repo.git", path: ".", ref: "main", wantClass: "locator has surrounding whitespace"},
		{name: "http transport", locator: "http://example.com/repo.git", path: ".", ref: "main", wantClass: "locator scheme is unsupported"},
		{name: "git transport", locator: "git://example.com/repo.git", path: ".", ref: "main", wantClass: "locator scheme is unsupported"},
		{name: "ftp transport", locator: "ftp://example.com/repo.git", path: ".", ref: "main", wantClass: "locator scheme is unsupported"},
		{name: "remote helper", locator: "ext::sh -c touch-owned", path: ".", ref: "main", wantClass: "locator form is unsupported"},
		{name: "relative repository", locator: "../repo", path: ".", ref: "main", wantClass: "locator must be an admitted URL, scp-like SSH address, or absolute path"},
		{name: "tilde repository", locator: "~/repo", path: ".", ref: "main", wantClass: "locator must be an admitted URL, scp-like SSH address, or absolute path"},
		{name: "https username", locator: "https://user@example.com/repo.git", path: ".", ref: "main", wantClass: "HTTPS locator must not contain userinfo"},
		{name: "https password", locator: "https://user:" + secret + "@example.com/repo.git", path: ".", ref: "main", wantClass: "HTTPS locator must not contain userinfo"},
		{name: "ssh password", locator: "ssh://git:" + secret + "@example.com/repo.git", path: ".", ref: "main", wantClass: "SSH locator must not contain a password"},
		{name: "query", locator: "https://example.com/repo.git?token=" + secret, path: ".", ref: "main", wantClass: "locator must not contain query or fragment fields"},
		{name: "fragment", locator: "https://example.com/repo.git#" + secret, path: ".", ref: "main", wantClass: "locator must not contain query or fragment fields"},
		{name: "empty fragment", locator: "https://example.com/repo.git#", path: ".", ref: "main", wantClass: "locator must not contain query or fragment fields"},
		{name: "malformed escape", locator: "https://example.com/repo%zz.git", path: ".", ref: "main", wantClass: "locator is malformed"},
		{name: "file host", locator: "file://example.com/repo.git", path: ".", ref: "main", wantClass: "file locator must not contain host or userinfo"},
		{name: "file URL with relative-looking host", locator: "file://repo.git", path: ".", ref: "main", wantClass: "file locator must not contain host or userinfo"},
		{name: "scp empty username", locator: "@example.com:repo.git", path: ".", ref: "main", wantClass: "locator must be an admitted URL, scp-like SSH address, or absolute path"},
		{name: "scp empty path", locator: "git@example.com:", path: ".", ref: "main", wantClass: "locator must be an admitted URL, scp-like SSH address, or absolute path"},
		{name: "encoded control", locator: "https://example.com/repo%0a.git", path: ".", ref: "main", wantClass: "locator contains a control or format character"},
		{name: "unclean path", locator: "https://example.com/repo.git", path: "skills/../review", ref: "main", wantClass: "repository path must already be clean"},
		{name: "absolute repository path", locator: "https://example.com/repo.git", path: "/skills/review", ref: "main", wantClass: "repository path must be relative"},
		{name: "backslash repository path", locator: "https://example.com/repo.git", path: `skills\review`, ref: "main", wantClass: "repository path must use POSIX separators"},
		{name: "path control", locator: "https://example.com/repo.git", path: "skills/\x00review", ref: "main", wantClass: "repository path contains a control or format character"},
		{name: "leading option ref", locator: "https://example.com/repo.git", path: ".", ref: "--upload-pack=" + secret, wantClass: "git ref must not begin with an option prefix"},
		{name: "abbreviated sha", locator: "https://example.com/repo.git", path: ".", ref: "deadbee", wantClass: "abbreviated object ids are unsupported"},
		{name: "revision expression", locator: "https://example.com/repo.git", path: ".", ref: "main~1", wantClass: "git ref contains forbidden revision or refspec syntax"},
		{name: "ref whitespace", locator: "https://example.com/repo.git", path: ".", ref: "feature branch", wantClass: "git ref has an invalid path component"},
		{name: "ref empty component", locator: "https://example.com/repo.git", path: ".", ref: "feature//branch", wantClass: "git ref has an invalid path component"},
		{name: "ref hidden component", locator: "https://example.com/repo.git", path: ".", ref: "feature/.hidden", wantClass: "git ref has an invalid path component"},
		{name: "ref lock suffix", locator: "https://example.com/repo.git", path: ".", ref: "feature/name.lock", wantClass: "git ref has an invalid path component"},
		{name: "ref trailing dot", locator: "https://example.com/repo.git", path: ".", ref: "feature.", wantClass: "git ref has an invalid path component"},
		{name: "refspec", locator: "https://example.com/repo.git", path: ".", ref: "refs/heads/main:refs/heads/owned", wantClass: "git ref contains forbidden revision or refspec syntax"},
		{name: "pseudo ref", locator: "https://example.com/repo.git", path: ".", ref: "HEAD", wantClass: "git pseudo-ref is unsupported"},
		{name: "unsupported refs namespace", locator: "https://example.com/repo.git", path: ".", ref: "refs/pull/1/head", wantClass: "git ref namespace is unsupported"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewGitSource(testCase.locator, testCase.path, testCase.ref)
			if err == nil || !strings.Contains(err.Error(), testCase.wantClass) {
				t.Fatalf("error = %v, want class %q", err, testCase.wantClass)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("error disclosed secret: %v", err)
			}
		})
	}
}

func TestGitSourceIDUsesEscapedCanonicalTuple(t *testing.T) {
	t.Parallel()

	sourceSpec, err := NewGitSource(
		"https://example.com/acme/repo.git",
		"skills/review & audit",
		"refs/tags/release@v1",
	)
	if err != nil {
		t.Fatalf("NewGitSource returned error: %v", err)
	}

	sourceID, err := SourceIDFor(sourceSpec)
	if err != nil {
		t.Fatalf("SourceIDFor returned error: %v", err)
	}

	want := "git:locator=https%3A%2F%2Fexample.com%2Facme%2Frepo.git&path=skills%2Freview+%26+audit&ref=tag%3Arelease%40v1"
	if string(sourceID) != want {
		t.Fatalf("source id = %q, want %q", sourceID, want)
	}
}

func TestGitSourceRefResolutionCandidates(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		ref            string
		wantCommit     bool
		wantCandidates []string
	}{
		{ref: "main", wantCandidates: []string{"refs/remotes/origin/main^{commit}", "refs/tags/main^{commit}"}},
		{ref: "refs/heads/main", wantCandidates: []string{"refs/remotes/origin/main^{commit}"}},
		{ref: "refs/tags/v1", wantCandidates: []string{"refs/tags/v1^{commit}"}},
		{ref: strings.Repeat("a", 40), wantCommit: true, wantCandidates: []string{strings.Repeat("a", 40) + "^{commit}"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.ref, func(t *testing.T) {
			sourceSpec, err := NewGitSource("https://example.com/repo.git", ".", testCase.ref)
			if err != nil {
				t.Fatalf("NewGitSource returned error: %v", err)
			}
			gitSource, _ := sourceSpec.Git()
			if gitSource.Ref().IsCommit() != testCase.wantCommit {
				t.Fatalf("is commit = %t, want %t", gitSource.Ref().IsCommit(), testCase.wantCommit)
			}
			if got := gitSource.Ref().ResolutionCandidates(); strings.Join(got, "\x00") != strings.Join(testCase.wantCandidates, "\x00") {
				t.Fatalf("candidates = %#v, want %#v", got, testCase.wantCandidates)
			}
		})
	}
}
