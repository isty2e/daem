package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"strconv"
	"strings"
)

const installedPrefix = "installed-v1:"

// PathHashes binds the installed-content digest and the compatible source-v1
// digests to the same byte stream. A regular file has two possible old baselines.
type PathHashes struct {
	Installed, Source, Alternate ContentHash
}

func (h PathHashes) Matches(want ContentHash) bool {
	return want != "" && (want == h.Installed || want == h.Source || want == h.Alternate)
}

func (h ContentHash) IsInstalled() bool { return strings.HasPrefix(string(h), installedPrefix) }

// ValidateManaged accepts legacy baselines and installed content, never used
// to admit a source lock or an ExactIdentity.
func (h ContentHash) ValidateManaged() error {
	return ContentHash(strings.TrimPrefix(string(h), installedPrefix)).Validate()
}

func beginInstalled(kind string) hash.Hash {
	h := sha256.New()
	writeRecord(h, "daem-installed-content-v1", kind)
	return h
}

func finishInstalled(h hash.Hash) ContentHash {
	return ContentHash(installedPrefix + "sha256:" + hex.EncodeToString(h.Sum(nil)))
}

func InstalledFileHash(content []byte) ContentHash {
	h := beginInstalled("file")
	writeRecord(h, strconv.Itoa(len(content)))
	h.Write(content)
	return finishInstalled(h)
}

func FileHashes(content []byte, executable bool) PathHashes {
	return PathHashes{Installed: InstalledFileHash(content), Source: HashFileContentWithExecutable(content, executable), Alternate: HashFileContentWithExecutable(content, !executable)}
}

// FileHashesFromReader consumes exactly size bytes once, including the EOF check.
func FileHashesFromReader(ctx context.Context, reader io.Reader, size int64, executable bool) (PathHashes, error) {
	if reader == nil || size < 0 {
		return PathHashes{}, fmt.Errorf("invalid file hash input")
	}
	installed, alternate := beginInstalled("file"), sha256.New()
	length := strconv.FormatInt(size, 10)
	writeRecord(installed, length)
	writeRecord(alternate, hashFormatVersion)
	writeRecord(alternate, "file", ".", executableLabel(!executable), length)
	actual, err := HashFileReader(ctx, io.TeeReader(reader, io.MultiWriter(installed, alternate)), size, executable)
	if err != nil {
		return PathHashes{}, err
	}
	writeRecord(alternate, "endfile")
	return PathHashes{Installed: finishInstalled(installed), Source: actual, Alternate: ContentHash("sha256:" + hex.EncodeToString(alternate.Sum(nil)))}, nil
}

func (h ContentHash) MatchesFile(content []byte) bool {
	if h.IsInstalled() {
		return InstalledFileHash(content) == h
	}
	return HashFileContent(content) == h || HashFileContentWithExecutable(content, true) == h
}
