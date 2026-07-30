package source

import (
	"fmt"
	"net/url"
	pathpkg "path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/supply/artifact"
)

type gitLocatorKind string

const (
	gitLocatorKindHTTPS       gitLocatorKind = "https"
	gitLocatorKindSSH         gitLocatorKind = "ssh"
	gitLocatorKindSCPLike     gitLocatorKind = "scp-like"
	gitLocatorKindFile        gitLocatorKind = "file"
	gitLocatorKindNativeLocal gitLocatorKind = "native-local"
)

type gitRefKind string

const (
	gitRefKindName   gitRefKind = "name"
	gitRefKindBranch gitRefKind = "branch"
	gitRefKindTag    gitRefKind = "tag"
	gitRefKindCommit gitRefKind = "commit"
)

// GitLocator is a validated credential-free Git repository address.
type GitLocator struct {
	value     string
	kind      gitLocatorKind
	localPath string
}

// String returns the canonical locator text.
func (locator GitLocator) String() string {
	return locator.value
}

// IsNativeLocal reports whether the locator names an OS-native absolute path.
func (locator GitLocator) IsNativeLocal() bool {
	return locator.kind == gitLocatorKindNativeLocal
}

// LocalPath returns the filesystem path for native and file URL locators.
func (locator GitLocator) LocalPath() (string, bool) {
	if locator.localPath == "" {
		return "", false
	}
	return locator.localPath, true
}

// Equivalent reports whether two canonical locators identify the same
// repository address. Native paths and file URLs share their canonical local
// path identity; network forms retain exact canonical spelling.
func (locator GitLocator) Equivalent(other GitLocator) bool {
	if locator.localPath != "" || other.localPath != "" {
		return locator.localPath != "" &&
			other.localPath != "" &&
			locator.localPath == other.localPath
	}
	return locator.value != "" && locator.value == other.value
}

// GitRepositoryPath is a validated repository-relative POSIX tree path.
type GitRepositoryPath struct {
	value string
}

// String returns the canonical repository path.
func (path GitRepositoryPath) String() string {
	return path.value
}

// GitRefSelector is a validated single branch, tag, symbolic name, or commit selector.
type GitRefSelector struct {
	kind  gitRefKind
	value string
}

// String returns the admitted declaration spelling.
func (selector GitRefSelector) String() string {
	switch selector.kind {
	case gitRefKindBranch:
		return "refs/heads/" + selector.value
	case gitRefKindTag:
		return "refs/tags/" + selector.value
	default:
		return selector.value
	}
}

// IsCommit reports whether the selector is a full immutable object id.
func (selector GitRefSelector) IsCommit() bool {
	return selector.kind == gitRefKindCommit
}

// Canonical returns the source-identity selector component.
func (selector GitRefSelector) Canonical() string {
	return string(selector.kind) + ":" + selector.value
}

// ResolutionCandidates returns inert object expressions derived from the selector.
func (selector GitRefSelector) ResolutionCandidates() []string {
	switch selector.kind {
	case gitRefKindName:
		return []string{
			"refs/remotes/origin/" + selector.value + "^{commit}",
			"refs/tags/" + selector.value + "^{commit}",
		}
	case gitRefKindBranch:
		return []string{"refs/remotes/origin/" + selector.value + "^{commit}"}
	case gitRefKindTag:
		return []string{"refs/tags/" + selector.value + "^{commit}"}
	case gitRefKindCommit:
		return []string{selector.value + "^{commit}"}
	default:
		return nil
	}
}

// GitSource is canonical provenance resolved through Git.
type GitSource struct {
	locator        GitLocator
	repositoryPath GitRepositoryPath
	ref            GitRefSelector
}

// Locator returns the validated repository address.
func (source GitSource) Locator() GitLocator {
	return source.locator
}

// RepositoryPath returns the validated path exported from the repository tree.
func (source GitSource) RepositoryPath() GitRepositoryPath {
	return source.repositoryPath
}

// Ref returns the validated declaration selector.
func (source GitSource) Ref() GitRefSelector {
	return source.ref
}

// SourceID returns the canonical lockfile identity for this Git declaration.
func (source GitSource) SourceID() artifact.SourceID {
	return artifact.SourceID(
		"git:locator=" + url.QueryEscape(source.locator.String()) +
			"&path=" + url.QueryEscape(source.repositoryPath.String()) +
			"&ref=" + url.QueryEscape(source.ref.Canonical()),
	)
}

// NewGitSource validates and constructs canonical Git provenance.
func NewGitSource(locatorValue string, repositoryPathValue string, refValue string) (Source, error) {
	locator, err := ParseGitLocator(locatorValue)
	if err != nil {
		return Source{}, err
	}

	repositoryPath, err := parseGitRepositoryPath(repositoryPathValue)
	if err != nil {
		return Source{}, err
	}

	ref, err := parseGitRefSelector(refValue)
	if err != nil {
		return Source{}, err
	}

	return Source{
		kind: SourceKindGit,
		git: GitSource{
			locator:        locator,
			repositoryPath: repositoryPath,
			ref:            ref,
		},
	}, nil
}

// ParseGitLocator validates a credential-free Git repository address.
func ParseGitLocator(value string) (GitLocator, error) {
	if value == "" {
		return GitLocator{}, fmt.Errorf("git locator is required")
	}
	if err := validateGitBoundaryText("git locator", value); err != nil {
		return GitLocator{}, err
	}
	if strings.HasPrefix(value, "-") {
		return GitLocator{}, fmt.Errorf("git locator must not begin with an option prefix")
	}
	if strings.Contains(value, "::") {
		return GitLocator{}, fmt.Errorf("git locator form is unsupported")
	}

	if filepath.IsAbs(value) {
		cleaned := filepath.Clean(value)
		return GitLocator{value: cleaned, kind: gitLocatorKindNativeLocal, localPath: cleaned}, nil
	}
	if strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "ssh://") || strings.HasPrefix(value, "file://") {
		return parseGitURLLocator(value)
	}
	if strings.Contains(value, "://") {
		return GitLocator{}, fmt.Errorf("git locator scheme is unsupported")
	}
	if locator, ok := parseSCPLikeGitLocator(value); ok {
		return locator, nil
	}

	return GitLocator{}, fmt.Errorf("git locator must be an admitted URL, scp-like SSH address, or absolute path")
}

func parseGitURLLocator(value string) (GitLocator, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Opaque != "" {
		return GitLocator{}, fmt.Errorf("git locator is malformed")
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.Contains(value, "#") {
		return GitLocator{}, fmt.Errorf("git locator must not contain query or fragment fields")
	}

	decodedPath, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil {
		return GitLocator{}, fmt.Errorf("git locator is malformed")
	}
	if err := validateGitBoundaryText("git locator", decodedPath); err != nil {
		return GitLocator{}, err
	}

	switch parsed.Scheme {
	case "https":
		if parsed.User != nil {
			return GitLocator{}, fmt.Errorf("HTTPS locator must not contain userinfo")
		}
		if parsed.Host == "" || parsed.Path == "" || parsed.Path == "/" {
			return GitLocator{}, fmt.Errorf("HTTPS locator must include a host and repository path")
		}
		return GitLocator{value: value, kind: gitLocatorKindHTTPS}, nil
	case "ssh":
		if parsed.Host == "" || parsed.Path == "" || parsed.Path == "/" {
			return GitLocator{}, fmt.Errorf("SSH locator must include a host and repository path")
		}
		if parsed.User != nil {
			if _, hasPassword := parsed.User.Password(); hasPassword {
				return GitLocator{}, fmt.Errorf("SSH locator must not contain a password")
			}
			if parsed.User.Username() == "" {
				return GitLocator{}, fmt.Errorf("SSH locator username is empty")
			}
			if err := validateGitBoundaryText("git locator", parsed.User.Username()); err != nil {
				return GitLocator{}, err
			}
		}
		return GitLocator{value: value, kind: gitLocatorKindSSH}, nil
	case "file":
		if parsed.User != nil || parsed.Host != "" {
			return GitLocator{}, fmt.Errorf("file locator must not contain host or userinfo")
		}
		if decodedPath == "" || !filepath.IsAbs(filepath.FromSlash(decodedPath)) {
			return GitLocator{}, fmt.Errorf("file locator must contain an absolute path")
		}
		return GitLocator{
			value:     value,
			kind:      gitLocatorKindFile,
			localPath: filepath.Clean(filepath.FromSlash(decodedPath)),
		}, nil
	default:
		return GitLocator{}, fmt.Errorf("git locator scheme is unsupported")
	}
}

func parseSCPLikeGitLocator(value string) (GitLocator, bool) {
	colon := strings.IndexByte(value, ':')
	if colon <= 0 || strings.Contains(value[:colon], "/") || colon == len(value)-1 {
		return GitLocator{}, false
	}

	authority := value[:colon]
	if strings.Count(authority, "@") > 1 || strings.ContainsFunc(value, unicode.IsSpace) {
		return GitLocator{}, false
	}
	host := authority
	if at := strings.IndexByte(authority, '@'); at >= 0 {
		if at == 0 || at == len(authority)-1 {
			return GitLocator{}, false
		}
		host = authority[at+1:]
	}
	if host == "" || strings.HasPrefix(host, "-") {
		return GitLocator{}, false
	}

	return GitLocator{value: value, kind: gitLocatorKindSCPLike}, true
}

func parseGitRepositoryPath(value string) (GitRepositoryPath, error) {
	if value == "" {
		return GitRepositoryPath{}, fmt.Errorf("git repository path is required")
	}
	if err := validateGitBoundaryText("git repository path", value); err != nil {
		return GitRepositoryPath{}, err
	}
	if strings.ContainsRune(value, '\\') {
		return GitRepositoryPath{}, fmt.Errorf("git repository path must use POSIX separators")
	}
	if pathpkg.IsAbs(value) {
		return GitRepositoryPath{}, fmt.Errorf("git repository path must be relative")
	}
	clean := pathpkg.Clean(value)
	if clean != value {
		return GitRepositoryPath{}, fmt.Errorf("git repository path must already be clean")
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return GitRepositoryPath{}, fmt.Errorf("git repository path must stay inside the repository")
	}

	return GitRepositoryPath{value: clean}, nil
}

func parseGitRefSelector(value string) (GitRefSelector, error) {
	if value == "" {
		return GitRefSelector{}, fmt.Errorf("git ref is required")
	}
	if err := validateGitBoundaryText("git ref", value); err != nil {
		return GitRefSelector{}, err
	}
	if strings.HasPrefix(value, "-") {
		return GitRefSelector{}, fmt.Errorf("git ref must not begin with an option prefix")
	}

	if isASCIIHex(value) {
		if len(value) != 40 && len(value) != 64 {
			return GitRefSelector{}, fmt.Errorf("abbreviated object ids are unsupported; use a full 40- or 64-hex id")
		}
		return GitRefSelector{kind: gitRefKindCommit, value: strings.ToLower(value)}, nil
	}

	if slices.Contains([]string{"HEAD", "FETCH_HEAD", "ORIG_HEAD", "MERGE_HEAD", "@"}, value) {
		return GitRefSelector{}, fmt.Errorf("git pseudo-ref is unsupported")
	}

	kind := gitRefKindName
	name := value
	switch {
	case strings.HasPrefix(value, "refs/heads/"):
		kind = gitRefKindBranch
		name = strings.TrimPrefix(value, "refs/heads/")
	case strings.HasPrefix(value, "refs/tags/"):
		kind = gitRefKindTag
		name = strings.TrimPrefix(value, "refs/tags/")
	case strings.HasPrefix(value, "refs/"):
		return GitRefSelector{}, fmt.Errorf("git ref namespace is unsupported")
	}

	if err := validateGitSymbolicRef(name); err != nil {
		return GitRefSelector{}, err
	}
	return GitRefSelector{kind: kind, value: name}, nil
}

func validateGitSymbolicRef(value string) error {
	if value == "" || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.Contains(value, "//") {
		return fmt.Errorf("git ref has an invalid path component")
	}
	if strings.HasPrefix(value, "-") {
		return fmt.Errorf("git ref must not begin with an option prefix")
	}
	if strings.Contains(value, "..") || strings.Contains(value, "@{") || strings.ContainsAny(value, "~^:?*[\\") {
		return fmt.Errorf("git ref contains forbidden revision or refspec syntax")
	}
	if strings.HasSuffix(value, ".") || strings.ContainsFunc(value, unicode.IsSpace) {
		return fmt.Errorf("git ref has an invalid path component")
	}
	for component := range strings.SplitSeq(value, "/") {
		if component == "" || strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return fmt.Errorf("git ref has an invalid path component")
		}
	}

	return nil
}

func validateGitBoundaryText(field string, value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s is not valid UTF-8", field)
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s has surrounding whitespace", field)
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.In(character, unicode.Cf) {
			return fmt.Errorf("%s contains a control or format character", field)
		}
	}
	return nil
}

func isASCIIHex(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if !((character >= '0' && character <= '9') ||
			(character >= 'a' && character <= 'f') ||
			(character >= 'A' && character <= 'F')) {
			return false
		}
	}
	return true
}
