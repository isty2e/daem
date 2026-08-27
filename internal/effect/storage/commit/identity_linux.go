//go:build linux

package commit

import "golang.org/x/sys/unix"

func observationDirectoryOpenFlags(searchOnly bool) int {
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW
	if searchOnly {
		flags = unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW
	}
	return flags
}

func statChangeTime(stat *unix.Stat_t) (int64, int64) {
	return int64(stat.Ctim.Sec), int64(stat.Ctim.Nsec)
}

func retainedDirectoryStillLinked(_ int, _ EntryIdentity, stat *unix.Stat_t) (bool, error) {
	return stat.Nlink != 0, nil
}
