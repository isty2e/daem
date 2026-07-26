package recovery

import (
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
)

func testPathStateHash(seed string) string {
	return string(artifact.HashFileContent([]byte(seed)))
}

func TestExpectedPathStateCloneOwnsPermissionMode(t *testing.T) {
	mode := PermissionMode(0o640)
	original := ExpectedPathState{
		Existed: true, Kind: PathKindFile, ContentHash: testPathStateHash("after"), PathMode: &mode,
	}
	clone := original.Clone()
	*clone.PathMode = PermissionMode(0o600)

	if *original.PathMode != PermissionMode(0o640) {
		t.Fatalf("original mode = %o, want 0640", *original.PathMode)
	}
	if original.Equal(clone) {
		t.Fatal("mutated clone unexpectedly equals original")
	}
}

func TestPathStateValidationAdmitsRecoveryShapes(t *testing.T) {
	fileMode := PermissionMode(0o600)
	for _, test := range []struct {
		name        string
		contentPath string
		before      BeforePathState
		expected    ExpectedPathState
	}{
		{
			name: "whole file",
			before: BeforePathState{
				Existed: true, Kind: PathKindFile, ContentHash: testPathStateHash("before"),
				BackupPath: "backups/file", PathMode: &fileMode,
			},
			expected: ExpectedPathState{
				Existed: true, Kind: PathKindFile, ContentHash: testPathStateHash("after"), PathMode: &fileMode,
			},
		},
		{
			name: "directory",
			before: BeforePathState{
				Existed: true, Kind: PathKindDirectory, ContentHash: testPathStateHash("before"),
				BackupPath: "backups/directory",
			},
			expected: ExpectedPathState{
				Existed: true, Kind: PathKindDirectory, ContentHash: testPathStateHash("after"),
			},
		},
		{
			name:     "symlink",
			before:   BeforePathState{Existed: true, Kind: PathKindSymlink, LinkTarget: "target"},
			expected: ExpectedPathState{Existed: true, Kind: PathKindSymlink, LinkTarget: "target"},
		},
		{
			name:        "absent aggregate contribution in existing file",
			contentPath: "/servers/context7",
			before: BeforePathState{
				PathExisted: true, ParentExisted: true, PathMode: &fileMode,
			},
			expected: ExpectedPathState{
				PathExisted: true, PathMode: &fileMode,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateBefore(test.before, test.contentPath); err != nil {
				t.Fatalf("ValidateBefore returned error: %v", err)
			}
			if err := ValidateExpected(test.expected, test.contentPath); err != nil {
				t.Fatalf("ValidateExpected returned error: %v", err)
			}
		})
	}
}

func TestPathStateValidationRejectsUnsafeOrContradictoryFacts(t *testing.T) {
	mode := PermissionMode(0o600)
	for _, test := range []struct {
		name string
		run  func() error
	}{
		{
			name: "unsafe backup",
			run: func() error {
				return ValidateBefore(BeforePathState{
					Existed: true, Kind: PathKindFile, ContentHash: testPathStateHash("before"),
					BackupPath: "../escape", PathMode: &mode,
				}, "")
			},
		},
		{
			name: "content path without physical file",
			run: func() error {
				return ValidateExpected(ExpectedPathState{
					Existed: true, Kind: PathKindFile, ContentHash: testPathStateHash("after"), PathMode: &mode,
				}, "/servers/context7")
			},
		},
		{
			name: "directory carries file mode",
			run: func() error {
				return ValidateExpected(ExpectedPathState{
					Existed: true, Kind: PathKindDirectory, ContentHash: testPathStateHash("after"), PathMode: &mode,
				}, "")
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); err == nil {
				t.Fatal("validation unexpectedly succeeded")
			}
		})
	}
}
