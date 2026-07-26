package source

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
)

func TestValidateResolutionCorrelationEnforcesSourceSpecificRefs(t *testing.T) {
	t.Parallel()

	gitSource, err := NewGitSource("https://example.com/repo.git", ".", "main")
	if err != nil {
		t.Fatalf("NewGitSource() error = %v", err)
	}
	localSource := mustLocalSource(t, "skills/demo", LocalSourceModeVendor)
	s3Source := mustS3Source(t, "s3://bucket/demo.tar.gz", "version-1", "", S3ObjectFormatTarGzip)

	tests := []struct {
		name        string
		source      Source
		resolvedRef artifact.ResolvedRef
		wantError   bool
	}{
		{name: "git full sha1", source: gitSource, resolvedRef: artifact.ResolvedRef(strings.Repeat("a", 40))},
		{name: "git abbreviated", source: gitSource, resolvedRef: "abc", wantError: true},
		{name: "git uppercase", source: gitSource, resolvedRef: artifact.ResolvedRef(strings.Repeat("A", 40)), wantError: true},
		{name: "local empty", source: localSource},
		{name: "local ref", source: localSource, resolvedRef: "unexpected", wantError: true},
		{name: "s3 exact version", source: s3Source, resolvedRef: "version-1"},
		{name: "s3 wrong version", source: s3Source, resolvedRef: "version-2", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			sourceID, err := SourceIDFor(test.source)
			if err != nil {
				t.Fatalf("SourceIDFor() error = %v", err)
			}
			err = ValidateResolutionCorrelation(test.source, sourceID, test.resolvedRef)
			if (err != nil) != test.wantError {
				t.Fatalf("ValidateResolutionCorrelation() error = %v, wantError %v", err, test.wantError)
			}
		})
	}
}

func TestValidateResolutionCorrelationRejectsForeignSourceID(t *testing.T) {
	t.Parallel()

	sourceSpec := mustLocalSource(t, "skills/demo", LocalSourceModeVendor)
	if err := ValidateResolutionCorrelation(sourceSpec, "foreign", ""); err == nil {
		t.Fatal("ValidateResolutionCorrelation() accepted a foreign source id")
	}
}
