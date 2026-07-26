package execute

import "github.com/isty2e/daem/internal/supply/artifact"

func testArtifactHash(seed string) artifact.ContentHash {
	contentHash := artifact.ContentHash(seed)
	if contentHash.Validate() == nil {
		return contentHash
	}
	return artifact.HashFileContent([]byte(seed))
}
