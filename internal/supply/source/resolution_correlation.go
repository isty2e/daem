package source

import (
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/supply/artifact"
)

// ValidateResolutionCorrelation checks source-specific identity facts returned
// by acquisition. Artifact content and materialized access remain foreign.
func ValidateResolutionCorrelation(
	sourceSpec Source,
	sourceID artifact.SourceID,
	resolvedRef artifact.ResolvedRef,
) error {
	if err := sourceID.Validate(); err != nil {
		return fmt.Errorf("resolution: %w", err)
	}
	if err := resolvedRef.Validate(); err != nil {
		return fmt.Errorf("resolution: %w", err)
	}
	expectedSourceID, err := SourceIDFor(sourceSpec)
	if err != nil {
		return fmt.Errorf("resolution source: %w", err)
	}
	if sourceID != expectedSourceID {
		return fmt.Errorf("resolution source id %q does not match requested source id %q", sourceID, expectedSourceID)
	}

	switch sourceSpec.Kind() {
	case SourceKindGit:
		gitSource, ok := sourceSpec.Git()
		if !ok {
			return fmt.Errorf("git source data is unavailable")
		}
		value := string(resolvedRef)
		if (len(value) != 40 && len(value) != 64) ||
			!isASCIIHex(value) || strings.ToLower(value) != value {
			return fmt.Errorf("git resolution requires a lowercase full immutable object id")
		}
		if gitSource.Ref().IsCommit() && value != gitSource.Ref().String() {
			return fmt.Errorf("git resolution ref does not match requested immutable object id")
		}
	case SourceKindLocal:
		if resolvedRef != "" {
			return fmt.Errorf("local resolution must not carry a resolved ref")
		}
	case SourceKindS3:
		s3Source, ok := sourceSpec.S3()
		if !ok {
			return fmt.Errorf("s3 source data is unavailable")
		}
		if s3Source.versionID != "" && string(resolvedRef) != s3Source.versionID {
			return fmt.Errorf("s3 resolution ref does not match requested version id")
		}
	default:
		return fmt.Errorf("unsupported source kind %q", sourceSpec.Kind())
	}
	return nil
}
