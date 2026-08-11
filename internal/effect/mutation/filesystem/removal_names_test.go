package filesystem

import "testing"

func TestLogicalRemovalNamesRequireDistinctRoleSpecificComponents(t *testing.T) {
	t.Parallel()

	valid, err := NewLogicalRemovalNames(
		".daem-tombstone-0123456789abcdef0123456789abcdef",
		".daem-cleanup-0123456789abcdef0123456789abcdef",
	)
	if err != nil {
		t.Fatalf("NewLogicalRemovalNames(valid): %v", err)
	}
	if !valid.Valid() || valid.Residue() != ".daem-tombstone-0123456789abcdef0123456789abcdef" ||
		valid.Cleanup() != ".daem-cleanup-0123456789abcdef0123456789abcdef" {
		t.Fatalf("valid names = %#v", valid)
	}

	for _, test := range []struct {
		name    string
		residue string
		cleanup string
	}{
		{name: "swapped roles", residue: ".daem-cleanup-token", cleanup: ".daem-tombstone-token"},
		{name: "missing residue token", residue: ".daem-tombstone-", cleanup: ".daem-cleanup-token"},
		{name: "missing cleanup token", residue: ".daem-tombstone-token", cleanup: ".daem-cleanup-"},
		{name: "nested residue", residue: ".daem-tombstone-a/b", cleanup: ".daem-cleanup-token"},
		{name: "nested cleanup", residue: ".daem-tombstone-token", cleanup: ".daem-cleanup-a/b"},
		{
			name:    "different tokens",
			residue: ".daem-tombstone-0123456789abcdef0123456789abcdef",
			cleanup: ".daem-cleanup-fedcba9876543210fedcba9876543210",
		},
		{
			name:    "uppercase token",
			residue: ".daem-tombstone-0123456789ABCDEF0123456789ABCDEF",
			cleanup: ".daem-cleanup-0123456789ABCDEF0123456789ABCDEF",
		},
		{
			name:    "short token",
			residue: ".daem-tombstone-0123456789abcdef",
			cleanup: ".daem-cleanup-0123456789abcdef",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewLogicalRemovalNames(test.residue, test.cleanup); err == nil {
				t.Fatal("NewLogicalRemovalNames accepted invalid pair")
			}
		})
	}
}
