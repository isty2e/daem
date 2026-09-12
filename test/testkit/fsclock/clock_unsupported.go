//go:build !darwin && !linux

package fsclock

import "testing"

// WaitForTick requires the native Unix ctime probe; unsupported calls fail.
func WaitForTick(t testing.TB, _ string) {
	t.Helper()
	t.Fatal("filesystem ctime probe requires Linux or Darwin")
}
