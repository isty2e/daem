package lock

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/isty2e/daem/internal/supply/artifact"
)

func testExactHash(label string) artifact.ContentHash {
	digest := sha256.Sum256([]byte(label))
	return artifact.ContentHash("sha256:" + hex.EncodeToString(digest[:]))
}
