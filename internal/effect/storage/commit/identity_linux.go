//go:build linux

package commit

import "golang.org/x/sys/unix"

func statChangeTime(stat *unix.Stat_t) (int64, int64) {
	return int64(stat.Ctim.Sec), int64(stat.Ctim.Nsec)
}
