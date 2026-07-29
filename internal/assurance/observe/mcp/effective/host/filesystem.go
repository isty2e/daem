package host

import (
	"fmt"
	"io"
	"os"
)

const maximumConfigBytes = 4 << 20

func readStableRegularFile(path string) ([]byte, bool, error) {
	before, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return nil, false, fmt.Errorf("file must not be a symlink")
	}
	if !before.Mode().IsRegular() {
		return nil, false, fmt.Errorf("path must be a regular file")
	}
	if before.Size() > maximumConfigBytes {
		return nil, false, fmt.Errorf("file exceeds %d bytes", maximumConfigBytes)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()

	opened, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	if !os.SameFile(before, opened) ||
		before.Size() != opened.Size() ||
		!before.ModTime().Equal(opened.ModTime()) {
		return nil, false, fmt.Errorf("file changed while opening")
	}
	content, err := io.ReadAll(io.LimitReader(file, maximumConfigBytes+1))
	if err != nil {
		return nil, false, err
	}
	if len(content) > maximumConfigBytes {
		return nil, false, fmt.Errorf("file exceeds %d bytes", maximumConfigBytes)
	}

	afterOpen, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	afterPath, err := os.Lstat(path)
	if err != nil {
		return nil, false, fmt.Errorf("reinspect file: %w", err)
	}
	if afterPath.Mode()&os.ModeSymlink != 0 ||
		!os.SameFile(opened, afterOpen) ||
		!os.SameFile(opened, afterPath) ||
		opened.Size() != afterOpen.Size() ||
		!opened.ModTime().Equal(afterOpen.ModTime()) {
		return nil, false, fmt.Errorf("file changed while reading")
	}
	return content, true, nil
}
