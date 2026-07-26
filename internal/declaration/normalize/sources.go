package normalize

import (
	"fmt"

	"github.com/isty2e/daem/internal/declaration"
	"github.com/isty2e/daem/internal/supply/source"
)

func normalizeRequiredSource(raw declaration.Source, context string) (source.Source, error) {
	if raw.Git != "" {
		if raw.S3 != "" {
			return source.Source{}, fmt.Errorf("%s.s3: git sources cannot set s3", context)
		}
		if raw.Mode != "" {
			return source.Source{}, fmt.Errorf("%s.mode: git sources cannot set mode", context)
		}
		if raw.VersionID != "" {
			return source.Source{}, fmt.Errorf("%s.version_id: git sources cannot set version_id", context)
		}
		if raw.Region != "" {
			return source.Source{}, fmt.Errorf("%s.region: git sources cannot set region", context)
		}
		if raw.Format != "" {
			return source.Source{}, fmt.Errorf("%s.format: git sources cannot set format", context)
		}

		if raw.Path == "" {
			return source.Source{}, fmt.Errorf("%s.path: required for git source", context)
		}

		if raw.Ref == "" {
			return source.Source{}, fmt.Errorf("%s.ref: required for git source", context)
		}

		sourceSpec, err := source.NewGitSource(raw.Git, raw.Path, raw.Ref)
		if err != nil {
			return source.Source{}, fmt.Errorf("%s: %w", context, err)
		}
		return sourceSpec, nil
	}

	if raw.S3 != "" {
		if raw.Path != "" {
			return source.Source{}, fmt.Errorf("%s.path: s3 sources cannot set path", context)
		}
		if raw.Ref != "" {
			return source.Source{}, fmt.Errorf("%s.ref: s3 sources cannot set ref; use version_id", context)
		}
		if raw.Mode != "" {
			return source.Source{}, fmt.Errorf("%s.mode: s3 sources cannot set mode", context)
		}
		if _, err := source.ParseS3ObjectURI(raw.S3); err != nil {
			return source.Source{}, fmt.Errorf("%s.s3: %w", context, err)
		}

		format, err := source.ParseS3ObjectFormat(raw.Format)
		if err != nil {
			return source.Source{}, fmt.Errorf("%s.format: %w", context, err)
		}

		sourceSpec, err := source.NewS3Source(raw.S3, raw.VersionID, raw.Region, format)
		if err != nil {
			return source.Source{}, fmt.Errorf("%s: %w", context, err)
		}
		return sourceSpec, nil
	}

	if raw.Path == "" {
		return source.Source{}, fmt.Errorf("%s: source must set git, path, or s3", context)
	}

	if raw.Ref != "" {
		return source.Source{}, fmt.Errorf("%s.ref: local sources cannot set ref", context)
	}
	if raw.VersionID != "" {
		return source.Source{}, fmt.Errorf("%s.version_id: local sources cannot set version_id", context)
	}
	if raw.Region != "" {
		return source.Source{}, fmt.Errorf("%s.region: local sources cannot set region", context)
	}
	if raw.Format != "" {
		return source.Source{}, fmt.Errorf("%s.format: local sources cannot set format", context)
	}

	if raw.Mode == "" {
		return source.Source{}, fmt.Errorf("%s.mode: required for local source", context)
	}

	mode, err := source.ParseLocalSourceMode(raw.Mode)
	if err != nil {
		return source.Source{}, fmt.Errorf("%s.mode: %w", context, err)
	}

	sourceSpec, err := source.NewLocalSource(raw.Path, mode)
	if err != nil {
		return source.Source{}, fmt.Errorf("%s: %w", context, err)
	}
	return sourceSpec, nil
}
