package gitcli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/supply/source"
)

type gitObjectFormat string

const (
	gitObjectFormatSHA1   gitObjectFormat = "sha1"
	gitObjectFormatSHA256 gitObjectFormat = "sha256"

	defaultRemoteRefAdvertisementBytes   = 8 << 20
	defaultRemoteRefAdvertisementRecords = 100_000
	defaultRemoteRefAdvertisementLine    = 4_096
)

type remoteRefAdvertisementBudget struct {
	maxBytes     int
	maxRecords   int
	maxLineBytes int
	bytes        int
	records      int
}

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

func defaultRemoteRefAdvertisementBudget() remoteRefAdvertisementBudget {
	return remoteRefAdvertisementBudget{
		maxBytes:     defaultRemoteRefAdvertisementBytes,
		maxRecords:   defaultRemoteRefAdvertisementRecords,
		maxLineBytes: defaultRemoteRefAdvertisementLine,
	}
}

func (budget *remoteRefAdvertisementBudget) admit(recordBytes int) error {
	if budget == nil || budget.maxBytes <= 0 || budget.maxRecords <= 0 || budget.maxLineBytes <= 0 {
		return fmt.Errorf("git ls-remote advertisement budget is invalid")
	}
	if recordBytes > budget.maxLineBytes {
		return fmt.Errorf("git ls-remote advertised a record that exceeds %d bytes", budget.maxLineBytes)
	}
	budget.bytes += recordBytes
	if budget.bytes > budget.maxBytes {
		return fmt.Errorf("git ls-remote advertisement exceeds %d bytes", budget.maxBytes)
	}
	budget.records++
	if budget.records > budget.maxRecords {
		return fmt.Errorf("git ls-remote advertisement exceeds %d records", budget.maxRecords)
	}
	return nil
}

func gitObjectFormatFromAdvertisedIDs(output string) (gitObjectFormat, bool, error) {
	return observeAdvertisedObjectFormat(strings.NewReader(output), defaultRemoteRefAdvertisementBudget())
}

func observeAdvertisedObjectFormat(
	output io.Reader,
	budget remoteRefAdvertisementBudget,
) (gitObjectFormat, bool, error) {
	if budget.maxBytes <= 0 || budget.maxRecords <= 0 || budget.maxLineBytes <= 0 {
		return "", false, fmt.Errorf("git ls-remote advertisement budget is invalid")
	}
	reader := bufio.NewReaderSize(output, budget.maxLineBytes)
	var format gitObjectFormat
	found := false
	for {
		record, readErr := reader.ReadSlice('\n')
		if errors.Is(readErr, bufio.ErrBufferFull) {
			return "", false, fmt.Errorf(
				"git ls-remote advertised a record that exceeds %d bytes",
				budget.maxLineBytes,
			)
		}
		if len(record) == 0 && errors.Is(readErr, io.EOF) {
			return format, found, nil
		}
		if len(record) > 0 {
			if err := budget.admit(len(record)); err != nil {
				return "", false, err
			}
			line := strings.TrimSuffix(strings.TrimSuffix(string(record), "\n"), "\r")
			if line != "" {
				current, skip, err := advertisedObjectFormatFromLine(line)
				if err != nil {
					return "", false, err
				}
				if !skip {
					if !found {
						format = current
						found = true
					} else if current != format {
						return "", false, fmt.Errorf("git source object format is inconsistent: origin advertised mixed object-id widths")
					}
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			return format, found, nil
		}
		if readErr != nil {
			return "", false, readErr
		}
	}
}

func advertisedObjectFormatFromLine(line string) (gitObjectFormat, bool, error) {
	id, _, ok := strings.Cut(line, "\t")
	if !ok {
		return "", false, fmt.Errorf("git ls-remote output is malformed")
	}
	if strings.HasPrefix(id, "ref: ") {
		return "", true, nil
	}
	if !isGitObjectID(id) {
		return "", false, fmt.Errorf("git ls-remote advertised an unsupported object id")
	}
	format, err := gitObjectFormatFromCommitID(id)
	if err != nil {
		return "", false, err
	}
	return format, false, nil
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
	var format gitObjectFormat
	switch {
	case hasDeclared && hasOrigin && declared != origin:
		return "", fmt.Errorf("git source object format does not match the declared commit id")
	case hasDeclared:
		format = declared
	case hasOrigin:
		format = origin
	default:
		return "", fmt.Errorf(
			"git source %q object format is unobservable: origin advertised no object ids",
			gitSource.Locator().String(),
		)
	}
	if err := resolver.admitObservedObjectFormat(ctx, format); err != nil {
		return "", err
	}
	return format, nil
}

func (resolver Resolver) admitObservedObjectFormat(ctx context.Context, format gitObjectFormat) error {
	if err := format.validate(); err != nil {
		return err
	}
	if format != gitObjectFormatSHA256 {
		return nil
	}
	supported, err := resolver.explicitObjectFormatSupported(ctx)
	if err != nil {
		return err
	}
	if supported {
		return nil
	}
	return fmt.Errorf("git source object format sha256 requires a git binary that supports --object-format")
}

func (resolver Resolver) observeOriginObjectFormat(
	ctx context.Context,
	cacheRoot *rootedpath.CapturedRoot,
	locator source.GitLocator,
) (gitObjectFormat, bool, error) {
	if localPath, ok := locator.LocalPath(); ok {
		return resolver.observeLocalObjectFormat(ctx, cacheRoot, localPath)
	}

	if err := resolver.requireDeclaredTransportEndpoint(ctx, cacheRoot, locator); err != nil {
		return "", false, err
	}

	var format gitObjectFormat
	found := false
	err := resolver.consumeGitOutputInCacheRoot(
		ctx,
		cacheRoot,
		func(output io.Reader) error {
			var consumeErr error
			format, found, consumeErr = observeAdvertisedObjectFormat(
				output,
				resolver.remoteRefAdvertisementBudget(),
			)
			return consumeErr
		},
		lsRemoteRefsArgs(locator.String())...,
	)
	if err != nil {
		return "", false, fmt.Errorf("inspect remote git object format: %w", err)
	}
	return format, found, nil
}

func (resolver Resolver) observeLocalObjectFormat(
	ctx context.Context,
	cacheRoot *rootedpath.CapturedRoot,
	localPath string,
) (gitObjectFormat, bool, error) {
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
	if err == nil {
		format, parseErr := parseGitObjectFormat(output)
		if parseErr != nil {
			return "", false, parseErr
		}
		return format, true, nil
	}
	supported, capabilityErr := resolver.explicitObjectFormatSupported(ctx)
	if capabilityErr != nil {
		return "", false, capabilityErr
	}
	if supported || !gitErrorIndicatesUnknownOption(err) {
		return "", false, fmt.Errorf("inspect local git object format: %w", err)
	}
	head, headErr := resolver.gitOutputInCacheRoot(ctx, cacheRoot, localHEADObjectIDArgs(localPath)...)
	if headErr != nil {
		return "", false, fmt.Errorf("inspect local git object format: %w", err)
	}
	format, parseErr := gitObjectFormatFromCommitID(strings.TrimSpace(head))
	if parseErr != nil {
		return "", false, parseErr
	}
	return format, true, nil
}

func (resolver Resolver) requireDeclaredTransportEndpoint(
	ctx context.Context,
	cacheRoot *rootedpath.CapturedRoot,
	locator source.GitLocator,
) error {
	if cacheRoot == nil {
		return fmt.Errorf("git source cache root authority is required")
	}
	authority, err := cacheRoot.Authority()
	if err != nil {
		return fmt.Errorf("inspect git source cache root: %w", err)
	}
	probeDir, err := os.MkdirTemp(authority.PhysicalRoot(), "url-probe-")
	if err != nil {
		return fmt.Errorf("create git locator rewrite probe: %w", err)
	}
	defer os.RemoveAll(probeDir)

	if err := resolver.runGitInCacheRoot(ctx, cacheRoot, initializeBareDirectoryArgs(probeDir)...); err != nil {
		return fmt.Errorf("initialize git locator rewrite probe: %w", err)
	}
	if err := resolver.runGitInCacheRoot(
		ctx,
		cacheRoot,
		gitCommandInDirectoryArgs(probeDir, addOriginArgs(locator.String()))...,
	); err != nil {
		return fmt.Errorf("declare git locator rewrite probe origin: %w", err)
	}
	output, err := resolver.gitOutputInCacheRoot(
		ctx,
		cacheRoot,
		gitCommandInDirectoryArgs(probeDir, inspectEffectiveOriginArgs())...,
	)
	if err != nil {
		return fmt.Errorf("inspect effective git source origin: %w", err)
	}
	effectiveValue, ok := trimSingleGitConfigValue(output)
	if !ok {
		return fmt.Errorf("effective git source origin must contain exactly one canonical locator")
	}
	effective, err := source.ParseGitLocator(effectiveValue)
	if err != nil || !locator.Equivalent(effective) {
		return fmt.Errorf("effective git source origin does not match the declared locator")
	}
	return nil
}

func (resolver Resolver) remoteRefAdvertisementBudget() remoteRefAdvertisementBudget {
	if resolver.state != nil && resolver.state.testRemoteRefAdvertisementBudget != nil {
		return *resolver.state.testRemoteRefAdvertisementBudget
	}
	return defaultRemoteRefAdvertisementBudget()
}
