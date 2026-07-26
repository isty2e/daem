package source

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/supply/artifact"
)

// LocalSourceMode controls how a local source participates in reproducibility.
type LocalSourceMode string

const (
	LocalSourceModeVendor LocalSourceMode = "vendor"
	LocalSourceModeLink   LocalSourceMode = "link"
)

// S3ObjectFormat controls how an S3 object is materialized into an artifact.
type S3ObjectFormat string

const (
	S3ObjectFormatFile    S3ObjectFormat = "file"
	S3ObjectFormatTar     S3ObjectFormat = "tar"
	S3ObjectFormatTarGzip S3ObjectFormat = "tar.gz"
)

// SourceKind identifies the provenance family of a source.
type SourceKind string

const (
	SourceKindGit   SourceKind = "git"
	SourceKindLocal SourceKind = "local"
	SourceKindS3    SourceKind = "s3"
)

// LocalSource describes a source resolved from the local filesystem.
type LocalSource struct {
	path string
	mode LocalSourceMode
}

// S3Source describes a source resolved from a single S3 object.
type S3Source struct {
	objectURI S3ObjectURI
	versionID string
	region    string
	format    S3ObjectFormat
}

// Source is the canonical provenance for a resource.
type Source struct {
	kind  SourceKind
	git   GitSource
	local LocalSource
	s3    S3Source
}

// S3ObjectURI is a validated s3://bucket/key object address.
type S3ObjectURI struct {
	bucket    string
	key       string
	canonical string
}

// NewLocalSource validates and constructs canonical local provenance.
func NewLocalSource(path string, mode LocalSourceMode) (Source, error) {
	canonicalPath, err := canonicalLocalSourcePath(path)
	if err != nil {
		return Source{}, err
	}
	canonicalMode, err := ParseLocalSourceMode(string(mode))
	if err != nil {
		return Source{}, err
	}

	return Source{
		kind: SourceKindLocal,
		local: LocalSource{
			path: canonicalPath,
			mode: canonicalMode,
		},
	}, nil
}

// NewS3Source validates and constructs canonical S3 provenance.
func NewS3Source(uri string, versionID string, region string, format S3ObjectFormat) (Source, error) {
	objectURI, err := ParseS3ObjectURI(uri)
	if err != nil {
		return Source{}, err
	}
	canonicalFormat, err := ParseS3ObjectFormat(string(format))
	if err != nil {
		return Source{}, err
	}

	return Source{
		kind: SourceKindS3,
		s3: S3Source{
			objectURI: objectURI,
			versionID: strings.TrimSpace(versionID),
			region:    strings.TrimSpace(region),
			format:    canonicalFormat,
		},
	}, nil
}

// Kind returns the source provenance family.
func (source Source) Kind() SourceKind {
	return source.kind
}

// Git returns the Git source data when this source is Git-backed.
func (source Source) Git() (GitSource, bool) {
	return source.git, source.kind == SourceKindGit
}

// Local returns the local source data when this source is filesystem-backed.
func (source Source) Local() (LocalSource, bool) {
	return source.local, source.kind == SourceKindLocal
}

// S3 returns the S3 source data when this source is S3-backed.
func (source Source) S3() (S3Source, bool) {
	return source.s3, source.kind == SourceKindS3
}

// Path returns the canonical lexical local path.
func (source LocalSource) Path() string { return source.path }

// Mode returns the admitted local reproducibility mode.
func (source LocalSource) Mode() LocalSourceMode { return source.mode }

// ObjectURI returns the validated S3 object address.
func (source S3Source) ObjectURI() S3ObjectURI { return source.objectURI }

// Bucket returns the decoded S3 bucket name.
func (uri S3ObjectURI) Bucket() string { return uri.bucket }

// Key returns the decoded S3 object key.
func (uri S3ObjectURI) Key() string { return uri.key }

// Canonical returns the canonical escaped S3 URI text.
func (uri S3ObjectURI) Canonical() string { return uri.canonical }

// VersionID returns the optional exact object version requested by the declaration.
func (source S3Source) VersionID() string { return source.versionID }

// Region returns the optional S3 region hint.
func (source S3Source) Region() string { return source.region }

// Format returns the admitted object materialization format.
func (source S3Source) Format() S3ObjectFormat { return source.format }

// SourceIDFor returns the canonical lockfile identity for a source.
func SourceIDFor(source Source) (artifact.SourceID, error) {
	switch source.Kind() {
	case SourceKindGit:
		gitSource, ok := source.Git()
		if !ok {
			return "", fmt.Errorf("git source data is unavailable")
		}

		return gitSource.SourceID(), nil
	case SourceKindLocal:
		localSource, ok := source.Local()
		if !ok {
			return "", fmt.Errorf("local source data is unavailable")
		}

		return artifact.SourceID(fmt.Sprintf("local:%s?mode=%s", filepath.ToSlash(localSource.path), localSource.mode)), nil
	case SourceKindS3:
		s3Source, ok := source.S3()
		if !ok {
			return "", fmt.Errorf("s3 source data is unavailable")
		}

		parts := make([]string, 0, 3)
		if s3Source.format != S3ObjectFormatFile {
			parts = append(parts, "format="+url.QueryEscape(string(s3Source.format)))
		}
		region := s3Source.region
		versionID := s3Source.versionID
		if region != "" {
			parts = append(parts, "region="+url.QueryEscape(region))
		}
		if versionID != "" {
			parts = append(parts, "version_id="+url.QueryEscape(versionID))
		}

		if len(parts) == 0 {
			return artifact.SourceID("s3:" + s3Source.objectURI.canonical), nil
		}

		return artifact.SourceID("s3:" + s3Source.objectURI.canonical + "?" + strings.Join(parts, "&")), nil
	default:
		return "", fmt.Errorf("unsupported source kind %q", source.Kind())
	}
}

// ParseLocalSourceMode validates a local source mode string.
func ParseLocalSourceMode(value string) (LocalSourceMode, error) {
	switch LocalSourceMode(value) {
	case LocalSourceModeVendor, LocalSourceModeLink:
		return LocalSourceMode(value), nil
	default:
		return "", fmt.Errorf("unknown local source mode %q", value)
	}
}

// ParseS3ObjectFormat validates and normalizes an S3 object format string.
func ParseS3ObjectFormat(value string) (S3ObjectFormat, error) {
	switch strings.TrimSpace(value) {
	case "", string(S3ObjectFormatFile):
		return S3ObjectFormatFile, nil
	case string(S3ObjectFormatTar):
		return S3ObjectFormatTar, nil
	case string(S3ObjectFormatTarGzip), "tgz":
		return S3ObjectFormatTarGzip, nil
	default:
		return "", fmt.Errorf("unknown S3 object format %q", value)
	}
}

// ParseS3ObjectURI validates a single S3 object URI. Prefix directory sources are unsupported.
func ParseS3ObjectURI(value string) (S3ObjectURI, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return S3ObjectURI{}, fmt.Errorf("s3 uri is required")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return S3ObjectURI{}, fmt.Errorf("parse s3 uri %q: %w", value, err)
	}
	if parsed.Scheme != "s3" {
		return S3ObjectURI{}, fmt.Errorf("s3 uri %q must use s3:// scheme", value)
	}
	if parsed.User != nil {
		return S3ObjectURI{}, fmt.Errorf("s3 uri %q must not include credentials", value)
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return S3ObjectURI{}, fmt.Errorf("s3 uri %q must include a bucket", value)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return S3ObjectURI{}, fmt.Errorf("s3 uri %q must not include query or fragment fields", value)
	}

	key := strings.TrimPrefix(parsed.Path, "/")
	if key == "" {
		return S3ObjectURI{}, fmt.Errorf("s3 uri %q must include an object key; prefix directory sources are unsupported", value)
	}
	if strings.ContainsRune(key, '\x00') {
		return S3ObjectURI{}, fmt.Errorf("s3 uri %q must not include NUL bytes", value)
	}
	if strings.HasSuffix(key, "/") {
		return S3ObjectURI{}, fmt.Errorf("s3 uri %q looks like a prefix directory source; prefix directory sources are unsupported", value)
	}

	// Re-escape the decoded key without RawPath so equivalent URI spellings
	// cannot create distinct source identities for the same S3 object key.
	escapedKey := strings.TrimPrefix((&url.URL{Path: "/" + key}).EscapedPath(), "/")

	return S3ObjectURI{
		bucket:    parsed.Host,
		key:       key,
		canonical: "s3://" + parsed.Host + "/" + escapedKey,
	}, nil
}

func canonicalLocalSourcePath(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("local path is required")
	}
	if strings.ContainsRune(trimmed, '\x00') {
		return "", fmt.Errorf("local path must not contain NUL bytes")
	}

	return filepath.Clean(trimmed), nil
}
