//go:build darwin

package commit

import "golang.org/x/sys/unix"

func statChangeTime(stat *unix.Stat_t) (int64, int64) {
	return stat.Ctim.Sec, stat.Ctim.Nsec
}
