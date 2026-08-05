//go:build linux

package rootedpath

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

const (
	linuxBootIDPath         = "/proc/sys/kernel/random/boot_id"
	maximumLinuxBootIDBytes = 37
)

type linuxBootID struct {
	high uint64
	low  uint64
}

var currentLinuxBootID = sync.OnceValues(readLinuxBootID)

func readLinuxBootID() (result linuxBootID, resultErr error) {
	fd, err := unix.Open(
		linuxBootIDPath,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK,
		0,
	)
	if err != nil {
		return linuxBootID{}, err
	}
	file := os.NewFile(uintptr(fd), linuxBootIDPath)
	if file == nil {
		_ = unix.Close(fd)
		return linuxBootID{}, fmt.Errorf("wrap Linux boot identity descriptor")
	}
	defer func() {
		resultErr = errors.Join(resultErr, file.Close())
	}()

	var metadata unix.Stat_t
	if err := unix.Fstat(fd, &metadata); err != nil {
		return linuxBootID{}, err
	}
	if metadata.Mode&unix.S_IFMT != unix.S_IFREG {
		return linuxBootID{}, fmt.Errorf("Linux boot identity is not a regular procfs entry")
	}
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(fd, &filesystem); err != nil {
		return linuxBootID{}, err
	}
	if filesystem.Type != unix.PROC_SUPER_MAGIC {
		return linuxBootID{}, fmt.Errorf("Linux boot identity is not backed by procfs")
	}

	content, err := io.ReadAll(io.LimitReader(file, maximumLinuxBootIDBytes+1))
	if err != nil {
		return linuxBootID{}, err
	}
	if len(content) > maximumLinuxBootIDBytes {
		return linuxBootID{}, fmt.Errorf("Linux boot identity exceeds %d bytes", maximumLinuxBootIDBytes)
	}
	return parseLinuxBootID(string(content))
}

func parseLinuxBootID(value string) (linuxBootID, error) {
	value = strings.TrimSuffix(value, "\n")
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return linuxBootID{}, fmt.Errorf("Linux boot identity is not a canonical UUID")
	}
	if strings.ToLower(value) != value {
		return linuxBootID{}, fmt.Errorf("Linux boot identity must use lowercase hexadecimal")
	}
	hexadecimal := strings.NewReplacer("-", "").Replace(value)
	var decoded [16]byte
	if _, err := hex.Decode(decoded[:], []byte(hexadecimal)); err != nil {
		return linuxBootID{}, fmt.Errorf("Linux boot identity is not hexadecimal: %w", err)
	}
	return linuxBootID{
		high: binary.BigEndian.Uint64(decoded[:8]),
		low:  binary.BigEndian.Uint64(decoded[8:]),
	}, nil
}
