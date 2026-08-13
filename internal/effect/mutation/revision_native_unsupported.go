//go:build !darwin && !linux

package mutation

import (
	"fmt"
	"os"
)

type revisionNativeIdentity struct{}

func (revisionNativeIdentity) equal(revisionNativeIdentity) bool { return true }

type revisionNativeEntry struct {
	identity revisionNativeIdentity
	mode     uint32
	size     int64
}

func revisionNativeEntryFromFileInfo(os.FileInfo) (revisionNativeEntry, bool) {
	return revisionNativeEntry{}, false
}

func (revisionNativeEntry) isSymlink() bool   { return false }
func (revisionNativeEntry) isDirectory() bool { return false }
func (revisionNativeEntry) isRegular() bool   { return false }
func (revisionNativeEntry) executable() bool  { return false }

func observeRevisionChild(*os.File, string) (revisionNativeEntry, error) {
	return revisionNativeEntry{}, fmt.Errorf("descriptor-bound mutation revision traversal is unsupported")
}

func openRevisionChild(*os.File, string, revisionNativeEntry) (*os.File, error) {
	return nil, fmt.Errorf("descriptor-bound mutation revision traversal is unsupported")
}

func verifyRevisionChild(*os.File, string, *os.File, revisionNativeEntry) error {
	return fmt.Errorf("descriptor-bound mutation revision traversal is unsupported")
}

func observeRevisionOpened(*os.File) (revisionNativeEntry, error) {
	return revisionNativeEntry{}, fmt.Errorf("descriptor-bound mutation revision traversal is unsupported")
}

func readRevisionSymlink(*os.File, string, revisionNativeEntry) (string, error) {
	return "", fmt.Errorf("descriptor-bound mutation revision traversal is unsupported")
}
