package recovery

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/workmeasure"
)

func TestPathEvidenceCopyRetainsWorkAndOwnsPermissionMode(t *testing.T) {
	work, err := workmeasure.NewArtifactWork(2, 17)
	if err != nil {
		t.Fatal(err)
	}
	source := []PathEvidence{{Path: "managed-file", PathMode: NewPermissionMode(0o600), Work: work}}
	cloned := ClonePathEvidence(source)
	*source[0].PathMode = *NewPermissionMode(0o700)
	source[0].Path = "other-file"
	if len(cloned) != 1 || cloned[0].Path != "managed-file" || cloned[0].PathMode.FileMode() != 0o600 || cloned[0].Work != work {
		t.Fatal("copied recovery evidence lost its measured facts or retained a mutable permission witness")
	}
}
