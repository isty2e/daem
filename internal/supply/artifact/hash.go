package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"strconv"
)

const (
	hashAlgorithm     = "sha256"
	hashFormatVersion = "daem-source-hash-v1"
)

// HashFileContent computes hash-v1 for a non-executable regular file.
func HashFileContent(content []byte) ContentHash {
	return HashFileContentWithExecutable(content, false)
}

// HashFileContentWithExecutable computes hash-v1 for a regular file with declared mode.
func HashFileContentWithExecutable(content []byte, executable bool) ContentHash {
	hasher := sha256.New()
	writeRecord(hasher, hashFormatVersion)
	writeRecord(hasher, "file", ".", executableLabel(executable), strconv.Itoa(len(content)))
	hasher.Write(content)
	writeRecord(hasher, "endfile")

	return ContentHash(hashAlgorithm + ":" + hex.EncodeToString(hasher.Sum(nil)))
}

func executableLabel(executable bool) string {
	if executable {
		return "executable"
	}

	return "not-executable"
}

func writeRecord(hasher hash.Hash, fields ...string) {
	for _, field := range fields {
		hasher.Write([]byte(strconv.Itoa(len(field))))
		hasher.Write([]byte(":"))
		hasher.Write([]byte(field))
	}

	hasher.Write([]byte("\n"))
}
