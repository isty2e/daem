package journal

import "github.com/isty2e/daem/internal/supply/artifact"

func testContentHash(seed string) artifact.ContentHash {
	contentHash := artifact.ContentHash(seed)
	if contentHash.Validate() == nil {
		return contentHash
	}
	return artifact.HashFileContent([]byte(seed))
}

func testContentHashString(seed string) string {
	return string(testContentHash(seed))
}
