package execute

import (
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestJournalCleanupPathsRequireCanonicalAbsoluteRecoveryRoot(t *testing.T) {
	root := t.TempDir()
	separator := string(filepath.Separator)
	tests := []struct {
		name string
		path string
	}{
		{name: "empty"},
		{name: "relative", path: "recovery"},
		{
			name: "noncanonical",
			path: filepath.Join(root, "parent") +
				separator + ".." + separator + "recovery",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := (JournalCleanupPaths{RecoveryDir: test.path}).validate()
			if err == nil {
				t.Fatalf("JournalCleanupPaths.validate accepted %q", test.path)
			}
		})
	}

	canonical := filepath.Join(t.TempDir(), "recovery")
	if err := (JournalCleanupPaths{RecoveryDir: canonical}).validate(); err != nil {
		t.Fatalf("JournalCleanupPaths.validate(%q): %v", canonical, err)
	}
}

func TestJournalCleanupBoundaryExposesOnlyRecoveryRootAuthority(t *testing.T) {
	assertStructFields := func(value any, want []string) {
		t.Helper()
		kind := reflect.TypeOf(value)
		got := make([]string, kind.NumField())
		for index := range kind.NumField() {
			got[index] = kind.Field(index).Name
		}
		if !slices.Equal(got, want) {
			t.Fatalf("%s fields = %v, want %v", kind.Name(), got, want)
		}
	}

	assertStructFields(JournalCleanupPaths{}, []string{"RecoveryDir"})
	assertStructFields(
		JournalCleanupOptions{},
		[]string{
			"ValidateBeforeEffects",
			"Filesystem",
			"LegacyJournalAuthority",
			"StateCodec",
		},
	)
}
