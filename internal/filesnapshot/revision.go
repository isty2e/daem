package filesnapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"os"
	"strconv"
)

const regularFileSnapshotRevisionPrefix = "daem-regular-file-snapshot-v1:"

type fileObjectIdentity struct {
	device uint64
	inode  uint64
}

func regularFileSnapshotRevision(info os.FileInfo, content []byte) string {
	hasher := sha256.New()
	writeSnapshotRevisionRecord(
		hasher,
		"mode",
		strconv.FormatUint(uint64(info.Mode()), 10),
		"size",
		strconv.FormatInt(info.Size(), 10),
		"mtime",
		strconv.FormatInt(info.ModTime().Unix(), 10),
		strconv.FormatInt(int64(info.ModTime().Nanosecond()), 10),
	)
	if change, ok := fileChangeVersion(info); ok {
		writeSnapshotRevisionRecord(
			hasher,
			"change-version",
			strconv.FormatInt(change.seconds, 10),
			strconv.FormatInt(change.nanoseconds, 10),
		)
	} else {
		writeSnapshotRevisionRecord(hasher, "change-version-unavailable")
	}
	if identity, ok := regularFileObjectIdentity(info); ok {
		writeSnapshotRevisionRecord(
			hasher,
			"object-identity",
			strconv.FormatUint(identity.device, 10),
			strconv.FormatUint(identity.inode, 10),
		)
	} else {
		writeSnapshotRevisionRecord(hasher, "object-identity-unavailable")
	}
	writeSnapshotRevisionRecord(hasher, "content", strconv.Itoa(len(content)))
	_, _ = hasher.Write(content)
	writeSnapshotRevisionRecord(hasher, "end-content")
	return regularFileSnapshotRevisionPrefix + hex.EncodeToString(hasher.Sum(nil))
}

func writeSnapshotRevisionRecord(hasher hash.Hash, fields ...string) {
	for _, field := range fields {
		_, _ = hasher.Write([]byte(strconv.Itoa(len(field))))
		_, _ = hasher.Write([]byte(":"))
		_, _ = hasher.Write([]byte(field))
	}
	_, _ = hasher.Write([]byte("\n"))
}
