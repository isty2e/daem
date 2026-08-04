package clijson

import "testing"

func requireSchemaVersion(t testing.TB, surface string, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("%s schema_version = %d, want %d", surface, got, want)
	}
}
