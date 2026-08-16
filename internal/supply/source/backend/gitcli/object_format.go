package gitcli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/supply/source"
)

type gitObjectFormat string

const (
	gitObjectFormatSHA1   gitObjectFormat = "sha1"
	gitObjectFormatSHA256 gitObjectFormat = "sha256"
)

func (format gitObjectFormat) validate() error {
	switch format {
	case gitObjectFormatSHA1, gitObjectFormatSHA256:
		return nil
	default:
		return fmt.Errorf("git object format %q is unsupported", format)
	}
}

func parseGitObjectFormat(value string) (gitObjectFormat, error) {
	format := gitObjectFormat(strings.TrimSpace(value))
	if err := format.validate(); err != nil {
		return "", err
	}
	return format, nil
}

func gitObjectFormatFromCommitID(value string) (gitObjectFormat, error) {
	switch len(value) {
	case 40:
		return gitObjectFormatSHA1, nil
	case 64:
		return gitObjectFormatSHA256, nil
	default:
		return "", fmt.Errorf("git commit id width is unsupported")
	}
}

func gitObjectFormatFromAdvertisedIDs(output string) (gitObjectFormat, bool, error) {
	var format gitObjectFormat
	found := false
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		id, _, ok := strings.Cut(line, "\t")
		if !ok {
			return "", false, fmt.Errorf("git ls-remote output is malformed")
		}
		if strings.HasPrefix(id, "ref: ") {
			continue
		}
		if !isGitObjectID(id) {
			return "", false, fmt.Errorf("git ls-remote advertised an unsupported object id")
		}
		current, err := gitObjectFormatFromCommitID(id)
		if err != nil {
			return "", false, err
		}
		if !found {
			format = current
			found = true
			continue
		}
		if current != format {
			return "", false, fmt.Errorf("git source object format is inconsistent: origin advertised mixed object-id widths")
		}
	}
	return format, found, nil
}

func isGitObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for i := 0; i < len(value); i++ {
		character := value[i]
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') &&
			(character < 'A' || character > 'F') {
			return false
		}
	}
	return true
}

func (resolver Resolver) observeObjectFormat(
	ctx context.Context,
	cacheRoot *rootedpath.CapturedRoot,
	gitSource source.GitSource,
) (gitObjectFormat, error) {
	var declared gitObjectFormat
	hasDeclared := false
	if gitSource.Ref().IsCommit() {
		format, err := gitObjectFormatFromCommitID(gitSource.Ref().String())
		if err != nil {
			return "", err
		}
		declared = format
		hasDeclared = true
	}

	origin, hasOrigin, err := resolver.observeOriginObjectFormat(ctx, cacheRoot, gitSource.Locator())
	if err != nil {
		return "", err
	}
	switch {
	case hasDeclared && hasOrigin && declared != origin:
		return "", fmt.Errorf("git source object format does not match the declared commit id")
	case hasDeclared:
		return declared, nil
	case hasOrigin:
		return origin, nil
	default:
		return "", fmt.Errorf(
			"git source %q object format is unobservable: origin advertised no object ids",
			gitSource.Locator().String(),
		)
	}
}

func (resolver Resolver) observeOriginObjectFormat(
	ctx context.Context,
	cacheRoot *rootedpath.CapturedRoot,
	locator source.GitLocator,
) (gitObjectFormat, bool, error) {
	if localPath, ok := locator.LocalPath(); ok {
		info, err := os.Stat(localPath)
		if err != nil {
			if os.IsNotExist(err) {
				return "", false, nil
			}
			return "", false, fmt.Errorf("inspect local git locator: %w", err)
		}
		if !info.IsDir() {
			return "", false, nil
		}
		output, err := resolver.gitOutputInCacheRoot(ctx, cacheRoot, localObjectFormatArgs(localPath)...)
		if err != nil {
			return "", false, fmt.Errorf("inspect local git object format: %w", err)
		}
		format, err := parseGitObjectFormat(output)
		if err != nil {
			return "", false, err
		}
		return format, true, nil
	}

	output, err := resolver.gitOutputInCacheRoot(ctx, cacheRoot, lsRemoteRefsArgs(locator.String())...)
	if err != nil {
		return "", false, fmt.Errorf("inspect remote git object format: %w", err)
	}
	return gitObjectFormatFromAdvertisedIDs(output)
}
