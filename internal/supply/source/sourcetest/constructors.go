// Package sourcetest provides constructor-only helpers for tests that consume
// canonical source values across package boundaries.
package sourcetest

import "github.com/isty2e/daem/internal/supply/source"

// TB is the test capability required by constructor helpers.
type TB interface {
	Helper()
	Fatalf(format string, args ...any)
}

// Local constructs canonical local provenance or fails the test.
func Local(test TB, path string, mode source.LocalSourceMode) source.Source {
	test.Helper()
	value, err := source.NewLocalSource(path, mode)
	if err != nil {
		test.Fatalf("source.NewLocalSource returned error: %v", err)
	}
	return value
}

// S3 constructs canonical S3 provenance or fails the test.
func S3(
	test TB,
	uri string,
	versionID string,
	region string,
	format source.S3ObjectFormat,
) source.Source {
	test.Helper()
	value, err := source.NewS3Source(uri, versionID, region, format)
	if err != nil {
		test.Fatalf("source.NewS3Source returned error: %v", err)
	}
	return value
}
