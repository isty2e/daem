package commit

import (
	"fmt"
	"math"
)

// FileWork projects one ordinary unrooted storage primitive into namespace,
// entry and payload work. Rooted-capability validation is not included.
type FileWork struct {
	pathWork int
	byteWork int64
	entries  int
}

// MaximumFileCommitWork projects the ordinary file algorithm from an absolute
// path's component count and payload bound, including ancestor creation and
// returned-error cleanup but not parent refresh. It performs no I/O or admission.
func MaximumFileCommitWork(pathComponents int, maximumBytes int64) (FileWork, error) {
	if pathComponents <= 0 || maximumBytes < 0 {
		return FileWork{}, fmt.Errorf("file commit work requires positive path depth and nonnegative bytes")
	}
	if pathComponents > int(^uint(0)>>1)/8 || maximumBytes > math.MaxInt64/2 {
		return FileWork{}, fmt.Errorf("file commit work overflows")
	}
	// Parent setup can visit a concurrently created component twice; the
	// lifecycle seals the chain five times, and error cleanup revisits created
	// ancestors once. Payload verification reads only the bytes just written.
	return FileWork{pathWork: 8 * pathComponents, byteWork: 2 * maximumBytes, entries: 1}, nil
}

// MaximumFileSnapshotWork includes the parent opening and post-read chain seal
// of ReadRegularFileSnapshotUpTo. The reader never consumes a payload overflow
// byte: growth is detected through descriptor facts after its bounded read.
func MaximumFileSnapshotWork(pathComponents int, maximumBytes int64) (FileWork, error) {
	if pathComponents <= 0 || maximumBytes < 0 {
		return FileWork{}, fmt.Errorf("file snapshot work requires positive path depth and nonnegative bytes")
	}
	if pathComponents > int(^uint(0)>>1)/2 {
		return FileWork{}, fmt.Errorf("file snapshot work overflows")
	}
	return FileWork{pathWork: 2 * pathComponents, byteWork: maximumBytes, entries: 1}, nil
}

// MaximumFileIdentityWork bounds CaptureEntryIdentity's parent opening and
// final no-follow entry observation. It reads no payload.
func MaximumFileIdentityWork(pathComponents int) (FileWork, error) {
	if pathComponents <= 0 {
		return FileWork{}, fmt.Errorf("file identity work requires positive path depth")
	}
	return FileWork{pathWork: pathComponents, entries: 1}, nil
}

// MaximumFilePersistenceWork includes parent opening and the two ancestor-chain
// seals around ConfirmFilePersistence. File/parent sync reads no payload.
func MaximumFilePersistenceWork(pathComponents int) (FileWork, error) {
	if pathComponents <= 0 || pathComponents > int(^uint(0)>>1)/3 {
		return FileWork{}, fmt.Errorf("file persistence work requires bounded positive path depth")
	}
	return FileWork{pathWork: 3 * pathComponents, entries: 1}, nil
}

func (work FileWork) PathWork() int   { return work.pathWork }
func (work FileWork) EntryWork() int  { return work.entries }
func (work FileWork) ByteWork() int64 { return work.byteWork }
